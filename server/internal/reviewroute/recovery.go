package reviewroute

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (m Manager) release(ctx context.Context, lease, holder string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return m.Store.ReleaseLease(cleanup, lease, holder)
}

func recoveryInput(id string, expected int64, actor string) bool {
	return idPattern.MatchString(id) && expected > 0 && actor != "" && len(actor) <= 200 && strings.TrimSpace(actor) == actor && !strings.ContainsAny(actor, "\x00\r\n") && utf8.ValidString(actor)
}

func (m Manager) recoveryAudit(ctx context.Context, v Receipt, actor, action string, confirmed bool) error {
	ev := domain.NewEvent(RecoveryRequested, v.Route.GraphID, map[string]any{
		"receipt": v.ID, "action": action, "expectedRevision": v.Revision, "confirmed": confirmed,
	}).WithActor(actor)
	return m.Store.AppendEvent(ctx, ev)
}

// Reconcile only reads historical dispatch evidence. It does not consult the
// current route, connection or graph, and never publishes or executes work.
// Missing evidence leaves the outcome unknown, including after history expiry.
func (m Manager) Reconcile(ctx context.Context, id string, expectedRevision int64, actor string) (out Receipt, resultErr error) {
	if !recoveryInput(id, expectedRevision, actor) {
		return out, ErrInvalid
	}
	if m.Store == nil {
		return out, ErrUnavailable
	}
	holder, lease := domain.NewID("review-route-recovery"), "review-route/"+id
	ok, err := m.Store.AcquireLease(ctx, lease, holder, 2*time.Minute)
	if err != nil {
		return out, err
	}
	if !ok {
		return out, storage.ErrConflict
	}
	defer func() { resultErr = errors.Join(resultErr, m.release(ctx, lease, holder)) }()
	v, err := m.GetReceipt(ctx, id)
	if err != nil {
		return out, err
	}
	if v.Revision != expectedRevision || (v.State != "unknown" && v.State != "dispatching") {
		return v, storage.ErrConflict
	}
	rows, err := m.Store.GetMessage(ctx, v.MessageID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return v, err
	}
	confirmed := false
	for _, row := range rows {
		if historicalMessageMatches(row, v) {
			confirmed = true
			break
		}
	}
	if err = m.recoveryAudit(ctx, v, actor, "reconcile", confirmed); err != nil {
		return v, err
	}
	v.State, v.Error = "unknown", ErrUnconfirmed.Error()
	if confirmed {
		v.State, v.Error, v.DispatchRunID = "published", "", v.ExpectedRunID
	}
	return m.saveReceipt(ctx, v, expectedRevision)
}

func historicalMessageMatches(row *domain.MessageRecord, v Receipt) bool {
	if row == nil || row.GraphID != v.Route.GraphID || row.Channel != v.Route.ChannelID {
		return false
	}
	switch row.Status {
	case domain.MsgPublished, domain.MsgDelivered, domain.MsgAcked, domain.MsgHeld:
	default:
		return false
	}
	m := row.Message
	return m.ID == v.MessageID && m.Type == MessageType && m.Source == "system:review-router" && m.Target == v.Route.NodeID &&
		m.Metadata["review_dispatch"] == true && m.Metadata[domain.MetaInjectedBy] == "system:review-router" &&
		m.Metadata[domain.MetaGraph] == v.Route.GraphID && m.Metadata[domain.MetaChannel] == v.Route.ChannelID &&
		m.Metadata[domain.MetaRunID] == v.ExpectedRunID && exactVersion(m.Metadata[domain.MetaGraphVersion], v.Route.GraphVersion)
}

// Persisted JSON turns integer metadata into float64. Reject fractions and
// lossy coercions: this evidence must identify exactly the reviewed version.
func exactVersion(value any, expected int) bool {
	switch v := value.(type) {
	case int:
		return v == expected
	case int64:
		return v == int64(expected)
	case float64:
		return v == float64(expected)
	case json.Number:
		return v.String() == strconv.Itoa(expected)
	default:
		return false
	}
}

// RetryFailed requeues only a known pre-dispatch failure under the unchanged
// reviewed route. Uncertain or published attempts can never use this path.
func (m Manager) RetryFailed(ctx context.Context, id string, expectedRevision int64, actor string) (out Receipt, resultErr error) {
	if !recoveryInput(id, expectedRevision, actor) {
		return out, ErrInvalid
	}
	if m.Store == nil {
		return out, ErrUnavailable
	}
	holder, lease := domain.NewID("review-route-retry"), "review-route/"+id
	ok, err := m.Store.AcquireLease(ctx, lease, holder, 2*time.Minute)
	if err != nil {
		return out, err
	}
	if !ok {
		return out, storage.ErrConflict
	}
	defer func() { resultErr = errors.Join(resultErr, m.release(ctx, lease, holder)) }()
	v, err := m.GetReceipt(ctx, id)
	if err != nil {
		return out, err
	}
	if v.Revision != expectedRevision || v.State != "failed" || v.ExpectedRunID != "" || v.DispatchRunID != "" {
		return v, storage.ErrConflict
	}
	r, err := m.Get(ctx, v.Route.Name)
	if err != nil {
		return v, err
	}
	if !r.Enabled || r.Revision != v.Route.Revision || r.Input != v.Route.Input || r.ActivationSeq != v.Route.ActivationSeq {
		return v, storage.ErrConflict
	}
	docRev, err := m.validate(ctx, r)
	if err != nil {
		return v, err
	}
	if m.Dispatch == nil {
		return v, ErrUnavailable
	}
	record, err := m.Store.GetGraphRecord(ctx, r.GraphID)
	if err != nil {
		return v, err
	}
	if record.Version != r.GraphVersion || record.RunID == "" || record.State != domain.GraphRunning || record.DesiredState != domain.GraphRunning {
		return v, ErrUnavailable
	}
	if err = m.recoveryAudit(ctx, v, actor, "retry", false); err != nil {
		return v, err
	}
	v.State, v.Error = "queued", ""
	return m.saveReceipt(ctx, v, expectedRevision,
		storage.SettingRevision{Key: RoutePrefix + r.Name, Revision: r.Revision},
		storage.SettingRevision{Key: scm.SettingKey, Revision: docRev})
}
