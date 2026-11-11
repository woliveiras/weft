package engine_test

import (
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue/memory"
)

func TestTreeBudgetRecoveryAfterStorageReopen(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	ctx := t.Context()
	m := delegation.Manager{Store: s}
	for _, id := range []string{"crashed", "other-worker"} {
		instance := "restarted"
		if id == "other-worker" {
			instance = "other"
		}
		if err := s.PutExecution(ctx, &domain.Execution{ID: id, GraphID: "g", Status: domain.ExecRunning, Instance: instance, StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		for _, call := range []string{"pending", "dispatched"} {
			if err := m.ReserveTokens(ctx, id, call, 100, 30); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.DispatchTokens(ctx, id, "dispatched"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(t, dir)
	r := newRig(t, reopened, memory.New(), dir, "restarted")
	m = delegation.Manager{Store: reopened}
	x, err := reopened.GetExecution(ctx, "crashed")
	if err != nil || x.Status != domain.ExecInterrupted {
		t.Fatal(x, err)
	}
	v, err := m.Budget(ctx, "crashed")
	if err != nil || v.Reserved != 0 || v.Charged != 30 || v.Estimated != 30 {
		t.Fatal(v, err)
	}
	v, err = m.Budget(ctx, "other-worker")
	if err != nil || v.Reserved != 60 {
		t.Fatal("active foreign worker changed", v, err)
	}
	if err = m.ReconcileBudgets(r.ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	v, err = m.Budget(ctx, "crashed")
	if err != nil || v.Charged != 30 {
		t.Fatal(v, err)
	}
}
