package reviewroute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Preview reads local state only and never appends events, writes settings,
// decrypts credentials or contacts an SCM provider.
func (m Manager) Preview(ctx context.Context, in Input) (Route, error) {
	in, err := normalize(in)
	if err != nil {
		return Route{}, err
	}
	r := Route{Input: in}
	_, err = m.validate(ctx, r)
	return r, err
}
func (m Manager) Get(ctx context.Context, name string) (Route, error) {
	if !namePattern.MatchString(name) {
		return Route{}, ErrInvalid
	}
	row, err := m.Store.GetSetting(ctx, RoutePrefix+name)
	if err != nil {
		return Route{}, err
	}
	var r Route
	if json.Unmarshal(row.Value, &r) != nil || r.Name != name {
		return Route{}, errors.New("stored review route is corrupt")
	}
	in, err := normalize(r.Input)
	if err != nil || in != r.Input || r.ActivationSeq < 1 || r.UpdatedAt.IsZero() {
		return Route{}, errors.New("stored review route is corrupt")
	}
	r.Revision = row.Revision
	return r, nil
}
func (m Manager) ListPage(ctx context.Context, after string, limit int) ([]Route, string, error) {
	if after != "" && !namePattern.MatchString(after) {
		return nil, "", ErrInvalid
	}
	cursor := ""
	if after != "" {
		cursor = RoutePrefix + after
	}
	rows, err := m.Store.ListSettingsPage(ctx, RoutePrefix, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]Route, 0, len(rows))
	next := ""
	for _, row := range rows {
		name := strings.TrimPrefix(row.Key, RoutePrefix)
		r, err := m.Get(ctx, name)
		if err != nil {
			return nil, "", err
		}
		out = append(out, r)
		next = name
	}
	if len(rows) < limit {
		next = ""
	}
	return out, next, nil
}

// Put creates a new activation barrier before the CAS. Even if that CAS fails,
// its audit event is only a request and cannot activate routing. Re-enabling or
// editing an enabled route starts after the new barrier, never with a backlog.
func (m Manager) Put(ctx context.Context, in Input, expected int64, actor string) (Route, error) {
	in, err := normalize(in)
	if err != nil || expected < 0 || actor == "" || len(actor) > 200 {
		return Route{}, ErrInvalid
	}
	old, err := m.Get(ctx, in.Name)
	fresh := errors.Is(err, storage.ErrNotFound)
	if err != nil && !fresh {
		return Route{}, err
	}
	if (fresh && expected != 0) || (!fresh && old.Revision != expected) {
		return Route{}, storage.ErrConflict
	}
	if !fresh && old.Input == in {
		return old, nil
	}
	r := Route{Input: in, UpdatedAt: m.now(), UpdatedBy: actor}
	var guards []storage.SettingRevision
	disabled := old.Input
	disabled.Enabled = false
	if fresh || in != disabled {
		docRev, err := m.validate(ctx, r)
		if err != nil {
			return Route{}, err
		}
		guards = append(guards, storage.SettingRevision{Key: scm.SettingKey, Revision: docRev})
	}
	ev := domain.NewEvent(ChangeRequested, in.GraphID, map[string]any{"route": in.Name, "expectedRevision": expected, "enabled": in.Enabled}).WithActor(actor)
	if err = m.Store.AppendEvent(ctx, ev); err != nil {
		return Route{}, err
	}
	if ev.Seq < 1 {
		return Route{}, errors.New("review route activation sequence is unavailable")
	}
	r.ActivationSeq = ev.Seq
	raw, err := json.Marshal(r)
	if err != nil {
		return Route{}, err
	}
	r.Revision, err = m.Store.PutSettingIf(ctx, RoutePrefix+in.Name, raw, expected, actor, guards)
	return r, err
}

