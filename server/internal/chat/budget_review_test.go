package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
)

func TestBudgetRaiseIntentRequiresAbsoluteExactAmount(t *testing.T) {
	good := `{"kind":"budget.raise","budgetScope":"workflow","budgetTarget":"weekly-review","budgetAmount":"2.004005","budgetCurrency":"USD"}`
	plan, err := decodePlan(good)
	if err != nil || plan.Budget.LimitMicros() != 2_004_005 {
		t.Fatalf("decode: %+v %v", plan, err)
	}
	for _, raw := range []string{
		`{"kind":"budget.raise","budgetScope":"workflow","budgetAmount":"2","budgetCurrency":"USD"}`,
		`{"kind":"budget.raise","budgetScope":"installation","budgetAmount":"1.0000001","budgetCurrency":"USD"}`,
		`{"kind":"budget.raise","budgetScope":"installation","budgetAmount":"1000000000.000001","budgetCurrency":"USD"}`,
		`{"kind":"budget.raise","budgetScope":"installation","budgetAmount":"+1","budgetCurrency":"USD"}`,
		`{"kind":"budget.raise","budgetScope":"user","budgetTarget":"editor@2026-01","budgetAmount":"1","budgetCurrency":"USD"}`,
		`{"kind":"budget.raise","budgetScope":"run","budgetTarget":"review","budgetAmount":"1"}`,
		`{"kind":"installation.plan","document":"test","budgetAmount":"1"}`,
	} {
		if _, err := decodePlan(raw); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("accepted %s: %v", raw, err)
		}
	}
}

func TestBudgetRaiseConfirmationBindsAmountActorAndBothRevisions(t *testing.T) {
	now := time.Now()
	value := BudgetRaise{Target: cost.RaiseTarget{Scope: cost.Workflow, ID: "weekly@2026-11", Currency: "USD", Limit: 1, Revision: 5, DefinitionsRevision: 8},
		Subject: "weekly", Actor: "admin", Limit: 2}
	review, err := NewBudgetRaiseReview("owner", "session", value, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BudgetRaise){
		func(r *BudgetRaise) { r.Limit++ },
		func(r *BudgetRaise) { r.Actor = "different" },
		func(r *BudgetRaise) { r.Target.Scope = cost.User },
		func(r *BudgetRaise) { r.Target.Revision++ },
		func(r *BudgetRaise) { r.Target.DefinitionsRevision++ },
	} {
		altered, input := *review, value
		mutate(&input)
		altered.BudgetRaise = &input
		if err := altered.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
			t.Errorf("mutated confirmation: %v", err)
		}
	}
}
