package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestTriggerControlBindsNativeConsentAndRejectsUnsafeQuery(t *testing.T) {
	good := `{"kind":"trigger.control","triggerTarget":"ready","triggerAction":"resume"}`
	plan, err := decodePlan(good)
	if err != nil || plan.Trigger.Target != "ready" || plan.Trigger.Action != "resume" {
		t.Fatalf("invalid trigger plan: %+v %v", plan, err)
	}
	for _, raw := range []string{
		`{"kind":"trigger.control","triggerTarget":"ready","triggerAction":"stop"}`,
		`{"kind":"workflow.control","controlWorkflow":"review","controlAction":"pause","triggerTarget":"ready"}`,
	} {
		if _, err := decodePlan(raw); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("ambiguous intent accepted: %v", err)
		}
	}
	scope := trigger.IntakeScope{ID: "trg_ready", Name: "Ready", Action: "resume", Actor: "admin", Workflow: "fixture", WorkflowVersion: 1,
		Connection: "tracker", ConsentScope: "old-scope", Configuration: "revision", CurrentTask: "tsk_current"}
	now := time.Now()
	review, err := NewTriggerControlReview("owner", "session", scope, now)
	if err != nil {
		t.Fatal(err)
	}
	changed := scope
	changed.ConsentScope = "different-scope"
	review.TriggerControl = &changed
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("consent revision was not bound: %v", err)
	}
	scope.Query = "api_key=synthetic-invalid-query"
	if _, err := NewTriggerControlReview("owner", "session", scope, now); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("credential-shaped query entered history: %v", err)
	}
}
