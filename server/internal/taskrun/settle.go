package taskrun

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// Settle is called by the engine's existing reconciliation cycle. Historical
// copies stay untouched until the operator explicitly stops their task.
// Reads (View and API GETs) never request a graph transition.
func (m Manager) Settle(ctx context.Context, graphID string) error {
	g, err := m.Store.GetGraph(ctx, graphID, 0)
	if err != nil {
		return err
	}
	id := g.Metadata.Labels[domain.LabelTask]
	if id == "" || graphID != ExecutionID(id) {
		return errors.New("invalid task graph ownership")
	}
	if g.Metadata.Labels["weft.task.lifecycle"] != "settled" {
		d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
		if err != nil {
			return err
		}
		// Historical runs only settle an explicit operator cancellation.
		if d.Status != "cancel_requested" {
			return nil
		}
		if d.ExecutionGraphID != graphID {
			return errors.New("invalid task graph binding")
		}
	}
	v, err := m.View(ctx, id)
	if err != nil {
		return err
	}
	switch v.Task.Status {
	case "draft", "preparing", "starting", "environment_pending", "environment_build_requested":
		return nil
	}
	if v.Task.Status == "cancel_requested" && v.Task.CancelOperation != "" {
		record, err := m.Store.GetGraphRecord(ctx, graphID)
		if err != nil {
			return err
		}
		if record.RunID != v.Task.CancelRunID {
			return storage.ErrConflict
		}
		if record.DesiredState != domain.GraphStopped ||
			(record.State != domain.GraphStopped && record.ControlOperation != v.Task.CancelOperation) || len(v.PendingApprovals) > 0 {
			return m.Engine.CancelReviewedTaskRun(ctx, graphID, v.Task.ID, v.Task.CancelRunID, v.Task.UpdatedBy, v.Task.CancelOperation)
		}
	}
	if !terminalOutcomeRecorded(v) {
		return nil
	}
	rec, err := m.Store.GetGraphRecord(ctx, graphID)
	if err != nil {
		return err
	}
	if v.State == "cancelled" && !cancellationConfirmed(rec, v) {
		return nil
	}
	if rec.DesiredState != domain.GraphStopped {
		return m.Engine.GraphAction(ctx, graphID, "stop", "system:task")
	}
	if rec.State != domain.GraphStopped || v.Task.Status == v.State {
		return nil
	}
	_, err = (taskconfig.Manager{Store: m.Store}).Transition(ctx, v.Task, v.State, "system:task")
	if errors.Is(err, storage.ErrConflict) {
		return nil
	} // recompute next cycle
	return err
}

// View only reports failed after pending approvals and active work are settled.
// A failed message gate may prevent every execution from starting. Preparation
// blocks still require an explicit operator stop when no execution was recorded.
func terminalOutcomeRecorded(v *View) bool {
	switch v.State {
	case "succeeded", "failed", "blocked", "interrupted", "cancelled":
		return len(v.Executions) > 0 || v.State == "failed" || v.State == "cancelled"
	default:
		return false
	}
}

// Cancellation is final only after both control and execution state agree.
// Bounded or pending history cannot authorize a persisted terminal status.
func cancellationConfirmed(rec *domain.GraphRecord, v *View) bool {
	if rec.State != domain.GraphStopped || rec.DesiredState != domain.GraphStopped || len(v.Executions) >= 1000 || len(v.PendingApprovals) > 0 {
		return false
	}
	for _, x := range v.Executions {
		if !x.Status.Terminal() {
			return false
		}
	}
	return true
}
