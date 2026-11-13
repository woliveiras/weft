package cost_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

// One micro-unit per token, so amounts are easy to follow.
var flat = cost.Catalogue{Version: 1, Currency: "USD", Unpriced: cost.UnpricedAllow, Prices: []cost.Price{{Provider: "team", Model: "*", InputMicrosPerMTok: 1_000_000, OutputMicrosPerMTok: 1_000_000}}}

func cat(c cost.Catalogue) func(context.Context) (cost.Catalogue, error) {
	return func(context.Context) (cost.Catalogue, error) { return c, nil }
}

// request estimates to 10 input tokens; with maxTokens it reserves 10+max.
func request(max int) *llm.ChatRequest {
	return &llm.ChatRequest{Model: "m", MaxTokens: max, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: strings.Repeat("x", 32)}}}
}

func budgetEnv(t *testing.T, limit int64) (storage.Store, cost.Budgets) {
	t.Helper()
	st := store(t)
	b := cost.Budgets{Store: st, Bus: events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))}
	if limit > 0 {
		if _, err := b.SetPolicy(ctx, cost.Policy{TaskRunBudgetMicros: limit}, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
	return st, b
}

func eventsOf(t *testing.T, st storage.Store, typ domain.EventType) []*domain.Event {
	t.Helper()
	evs, err := st.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{typ}})
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestNoBudgetMeansNoReservation(t *testing.T) {
	_, b := budgetEnv(t, 0)
	read := false
	r, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", func(context.Context) (cost.Catalogue, error) { read = true; return flat, nil }, "team", "openai", request(5))
	if r != nil || err != nil || read {
		t.Fatalf("reservation without budget: %v %v (catalogue read %v)", r, err, read)
	}
}

func TestTaskRunBudgetReservesSettlesWarnsAndRefuses(t *testing.T) {
	st, b := budgetEnv(t, 100)
	req := request(30)
	r1, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", req)
	if err != nil || r1 == nil || r1.Amount() != int64(llm.EstimateTokens(req))+30 {
		t.Fatalf("reserve: %+v %v", r1, err)
	}
	v, _ := b.Ledger(ctx, cost.TaskRun, "task-run-a")
	if v.Limit != 100 || v.Reserved != r1.Amount() || v.Charged != 0 || v.Currency != "USD" {
		t.Fatalf("after reserve: %+v", v)
	}
	// Reported usage settles below the reservation and frees the rest;
	// settlement is idempotent.
	resp := &llm.ChatResponse{Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}}
	if err := r1.Settle(ctx, resp); err != nil {
		t.Fatal(err)
	}
	if err := r1.Settle(ctx, &llm.ChatResponse{Usage: llm.Usage{InputTokens: 90}}); err != nil {
		t.Fatal(err)
	}
	v, _ = b.Ledger(ctx, cost.TaskRun, "task-run-a")
	if v.Charged != 15 || v.Reserved != 0 || v.Remaining != 85 || v.Estimated != 0 {
		t.Fatalf("after settle: %+v", v)
	}
	// Unknown usage keeps the whole reservation, marked estimated.
	r2, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", request(30))
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.Settle(ctx, nil); err != nil {
		t.Fatal(err)
	}
	v, _ = b.Ledger(ctx, cost.TaskRun, "task-run-a")
	if v.Charged != 15+r2.Amount() || v.Estimated != r2.Amount() {
		t.Fatalf("estimated: %+v", v)
	}
	// A released (never dispatched) reservation costs nothing.
	r3, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", request(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := r3.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if after, _ := b.Ledger(ctx, cost.TaskRun, "task-run-a"); after.Charged != v.Charged {
		t.Fatalf("release charged: %+v", after)
	}
	if n := len(eventsOf(t, st, domain.EvBudgetWarning)); n != 0 {
		t.Fatalf("warning below 80%%: %d", n)
	}
	// Crossing 80% (55 used + 30 reserved) warns once.
	r4, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", request(20))
	if err != nil {
		t.Fatal(err)
	}
	_ = r4.Settle(ctx, &llm.ChatResponse{Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}})
	if n := len(eventsOf(t, st, domain.EvBudgetWarning)); n != 1 {
		t.Fatalf("warnings = %d", n)
	}
	// A call that cannot fit is refused before dispatch; reached is
	// announced once, and later calls stay refused until a raise.
	for i := 0; i < 2; i++ {
		if _, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", request(30)); !errors.Is(err, cost.ErrBudgetReached) || !strings.Contains(err.Error(), "raise") {
			t.Fatalf("over budget: %v", err)
		}
	}
	if n := len(eventsOf(t, st, domain.EvBudgetReached)); n != 1 {
		t.Fatalf("reached events = %d", n)
	}
	if _, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", request(1)); !errors.Is(err, cost.ErrBudgetReached) {
		t.Fatalf("a reached budget stays closed to priced calls: %v", err)
	}
	// Free calls (a local plugin) still run.
	if r, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "local", "ollama", request(30)); err != nil || r.Amount() != 0 {
		t.Fatalf("free call at the limit: %+v %v", r, err)
	}
	// Another run has its own budget.
	if _, err := b.ReserveTaskRun(ctx, "task-run-b", "y1", cat(flat), "team", "openai", request(30)); err != nil {
		t.Fatalf("independent run: %v", err)
	}
}