func receiptValid(v Receipt) bool {
	in, err := normalize(v.Route.Input)
	if err != nil || in != v.Route.Input || !v.Route.Enabled || v.Route.Revision < 1 || v.Route.ActivationSeq < 1 || !validPull(v.PullRequest) || v.ID != receiptID(v.PullRequest) || v.EventID == "" || len(v.EventID) > 200 || v.EventSeq <= v.Route.ActivationSeq || v.MessageID != "review-route-"+v.ID || v.CreatedAt.IsZero() || v.UpdatedAt.Before(v.CreatedAt) {
		return false
	}
	if v.Connection != v.Route.Connection || v.ConnectionRevision != v.Route.ConnectionRevision || v.Provider != v.Route.Provider || v.Instance != v.Route.Instance || v.Repository != v.Route.Repository {
		return false
	}
	switch v.State {
	case "queued", "failed":
	case "dispatching", "unknown":
		if v.ExpectedRunID == "" {
			return false
		}
	case "published":
		if v.ExpectedRunID == "" || v.DispatchRunID != v.ExpectedRunID {
			return false
		}
	default:
		return false
	}
	return true
}
func (m Manager) GetReceipt(ctx context.Context, id string) (Receipt, error) {
	if !idPattern.MatchString(id) {
		return Receipt{}, ErrInvalid
	}
	row, err := m.Store.GetSetting(ctx, ReceiptPrefix+id)
	if err != nil {
		return Receipt{}, err
	}
	var v Receipt
	if json.Unmarshal(row.Value, &v) != nil || v.ID != id || !receiptValid(v) {
		return Receipt{}, errors.New("stored review route receipt is corrupt")
	}
	v.Revision = row.Revision
	return v, nil
}
func (m Manager) ReceiptsPage(ctx context.Context, after string, limit int) ([]Receipt, string, error) {
	if after != "" && !idPattern.MatchString(after) {
		return nil, "", ErrInvalid
	}
	cursor := ""
	if after != "" {
		cursor = ReceiptPrefix + after
	}
	rows, err := m.Store.ListSettingsPage(ctx, ReceiptPrefix, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]Receipt, 0, len(rows))
	next := ""
	for _, row := range rows {
		id := strings.TrimPrefix(row.Key, ReceiptPrefix)
		v, err := m.GetReceipt(ctx, id)
		if err != nil {
			return nil, "", err
		}
		out = append(out, v)
		next = id
	}
	if len(rows) < limit {
		next = ""
	}
	return out, next, nil
}
func (m Manager) saveReceipt(ctx context.Context, v Receipt, expected int64, guards ...storage.SettingRevision) (Receipt, error) {
	v.UpdatedAt = m.now()
	if v.UpdatedAt.Before(v.CreatedAt) {
		v.UpdatedAt = v.CreatedAt
	}
	v.Revision = 0
	if !receiptValid(v) {
		return v, errors.New("invalid review route receipt")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	v.Revision, err = m.Store.PutSettingIf(ctx, ReceiptPrefix+v.ID, raw, expected, "system:review-route", guards)
	return v, err
}

func eventPull(ev *domain.Event) (PullRequest, error) {
	if ev == nil || ev.Type != domain.EvPullRequestOpened || ev.Seq < 1 || ev.ID == "" || len(ev.Data) > 16384 {
		return PullRequest{}, ErrInvalid
	}
	var d struct {
		PullRequest
		ReviewRevisionAvailable bool `json:"reviewRevisionAvailable"`
	}
	if json.Unmarshal(ev.Data, &d) != nil || !d.ReviewRevisionAvailable {
		return PullRequest{}, ErrInvalid
	}
	p := d.PullRequest
	u, err := instance(p.Instance)
	if err != nil {
		return p, err
	}
	p.Instance = u
	if !validPull(p) {
		return p, ErrInvalid
	}
	return p, nil
}
func (m Manager) enqueue(ctx context.Context, r Route, ev *domain.Event) error {
	p, err := eventPull(ev)
	if err != nil {
		return nil
	} // Legacy/incomplete facts cannot grant review authority.
	if ev.Seq <= r.ActivationSeq || p.Connection != r.Connection || p.ConnectionRevision != r.ConnectionRevision || p.Provider != r.Provider || p.Instance != r.Instance || p.Repository != r.Repository {
		return nil
	}
	id := receiptID(p)
	if _, err = m.GetReceipt(ctx, id); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	docRev, err := m.connection(ctx, r.Input)
	if err != nil {
		return err
	}
	now := m.now()
	v := Receipt{ID: id, PullRequest: p, Route: r, EventID: ev.ID, EventSeq: ev.Seq, SourceGraphID: ev.GraphID, SourceExecutionID: ev.ExecutionID, State: "queued", MessageID: "review-route-" + id, CreatedAt: now, UpdatedAt: now}
	_, err = m.saveReceipt(ctx, v, 0, storage.SettingRevision{Key: RoutePrefix + r.Name, Revision: r.Revision}, storage.SettingRevision{Key: scm.SettingKey, Revision: docRev})
	if errors.Is(err, storage.ErrConflict) {
		// A competing route may have reserved this global resource revision.
		if _, readErr := m.GetReceipt(ctx, id); readErr == nil {
			return nil
		}
	}
	return err
}

type cursor struct {
	AfterSeq int64  `json:"afterSeq"`
	AfterKey string `json:"afterKey,omitempty"`
}

func (m Manager) loadCursor(ctx context.Context, key string) (cursor, int64, error) {
	row, err := m.Store.GetSetting(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return cursor{}, 0, nil
	}
	if err != nil {
		return cursor{}, 0, err
	}
	var v cursor
	if json.Unmarshal(row.Value, &v) != nil || v.AfterSeq < 0 || len(v.AfterKey) > 200 {
		return v, 0, errors.New("stored review route cursor is corrupt")
	}
	return v, row.Revision, nil
}
func (m Manager) saveCursor(ctx context.Context, key string, v cursor, expected int64, guards ...storage.SettingRevision) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = m.Store.PutSettingIf(ctx, key, raw, expected, "system:review-route", guards)
	return err
}
func eventCursorKey(r Route) string {
	return fmt.Sprintf("scm/review-route-event-cursors/%s/%d", r.Name, r.Revision)
}
