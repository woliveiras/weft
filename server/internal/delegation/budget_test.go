package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func budgetManager(t *testing.T) Manager {
	t.Helper()
	s, e := sqlstore.OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "budget.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e = s.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	return Manager{Store: s}
}
func TestBudgetView(t *testing.T) {
	b := Budget{Limit: 100, Entries: map[string]Reservation{"parent": {Amount: 20, Settled: true, Charged: 7}, "child": {Amount: 30}, "failed": {Amount: 10, Settled: true, Charged: 10, Estimated: true}}}
	if v := b.View(); v != (BudgetView{100, 17, 30, 10}) {
		t.Fatal(v)
	}
}
func TestBudgetLifecycle(t *testing.T) {
	m := budgetManager(t)
	ctx := t.Context()
	if e := m.ReserveTokens(ctx, "root", "parent", 100, 60); e != nil {
		t.Fatal(e)
	}
	if e := m.ReserveTokens(ctx, "root", "child", 100, 50); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	if e := m.DispatchTokens(ctx, "root", "parent"); e != nil {
		t.Fatal(e)
	}
	if e := m.SettleTokens(ctx, "root", "parent", 20, true); e != nil {
		t.Fatal(e)
	}
	if e := m.SettleTokens(ctx, "root", "parent", 90, true); e != nil {
		t.Fatal(e)
	}
	if e := m.ReserveTokens(ctx, "root", "child", 100, 50); e != nil {
		t.Fatal(e)
	}
	if e := m.SettleTokens(ctx, "root", "child", 0, false); e != nil {
		t.Fatal(e)
	}
	v, e := m.Budget(ctx, "root")
	if e != nil || *v != (BudgetView{100, 20, 0, 0}) {
		t.Fatal(v, e)
	}
	if e = m.ReserveTokens(ctx, "root", "child", 100, 50); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("reused settled identity", e)
	}
	if e = m.DispatchTokens(ctx, "root", "child"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("dispatched released reservation", e)
	}
	if e = m.ReserveTokens(ctx, "root", "unknown", 100, 30); e != nil {
		t.Fatal(e)
	}
	if e = m.DispatchTokens(ctx, "root", "unknown"); e != nil {
		t.Fatal(e)
	}
	if e = m.SettleTokens(ctx, "root", "unknown", 0, false); e != nil {
		t.Fatal(e)
	}
	v, e = m.Budget(ctx, "root")
	if e != nil || *v != (BudgetView{100, 50, 0, 30}) {
		t.Fatal(v, e)
	}
	// A later policy increase cannot replenish a tree's original ceiling.
	if e = m.ReserveTokens(ctx, "root", "too-large", 200, 51); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	if e = m.ReserveTokens(ctx, "other", "call", 100, 100); e != nil {
		t.Fatal("unrelated tree affected", e)
	}
}
func TestBudgetConcurrentManagersAndRestart(t *testing.T) {
	m := budgetManager(t)
	ctx := t.Context()
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i))
			e := (Manager{Store: m.Store}).ReserveTokens(ctx, "root", id, 100, 20)
			if e == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			} else if !errors.Is(e, ErrBudget) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if admitted != 5 {
		t.Fatal(admitted)
	}
	v, e := (Manager{Store: m.Store}).Budget(ctx, "root")
	if e != nil || v.Reserved != 100 {
		t.Fatal(v, e)
	}
	// No process-local accounting; recreating the manager cannot refund held calls.
	if e = (Manager{Store: m.Store}).ReserveTokens(ctx, "root", "restart", 100, 1); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
}

type budgetFailure struct{ storage.Store }

func (budgetFailure) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, errors.New("store unavailable")
}
func TestBudgetFailsClosed(t *testing.T) {
	m := Manager{Store: budgetFailure{}}
	if e := m.ReserveTokens(t.Context(), "root", "call", 100, 10); e == nil {
		t.Fatal("storage error ignored")
	}
	for _, amount := range []int{0, -1, MaxTreeTokens + 1} {
		if e := m.ReserveTokens(t.Context(), "root", "call", 100, amount); e == nil {
			t.Fatal(amount)
		}
	}
	real := budgetManager(t)
	if _, e := real.Store.PutSetting(t.Context(), BudgetPrefix+"root", []byte(`{}`), 0, "test"); e != nil {
		t.Fatal(e)
	}
	if _, e := real.Budget(t.Context(), "root"); e == nil {
		t.Fatal("corruption ignored")
	}
}
func TestLegacyPolicyBudgetDefault(t *testing.T) {
	m := budgetManager(t)
	raw, _ := json.Marshal(DefaultPolicy())
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	delete(fields, "maxTreeTokens")
	raw, _ = json.Marshal(fields)
	if _, e := m.Store.PutSetting(t.Context(), PolicyKey, raw, 0, "legacy"); e != nil {
		t.Fatal(e)
	}
	p, e := m.Policy(t.Context())
	if e != nil || p.Policy.MaxTreeTokens != DefaultTreeTokens {
		t.Fatal(p, e)
	}
}

type budgetWriteFailure struct{ storage.Store }

func (budgetWriteFailure) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	return 0, errors.New("write unavailable")
}
func TestBudgetFailedSettlementPreservesReservationAndOverrunBlocksAdmission(t *testing.T) {
	m := budgetManager(t)
	ctx := t.Context()
	if e := m.ReserveTokens(ctx, "root", "call", 100, 40); e != nil {
		t.Fatal(e)
	}
	if e := m.DispatchTokens(ctx, "root", "call"); e != nil {
		t.Fatal(e)
	}
	broken := Manager{Store: budgetWriteFailure{m.Store}}
	if e := broken.SettleTokens(ctx, "root", "call", 10, true); e == nil {
		t.Fatal("write failure ignored")
	}
	v, e := m.Budget(ctx, "root")
	if e != nil || v.Reserved != 40 || v.Charged != 0 {
		t.Fatal(v, e)
	}
	if e = m.SettleTokens(ctx, "root", "call", 120, true); e != nil {
		t.Fatal(e)
	}
	if e = m.ReserveTokens(ctx, "root", "next", 100, 1); !errors.Is(e, ErrBudget) {
		t.Fatal("reported overrun allowed another call", e)
	}
}

func TestBudgetSurvivesStorageReopen(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "restart.db")
	first, e := sqlstore.OpenSQLite(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	if e = first.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = (Manager{Store: first}).ReserveTokens(ctx, "root", "held", 100, 70); e != nil {
		t.Fatal(e)
	}
	if e = first.Close(); e != nil {
		t.Fatal(e)
	}
	second, e := sqlstore.OpenSQLite(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	m := Manager{Store: second}
	v, e := m.Budget(ctx, "root")
	if e != nil || v.Reserved != 70 {
		t.Fatal(v, e)
	}
	if e = m.ReserveTokens(ctx, "root", "other", 100, 31); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	if e = m.SettleTokens(ctx, "root", "held", 0, false); e != nil {
		t.Fatal(e)
	}
	v, e = m.Budget(ctx, "root")
	if e != nil || v.Reserved != 0 || v.Charged != 0 {
		t.Fatal(v, e)
	}
}
