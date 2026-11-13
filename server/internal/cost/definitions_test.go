package cost_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestDefinitionsValidation(t *testing.T) {
	_, b := budgetEnv(t, 0)
	defs, rev, err := b.Definitions(ctx)
	if err != nil || rev != 0 || len(defs) != 0 {
		t.Fatalf("default: %+v %d %v", defs, rev, err)
	}
	_, err = b.SetDefinitions(ctx, []cost.Definition{
		{Scope: "team", ID: "x", LimitMicros: 1},
		{Scope: cost.Installation, ID: "everything", LimitMicros: 1},
		{Scope: cost.User, ID: "", LimitMicros: 1},
		{Scope: cost.Project, ID: "p@1", LimitMicros: 0},
		{Scope: cost.Workflow, ID: "wf", LimitMicros: 1, WarningPercent: 101, InterruptPercent: 100},
		{Scope: cost.Workflow, ID: "wf", LimitMicros: 1},
	}, "admin", 0)
	var ve *cost.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want validation error, got %v", err)
	}
	paths := map[string]bool{}
	for _, i := range ve.Errors {
		paths[i.Path] = true
	}
	for _, p := range []string{"budgets[0].scope", "budgets[1].id", "budgets[2].id", "budgets[3].id", "budgets[3].limitMicros", "budgets[4].warningPercent", "budgets[4].interruptPercent", "budgets[5]"} {
		if !paths[p] {
			t.Errorf("missing %s in %+v", p, ve.Errors)
		}
	}
	got, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Installation, LimitMicros: 100}}, "admin", 0)
	if err != nil || got[0].ID != "*" {
		t.Fatalf("installation id defaults to *: %+v %v", got, err)
	}
	if _, err := b.SetDefinitions(ctx, nil, "admin", 99); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
}

func TestMonthlyBudgetsApplyByScopeAndRollBackOnRefusal(t *testing.T) {
	_, b := budgetEnv(t, 0)
	if _, err := b.SetDefinitions(ctx, []cost.Definition{
		{Scope: cost.Installation, LimitMicros: 1000},
		{Scope: cost.User, ID: "alice", LimitMicros: 50},
		{Scope: cost.Project, ID: "prj_1", LimitMicros: 1000},
		{Scope: cost.Workflow, ID: "factory", LimitMicros: 1000},
	}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	period := cost.Period(time.Now())
	alice := cost.Target{Graph: "task-run-1", Execution: "x1", User: "alice", Project: "prj_1", Workflow: "factory"}
	r, err := b.Reserve(ctx, alice, cat(flat), "team", "openai", request(30))
	if err != nil || r == nil {
		t.Fatalf("reserve: %v", err)
	}
	for _, key := range []struct{ scope, id string }{{cost.Installation, "*"}, {cost.User, "alice"}, {cost.Project, "prj_1"}, {cost.Workflow, "factory"}} {
		v, err := b.Ledger(ctx, key.scope, cost.MonthlyID(key.id, period))
		if err != nil || v == nil || v.Reserved != r.Amount() {
			t.Fatalf("%s ledger: %+v %v", key.scope, v, err)
		}
	}
	// Someone else is not charged to alice's budget.
	bob := cost.Target{Graph: "task-run-2", Execution: "y1", User: "bob"}
	if _, err := b.Reserve(ctx, bob, cat(flat), "team", "openai", request(30)); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.Ledger(ctx, cost.User, cost.MonthlyID("bob", period)); v != nil {
		t.Fatalf("bob has no budget: %+v", v)
	}
	// Alice's own budget refuses the next call; the parts already reserved
	// on the other budgets are released, so nothing is held.
	before, _ := b.Ledger(ctx, cost.Installation, cost.MonthlyID("*", period))
	var reached *cost.ReachedError
	if _, err := b.Reserve(ctx, alice, cat(flat), "team", "openai", request(30)); !errors.As(err, &reached) || reached.Scope != cost.User {
		t.Fatalf("user budget: %v", err)
	}
	after, _ := b.Ledger(ctx, cost.Installation, cost.MonthlyID("*", period))
	if after.Reserved != before.Reserved || after.Charged != before.Charged {
		t.Fatalf("rollback: before %+v after %+v", before, after)
	}
	// New runs for alice are refused; bob's are not.
	if got, err := b.Check(ctx, cost.Target{User: "alice"}); err != nil || got == nil || got.Scope != cost.User {
		t.Fatalf("check alice: %+v %v", got, err)
	}
	if got, err := b.Check(ctx, cost.Target{User: "bob"}); err != nil || got != nil {
		t.Fatalf("check bob: %+v %v", got, err)
	}
	// An administrator raising the definition applies it to this month.
	if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.User, ID: "alice", LimitMicros: 500}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reserve(ctx, alice, cat(flat), "team", "openai", request(30)); err != nil {
		t.Fatalf("after raising the definition: %v", err)
	}
}

