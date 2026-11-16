package trigger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// FailedRetry is an explicit, single-use admission generation. Original
// admissions and processed outcomes are retained; nothing is deleted.
type FailedRetry struct {
	Generation string    `json:"generation"`
	TaskID     string    `json:"taskId"`
	Actor      string    `json:"actor"`
	RecordedAt time.Time `json:"recordedAt"`
	Consumed   bool      `json:"consumed"`
}

func pendingFailedRetry(t *Trigger, key string) bool {
	r, ok := t.FailedRetries[key]
	return t.Processed[key] == "failed" && ok && r.Generation != "" && !r.Consumed
}

// RetryFailed authorizes reconsideration only for an undispatched GitHub draft.
// The old task is cancelled by revision before publishing the retry permission.
// Preparation/start interruptions with retained execution resources require
// inspection and cannot use this narrow recovery path.
func (m Manager) RetryFailed(ctx context.Context, id, issue, taskID, actor string) (*Trigger, error) {
	lease, holder := "trigger/"+id, domain.NewID("retry")
	ok, err := m.Store.AcquireLease(ctx, lease, holder, time.Minute)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, storage.ErrConflict
	}
	defer func() { _ = m.Store.ReleaseLease(context.WithoutCancel(ctx), lease, holder) }()
	t, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	key := DedupKey(t, Issue{Source: "github", Key: issue})
	if t.State != Paused || t.Source.Kind != "github" || !strings.HasPrefix(issue, t.Source.Repository+"#") || t.ReadinessDisabled || t.CurrentTask != "" || t.Claiming != "" || t.Processed[key] != "failed" || pendingFailedRetry(t, key) {
		return nil, fmt.Errorf("%w: select a paused readiness trigger with a failed admission", storage.ErrConflict)
	}
	tm := taskconfig.Manager{Store: m.Store}
	d, err := tm.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if d.TriggerGitHubIssue != issue || d.GraphID != t.Workflow || d.ExecutionGraphID != "" || (d.Status != "draft" && !(d.Status == "cancelled" && d.UpdatedBy == actor)) {
		return nil, fmt.Errorf("%w: recovery requires the original undispatched issue draft", storage.ErrConflict)
	}
	if _, err := m.Store.GetGraph(ctx, domain.TaskRunGraphPrefix+strings.TrimPrefix(taskID, "tsk_"), 0); !errors.Is(err, storage.ErrNotFound) {
		if err != nil {
			return nil, err
		}
		return nil, storage.ErrConflict
	}
	xs, err := m.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: domain.TaskRunGraphPrefix + strings.TrimPrefix(taskID, "tsk_"), Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(xs) != 0 {
		return nil, storage.ErrConflict
	}
	r := FailedRetry{Generation: domain.NewID("retry"), TaskID: taskID, Actor: actor, RecordedAt: m.now()}
	if err := m.Store.AppendEvent(ctx, domain.NewEvent("trigger.failed_retry_requested", t.Workflow, map[string]any{"trigger": id, "issue": issue, "task": taskID, "generation": r.Generation}).WithActor(actor)); err != nil {
		return nil, err
	}
	if d.Status == "draft" {
		if _, err := tm.Transition(ctx, d, "cancelled", actor); err != nil {
			return nil, err
		}
	}
	out, err := m.Update(ctx, id, actor, func(current *Trigger) error {
		if current.Source != t.Source || current.Workflow != t.Workflow || current.Consent.Scope != t.Consent.Scope || current.State != Paused || current.CurrentTask != "" || current.Claiming != "" || current.Processed[key] != "failed" || pendingFailedRetry(current, key) {
			return storage.ErrConflict
		}
		if current.FailedRetries == nil {
			current.FailedRetries = map[string]FailedRetry{}
		}
		current.FailedRetries[key] = r
		return nil
	})
	if err == nil {
		m.audit(ctx, out, "failed_retry_recorded", actor)
	}
	return out, err
}