func TestUnpricedCallsFollowThePolicyUnderABudget(t *testing.T) {
	_, b := budgetEnv(t, 100)
	allow := flat
	if r, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(allow), "claude", "anthropic", request(30)); err != nil || r.Amount() != 0 {
		t.Fatalf("allowed unpriced: %+v %v", r, err)
	}
	refuse := flat
	refuse.Unpriced = cost.UnpricedRefuse
	if _, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(refuse), "claude", "anthropic", request(30)); !errors.Is(err, cost.ErrUnpriced) {
		t.Fatalf("refused unpriced: %v", err)
	}
	// A budget whose catalogue cannot be read fails closed.
	if _, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", func(context.Context) (cost.Catalogue, error) { return cost.Catalogue{}, errors.New("store down") }, "team", "openai", request(1)); err == nil {
		t.Fatal("unreadable catalogue must refuse the call")
	}
	// A finite output allowance is imposed so the reservation is a ceiling.
	req := request(0)
	if _, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(allow), "local", "ollama", req); err != nil || req.MaxTokens != llm.DefaultEstimatedOutput {
		t.Fatalf("allowance = %d %v", req.MaxTokens, err)
	}
}

func TestConcurrentReservationsNeverOvershoot(t *testing.T) {
	_, b := budgetEnv(t, 1000)
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := int64(0)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := b.ReserveTaskRun(ctx, "task-run-c", "x", cat(flat), "team", "openai", request(40))
			if err == nil {
				mu.Lock()
				admitted += r.Amount()
				mu.Unlock()
			} else if !errors.Is(err, cost.ErrBudgetReached) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	v, _ := b.Ledger(ctx, cost.TaskRun, "task-run-c")
	if admitted == 0 || admitted > 1000 || v.Reserved != admitted {
		t.Fatalf("admitted %d, ledger %+v", admitted, v)
	}
}