func TestInterruptLevelStopsARunawayCall(t *testing.T) {
	_, b := budgetEnv(t, 0)
	if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Installation, LimitMicros: 40, InterruptPercent: 110}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	r, err := b.Reserve(ctx, cost.Target{Execution: "x1"}, cat(flat), "team", "openai", request(30))
	if err != nil {
		t.Fatal(err)
	}
	// The provider reported far more than the estimate (40): 100 > 110% of 40.
	err = r.Settle(ctx, &llm.ChatResponse{Usage: llm.Usage{InputTokens: 90, OutputTokens: 10}})
	var interrupt *cost.InterruptError
	if !errors.As(err, &interrupt) || !errors.Is(err, cost.ErrInterrupted) || interrupt.Scope != cost.Installation {
		t.Fatalf("interrupt: %v", err)
	}
	v, _ := b.Ledger(ctx, cost.Installation, cost.MonthlyID("*", cost.Period(time.Now())))
	if v.Charged != 100 {
		t.Fatalf("the overrun is still charged: %+v", v)
	}
	// Without an interrupt level an overrun only charges.
	_, b2 := budgetEnv(t, 0)
	if _, err := b2.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Installation, LimitMicros: 40}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	r2, _ := b2.Reserve(ctx, cost.Target{Execution: "x1"}, cat(flat), "team", "openai", request(30))
	if err := r2.Settle(ctx, &llm.ChatResponse{Usage: llm.Usage{InputTokens: 90, OutputTokens: 10}}); err != nil {
		t.Fatalf("no interrupt level: %v", err)
	}
}

type admissionReadFailure struct {
	storage.Store
	key string
	err error
}

func (s admissionReadFailure) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if key == s.key {
		return nil, s.err
	}
	return s.Store.GetSetting(ctx, key)
}

func TestCheckAdmissionUsesEffectiveLimitAndPropagatesLedgerErrors(t *testing.T) {
	st, budgets := budgetEnv(t, 0)
	ctx := t.Context()
	id := cost.MonthlyID("alice", cost.Period(time.Now()))
	key := cost.BudgetPrefix + cost.User + "/" + id
	raw := []byte(fmt.Sprintf(`{"scope":"user","id":%q,"limit":20,"compacted":8,"entries":{"pending":{"amount":2}}}`, id))
	rev, err := st.PutSetting(ctx, key, raw, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	setLimit := func(limit int64) {
		t.Helper()
		if _, err := budgets.SetDefinitions(ctx, []cost.Definition{{Scope: cost.User, ID: "alice", LimitMicros: limit}}, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
	setLimit(10)
	target := cost.Target{User: "alice"}
	var reached *cost.ReachedError
	if err := budgets.CheckAdmission(ctx, target); !errors.As(err, &reached) ||
		reached.ID != id || reached.View.Limit != 10 || reached.View.Remaining != 0 {
		t.Fatalf("expected the reached monthly ledger: %+v %v", reached, err)
	}
	setLimit(11)
	if err := budgets.CheckAdmission(ctx, target); err != nil {
		t.Fatalf("expected headroom: %v", err)
	}
	if row, err := st.GetSetting(ctx, key); err != nil || row.Revision != rev || !bytes.Equal(row.Value, raw) {
		t.Fatalf("admission changed ledger: %+v %v", row, err)
	}
	unavailable := errors.New("ledger unavailable")
	budgets.Store = admissionReadFailure{Store: st, key: key, err: unavailable}
	if err := budgets.CheckAdmission(ctx, target); !errors.Is(err, unavailable) {
		t.Fatalf("expected dependency failure: %v", err)
	}
}
