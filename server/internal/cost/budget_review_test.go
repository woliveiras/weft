package cost_test

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestReviewedMonthlyBudgetRaiseGuardsLedgerAndRecurringDefinition(t *testing.T) {
	_, budgets := budgetEnv(t, 0)
	ctx := t.Context()
	definition := []cost.Definition{{Scope: cost.Workflow, ID: "weekly", LimitMicros: 100}}
	if _, err := budgets.SetDefinitions(ctx, definition, "admin", 0); err != nil {
		t.Fatal(err)
	}
	reserve := func(execution string) {
		t.Helper()
		reservation, err := budgets.Reserve(ctx, cost.Target{Workflow: "weekly", Execution: execution}, cat(flat), "team", "openai", request(1))
		if err != nil || reservation == nil {
			t.Fatalf("reserve: %+v %v", reservation, err)
		}
	}
	reserve("first")
	now := time.Now()
	id := cost.MonthlyID("weekly", cost.Period(now))
	review := func() cost.RaiseTarget {
		t.Helper()
		target, err := budgets.ReviewRaise(ctx, cost.Workflow, id, now)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	action := review()
	reserve("second")
	if _, _, _, err := budgets.RaiseReviewed(ctx, action, 200, "admin", "stale-ledger"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("ledger revision did not gate the raise: %v", err)
	}
	action = review()
	if _, err := budgets.SetDefinitions(ctx, definition, "admin", action.DefinitionsRevision); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := budgets.RaiseReviewed(ctx, action, 200, "admin", "stale-definition"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("definition revision did not gate the raise: %v", err)
	}
	action = review()
	if got, previous, _, err := budgets.RaiseReviewed(ctx, action, 200, "admin", "reviewed"); err != nil || previous != 100 || got.Limit != 200 {
		t.Fatalf("reviewed raise: %+v %d %v", got, previous, err)
	}
	got, err := budgets.ReviewedRaiseReceipt(ctx, action, 200, "admin", "reviewed")
	if err != nil || got == nil || got.Revision != action.Revision+1 {
		t.Fatalf("receipt: %+v %v", got, err)
	}
	defs, revision, err := budgets.Definitions(ctx)
	if err != nil || revision != action.DefinitionsRevision || len(defs) != 1 || defs[0].LimitMicros != 100 {
		t.Fatalf("recurring definition changed: %+v %d %v", defs, revision, err)
	}
	if _, err := budgets.ReviewRaise(ctx, cost.Workflow, id, now.AddDate(0, 1, 0)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("closed period was offered for review: %v", err)
	}
}
