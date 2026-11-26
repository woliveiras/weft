package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/taskauth"
)

func TestTaskStartReviewBindsAuthorizationRevision(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"task.start","task":"Editorial review"}`)
	if err != nil || plan.Kind != StartTask || plan.Task != "Editorial review" {
		t.Fatalf("start plan = %+v, %v", plan, err)
	}
	now := time.Now()
	admission := TaskAdmission{
		ID: "tsk_example", WorkflowID: "editorial", Version: 1, Revision: 2,
		Consent: taskauth.StartConsent{Mode: taskauth.Ask, Scope: "reviewed-scope", Revision: 1},
	}
	review, err := NewTaskStartReview("owner", "session", admission, now)
	if err != nil {
		t.Fatal(err)
	}
	review.TaskStart.Consent.Revision++
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed consent revision passed review: %v", err)
	}
	if _, err := decodePlan(`{"kind":"task.start","task":"Editorial review","confirm":"true"}`); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("untrusted plan confirmed a start: %v", err)
	}
}
