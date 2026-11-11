//go:build e2e

package main

import (
	"context"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
)

// Saved crash leftovers exercise the real startup recovery and API/UI without
// exposing a production fault-injection endpoint or killing a user's service.
func prepareBudgetRecovery(ctx context.Context, a *app.App, instance string) error {
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "budget-recovery", Name: "Budget recovery"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "parent", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}}}}}
	if _, _, err := a.Engine.SaveGraph(ctx, g, 0, "fixture"); err != nil {
		return err
	}
	m := delegation.Manager{Store: a.Store}
	for _, id := range []string{"budget-crashed", "budget-active", "budget-retained"} {
		x := &domain.Execution{ID: id, GraphID: g.Metadata.ID, GraphVer: 1, NodeID: "parent", Instance: instance, Status: domain.ExecRunning, StartedAt: time.Now().UTC()}
		if id == "budget-active" {
			x.Instance = "other-worker"
		}
		if id == "budget-retained" {
			old := time.Now().UTC().Add(-31 * 24 * time.Hour)
			x.StartedAt = old
			x.CompletedAt = &old
			x.Status = domain.ExecInterrupted
		}
		if err := a.Store.PutExecution(ctx, x); err != nil {
			return err
		}
		if err := m.ReserveTokens(ctx, id, "pending", 100, 40); err != nil {
			return err
		}
		if err := m.ReserveTokens(ctx, id, "dispatched", 100, 30); err != nil {
			return err
		}
		if err := m.DispatchTokens(ctx, id, "dispatched"); err != nil {
			return err
		}
	}
	return nil
}
