package delegation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestCloseBudget(t *testing.T) {
	b := &Budget{Limit: 100, Entries: map[string]Reservation{
		"pending": {Amount: 30}, "unknown": {Amount: 40, Dispatched: true},
		"known": {Amount: 20, Dispatched: true, Settled: true, Charged: 7},
	}}
	closeBudget(b)
	want := BudgetView{100, 47, 0, 40}
	if !b.Closed || b.View() != want {
		t.Fatal(b)
	}
	closeBudget(b)
	if b.View() != want {
		t.Fatal("duplicate recovery changed usage", b)
	}
}

func TestBudgetReconciliationRetentionAndFencing(t *testing.T) {
	m := budgetManager(t)
	ctx := t.Context()
	now := time.Now().UTC()
	for _, id := range []string{"terminal", "active", "missing-time"} {
		x := &domain.Execution{ID: id, GraphID: "g", Status: domain.ExecInterrupted, CompletedAt: &now}
		if id == "active" {
			x.Status = domain.ExecRunning
		}
		if id == "missing-time" {
			x.CompletedAt = nil
		}
		if err := m.Store.PutExecution(ctx, x); err != nil {
			t.Fatal(err)
		}
		if err := m.ReserveTokens(ctx, id, "call", 100, 40); err != nil {
			t.Fatal(err)
		}
		if err := m.DispatchTokens(ctx, id, "call"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.ReconcileBudgets(ctx, now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	v, err := m.Budget(ctx, "terminal")
	if err != nil || *v != (BudgetView{100, 40, 0, 40}) {
		t.Fatal(v, err)
	}
	v, err = m.Budget(ctx, "active")
	if err != nil || v.Reserved != 40 {
		t.Fatal(v, err)
	}
	before, err := m.Store.GetSetting(ctx, BudgetPrefix+"terminal")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ReconcileBudgets(ctx, now.Add(BudgetDetailRetention-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := m.Store.GetSetting(ctx, BudgetPrefix+"terminal")
	if unchanged.Revision != before.Revision {
		t.Fatal("non-idempotent sweep")
	}
	if err = m.ReconcileBudgets(ctx, now.Add(BudgetDetailRetention)); err != nil {
		t.Fatal(err)
	}
	b, _, err := m.readBudget(ctx, "terminal")
	if err != nil || b.Compacted == nil || len(b.Entries) != 0 || b.View() != (BudgetView{100, 40, 0, 40}) {
		t.Fatal(b, err)
	}
	for _, id := range []string{"call", "new-call"} {
		if err = m.ReserveTokens(ctx, "terminal", id, 100, 1); !errors.Is(err, storage.ErrConflict) {
			t.Fatal("closed credit reopened", err)
		}
	}
	if err = m.DispatchTokens(ctx, "terminal", "call"); err == nil {
		t.Fatal("late dispatch allowed")
	}
	if err = m.SettleTokens(ctx, "terminal", "call", 1, true); err == nil {
		t.Fatal("compacted callback accepted")
	}
	b, _, err = m.readBudget(ctx, "missing-time")
	if err != nil || b.Compacted != nil {
		t.Fatal("compacted undated history", err)
	}
	v, err = m.Budget(ctx, "active")
	if err != nil || v.Reserved != 40 {
		t.Fatal("aged active credit refunded", v, err)
	}
}

type sweepFailure struct {
	storage.Store
	operation string
}

func (s sweepFailure) ListSettings(ctx context.Context, p string) ([]*storage.Setting, error) {
	if s.operation == "list" {
		return nil, errors.New("unavailable")
	}
	return s.Store.ListSettings(ctx, p)
}
func (s sweepFailure) GetExecution(ctx context.Context, id string) (*domain.Execution, error) {
	if s.operation == "execution" {
		return nil, errors.New("unavailable")
	}
	return s.Store.GetExecution(ctx, id)
}
func (s sweepFailure) PutSetting(ctx context.Context, k string, v []byte, r int64, by string) (int64, error) {
	if s.operation == "write" {
		return 0, errors.New("unavailable")
	}
	return s.Store.PutSetting(ctx, k, v, r, by)
}
func TestBudgetReconciliationFailsClosed(t *testing.T) {
	m := budgetManager(t)
	ctx := t.Context()
	if err := m.ReserveTokens(ctx, "root", "call", 100, 40); err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileBudgets(ctx, time.Now()); err == nil {
		t.Fatal("missing execution ignored")
	}
	if err := m.Store.PutExecution(ctx, &domain.Execution{ID: "root", GraphID: "g", Status: domain.ExecInterrupted}); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"list", "execution", "write"} {
		if err := (Manager{Store: sweepFailure{m.Store, op}}).ReconcileBudgets(ctx, time.Now()); err == nil {
			t.Fatal(op)
		}
		v, err := m.Budget(ctx, "root")
		if err != nil || v.Reserved != 40 {
			t.Fatal(op, v, err)
		}
	}
	if err := m.ReconcileBudgets(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	v, err := m.Budget(ctx, "root")
	if err != nil || v.Reserved != 0 || v.Charged != 0 {
		t.Fatal(v, err)
	}
}

func TestBudgetRecoveryRacesDispatch(t *testing.T) {
	for i := 0; i < 10; i++ {
		m := budgetManager(t)
		ctx := t.Context()
		if err := m.Store.PutExecution(ctx, &domain.Execution{ID: "root", GraphID: "g", Status: domain.ExecInterrupted}); err != nil {
			t.Fatal(err)
		}
		if err := m.ReserveTokens(ctx, "root", "call", 100, 40); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- m.DispatchTokens(ctx, "root", "call") }()
		if err := m.ReconcileBudgets(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		dispatchErr := <-done
		v, err := m.Budget(ctx, "root")
		if err != nil || v.Reserved != 0 {
			t.Fatal(v, err)
		}
		if dispatchErr == nil && v.Charged != 40 {
			t.Fatal("dispatched work refunded", v)
		}
		if dispatchErr != nil && (!errors.Is(dispatchErr, storage.ErrConflict) || v.Charged != 0) {
			t.Fatal(v, dispatchErr)
		}
	}
}

func TestBudgetRecoveryRetriesCompactionFailureAndPreservesKnownUsage(t *testing.T) {
	m := budgetManager(t)
	ctx := t.Context()
	old := time.Now().UTC().Add(-BudgetDetailRetention)
	if err := m.Store.PutExecution(ctx, &domain.Execution{ID: "root", GraphID: "g", Status: domain.ExecSucceeded, CompletedAt: &old}); err != nil {
		t.Fatal(err)
	}
	if err := m.ReserveTokens(ctx, "root", "known", 100, 40); err != nil {
		t.Fatal(err)
	}
	if err := m.DispatchTokens(ctx, "root", "known"); err != nil {
		t.Fatal(err)
	}
	if err := m.SettleTokens(ctx, "root", "known", 7, true); err != nil {
		t.Fatal(err)
	}
	if err := (Manager{Store: sweepFailure{m.Store, "write"}}).ReconcileBudgets(ctx, time.Now()); err == nil {
		t.Fatal("compaction write failure hidden")
	}
	b, _, err := m.readBudget(ctx, "root")
	if err != nil || len(b.Entries) != 1 || b.Compacted != nil {
		t.Fatal(b, err)
	}
	if err = m.ReconcileBudgets(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, rev, err := m.readBudget(ctx, "root")
	if err != nil || b.View() != (BudgetView{100, 7, 0, 0}) || b.Compacted == nil {
		t.Fatal(b, err)
	}
	if err = m.ReconcileBudgets(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	_, after, err := m.readBudget(ctx, "root")
	if err != nil || rev != after {
		t.Fatal("repeated compaction mutated tombstone", err)
	}
}

func TestBudgetRecoveryCorruptionDoesNotBlockHealthyTrees(t *testing.T) {
	m := budgetManager(t)
	ctx := t.Context()
	for _, id := range []string{"bad", "healthy"} {
		if err := m.Store.PutExecution(ctx, &domain.Execution{ID: id, GraphID: "g", Status: domain.ExecInterrupted}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Store.PutSetting(ctx, BudgetPrefix+"bad", []byte(`{"limit":100,"closed":false,"entries":{},"compacted":{"limit":100}}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := m.ReserveTokens(ctx, "healthy", "pending", 100, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileBudgets(ctx, time.Now()); err == nil {
		t.Fatal("corruption hidden")
	}
	v, err := m.Budget(ctx, "healthy")
	if err != nil || v.Reserved != 0 {
		t.Fatal("healthy tree not recovered", v, err)
	}
}