func TestRaiseSchedulesResumeAndReconcileChargesOpenWork(t *testing.T) {
	_, b := budgetEnv(t, 50)
	if _, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", request(100)); !errors.Is(err, cost.ErrBudgetReached) {
		t.Fatalf("expected refusal: %v", err)
	}
	target := cost.BudgetGraph{Scope: cost.TaskRun, ID: "task-run-a", Graph: "task-run-a",
		GraphAction: cost.GraphAction{Action: cost.PauseGraph, RunID: "run-a", Actor: "system:budget"}}
	if err := b.MarkPaused(ctx, target, 50); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.Ledger(ctx, cost.TaskRun, "task-run-a"); !v.Paused || !v.Reached {
		t.Fatalf("paused view: %+v", v)
	}
	var ve *cost.ValidationError
	if _, _, _, err := b.Raise(ctx, cost.TaskRun, "task-run-a", 40, "alice"); !errors.As(err, &ve) {
		t.Fatalf("lower limit: %v", err)
	}
	if _, _, _, err := b.Raise(ctx, cost.TaskRun, "task-run-a", 500, "system:budget"); err == nil {
		t.Fatal("the system must not raise a budget")
	}
	if _, _, _, err := b.Raise(ctx, cost.TaskRun, "task-run-zz", 500, "alice"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing budget: %v", err)
	}
	v, previous, resume, err := b.Raise(ctx, cost.TaskRun, "task-run-a", 500, "alice")
	if err != nil || previous != 50 || len(resume) != 1 || resume[0].Graph != "task-run-a" ||
		resume[0].RunID != "run-a" || v.Limit != 500 || v.Reached || !v.Paused || v.RaisedBy != "alice" ||
		len(v.NotResumed) != 1 || v.NotResumed[0] != "task-run-a" {
		t.Fatalf("raise: %+v %+v %v", v, resume, err)
	}
	if err := b.MarkPaused(ctx, target, 50); err != nil {
		t.Fatal(err)
	}
	if changed, err := b.ResolveGraphAction(ctx, target, true); err != nil || changed {
		t.Fatalf("obsolete pause replaced a resume: %t %v", changed, err)
	}
	if changed, err := b.ResolveGraphAction(ctx, resume[0], false); err != nil || !changed {
		t.Fatalf("complete resume: %t %v", changed, err)
	}
	if v, err := b.Ledger(ctx, cost.TaskRun, target.ID); err != nil || v.Paused || len(v.NotResumed) != 0 {
		t.Fatalf("completed ledger: %+v %v", v, err)
	}
	r, err := b.ReserveTaskRun(ctx, "task-run-a", "x1", cat(flat), "team", "openai", request(100))
	if err != nil {
		t.Fatalf("after raise: %v", err)
	}
	if _, err := b.ReserveTaskRun(ctx, "task-run-a", "x2", cat(flat), "team", "openai", request(5)); err != nil {
		t.Fatal(err)
	}
	// x1 ended without settling (a restart): its reservation is charged in
	// full as estimated; x2 is still running and stays reserved.
	if err := b.Reconcile(ctx, func(_ context.Context, id string) bool { return id == "x1" }); err != nil {
		t.Fatal(err)
	}
	rv, _ := b.Ledger(ctx, cost.TaskRun, "task-run-a")
	if rv.Charged != r.Amount() || rv.Estimated != r.Amount() || rv.Reserved == 0 {
		t.Fatalf("reconciled: %+v", rv)
	}
}

func TestPolicyValidation(t *testing.T) {
	st, b := budgetEnv(t, 0)
	p, rev, err := b.Policy(ctx)
	if err != nil || rev != 0 || p.TaskRunBudgetMicros != 0 || p.WarningPercent != 80 {
		t.Fatalf("default policy: %+v %d %v", p, rev, err)
	}
	var ve *cost.ValidationError
	if _, err := b.SetPolicy(ctx, cost.Policy{TaskRunBudgetMicros: -1, WarningPercent: 120}, "admin", 0); !errors.As(err, &ve) || len(ve.Errors) != 2 {
		t.Fatalf("invalid policy: %v", err)
	}
	if _, err := b.SetPolicy(ctx, cost.Policy{TaskRunBudgetMicros: 5}, "admin", 7); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale policy: %v", err)
	}
	if p, err := b.SetPolicy(ctx, cost.Policy{TaskRunBudgetMicros: 5}, "admin", 0); err != nil || p.WarningPercent != 80 || p.UpdatedBy != "admin" {
		t.Fatalf("policy: %+v %v", p, err)
	}
	cur, _ := st.GetSetting(ctx, cost.PolicyKey)
	if _, err := st.PutSetting(ctx, cost.PolicyKey, []byte(`{"taskRunBudgetMicros":-3,"warningPercent":80}`), cur.Revision, "x"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Policy(ctx); err == nil {
		t.Fatal("corrupt policy must fail")
	}
}
