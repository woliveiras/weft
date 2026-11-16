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
	return ok && r.Generation != "" && !r.Consumed && (t.Processed[key] == "failed" || (r.TaskID != "" && t.Processed[key] == r.TaskID))
}

// RetryFailed authorizes a new admission after an undispatched failure or a
// stopped task without delivery. Original tasks, graphs and admissions survive.
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
	if t.State != Paused || t.Source.Kind != "github" || !strings.HasPrefix(issue, t.Source.Repository+"#") || t.ReadinessDisabled || t.CurrentTask != "" || t.Claiming != "" || (t.Processed[key] != "failed" && t.Processed[key] != taskID) || pendingFailedRetry(t, key) {
		return nil, fmt.Errorf("%w: select a paused readiness trigger with a failed admission or undelivered task", storage.ErrConflict)
	}
	tm := taskconfig.Manager{Store: m.Store}
	d, err := tm.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if d.TriggerGitHubIssue != issue || d.GraphID != t.Workflow {
		return nil, fmt.Errorf("%w: recovery requires the original issue task", storage.ErrConflict)
	}
	if d.ExecutionGraphID != "" {
		if t.Processed[key] != taskID {
			return nil, storage.ErrConflict
		}
		if err := m.checkUndeliveredTask(ctx, d); err != nil {
			return nil, err
		}
	} else {
		if t.Processed[key] != "failed" || (d.Status != "draft" && !(d.Status == "cancelled" && d.UpdatedBy == actor)) {
			return nil, storage.ErrConflict
		}
		graphID := domain.TaskRunGraphPrefix + strings.TrimPrefix(taskID, "tsk_")
		if _, err := m.Store.GetGraph(ctx, graphID, 0); !errors.Is(err, storage.ErrNotFound) {
			if err != nil {
				return nil, err
			}
			return nil, storage.ErrConflict
		}
		xs, err := m.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: graphID, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(xs) != 0 {
			return nil, storage.ErrConflict
		}
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
		if current.Source != t.Source || current.Workflow != t.Workflow || current.Consent.Scope != t.Consent.Scope || current.State != Paused || current.CurrentTask != "" || current.Claiming != "" || current.Processed[key] != t.Processed[key] || pendingFailedRetry(current, key) {
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

// A stopped graph is the fence against redispatch and pending external effects.
// Historical successful turns without a PR are eligible after explicit task stop.
func (m Manager) checkUndeliveredTask(ctx context.Context, d *taskconfig.Draft) error {
	if d.ExecutionGraphID != domain.TaskRunGraphPrefix+strings.TrimPrefix(d.ID, "tsk_") {
		return storage.ErrConflict
	}
	switch d.Status {
	case "blocked", "failed", "interrupted", "cancelled", "succeeded":
	default:
		return fmt.Errorf("%w: stop the original task before requesting retry", storage.ErrConflict)
	}
	g, err := m.Store.GetGraph(ctx, d.ExecutionGraphID, 0)
	if err != nil {
		return err
	}
	if g.Metadata.Labels[domain.LabelTask] != d.ID {
		return storage.ErrConflict
	}
	rec, err := m.Store.GetGraphRecord(ctx, d.ExecutionGraphID)
	if err != nil {
		return err
	}
	if rec.State != domain.GraphStopped || rec.DesiredState != domain.GraphStopped {
		return fmt.Errorf("%w: wait for the original task graph to stop", storage.ErrConflict)
	}
	xs, err := m.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: d.ExecutionGraphID, Limit: 1000})
	if err != nil {
		return err
	}
	if len(xs) == 0 || len(xs) >= 1000 {
		return storage.ErrConflict
	}
	for _, x := range xs {
		if !x.Status.Terminal() {
			return storage.ErrConflict
		}
	}
	approvals, err := m.Store.ListApprovals(ctx, storage.ApprovalFilter{GraphID: d.ExecutionGraphID, Status: domain.ApprovalPending, Limit: 1})
	if err != nil {
		return err
	}
	if len(approvals) > 0 {
		return storage.ErrConflict
	}
	delivered, err := m.Store.ListEvents(ctx, storage.EventFilter{GraphID: d.ExecutionGraphID, Types: []domain.EventType{domain.EvPullRequestOpened}, Limit: 1})
	if err != nil {
		return err
	}
	if len(delivered) > 0 {
		return fmt.Errorf("%w: the original task already delivered a pull request", storage.ErrConflict)
	}
	return nil
}
