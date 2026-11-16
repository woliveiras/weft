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
	if g.Metadata.Labels["weft.task.lifecycle"] != "settled" {
		return nil
	}
	id := g.Metadata.Labels[domain.LabelTask]
	if id == "" || graphID != ExecutionID(id) {
		return errors.New("invalid task graph ownership")
	}
	v, err := m.View(ctx, id)
	if err != nil {
		return err
	}
	switch v.Task.Status {
	case "draft", "preparing", "starting", "environment_pending", "environment_build_requested":
		return nil
	}
	switch v.State {
	case "succeeded", "failed", "blocked", "interrupted", "cancelled":
	default:
		return nil
	}
	rec, err := m.Store.GetGraphRecord(ctx, graphID)
	if err != nil {
		return err
	}
	// A preparation block has no terminal execution and still requires an
	// explicit operator stop. A pending approval or budget pause is not done.
	if len(v.Executions) == 0 && v.State != "cancelled" {
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
