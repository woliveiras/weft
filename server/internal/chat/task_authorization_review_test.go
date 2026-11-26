package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskauth"
)

func TestTaskAuthorizationBindsModeRevisionAndSafeScope(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"task.authorize","task":"Editorial","taskAuthorizationMode":"ask"}`)
	if err != nil || plan.Kind != AuthorizeTask || plan.TaskAuthorizationMode != taskauth.Ask {
		t.Fatalf("task authorization plan: %+v %v", plan, err)
	}
	now := time.Now()
	p := TaskAuthorization{
		ID: "tsk_reviewed", TaskRevision: 2, WorkflowID: "editorial", WorkflowVersion: 3,
		Current: taskauth.StartConsent{Mode: taskauth.Workspace, Scope: "scope", Revision: 4},
		Mode:    plan.TaskAuthorizationMode,
	}
	review, err := NewTaskAuthorizationReview("owner", "session", p, now)
	if err != nil {
		t.Fatal(err)
	}
	review.TaskAuthorization.Current.Revision++
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("different authorization revision passed: %v", err)
	}
	p.Agents = []taskauth.AgentScope{{ID: "worker", Grants: []domain.ToolGrant{{Name: "shell.exec", Commands: []string{"password=fixture-value"}}}}}
	if _, err := NewTaskAuthorizationReview("owner", "session", p, now); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("credential-shaped scope persisted: %v", err)
	}
	if _, err := decodePlan(`{"kind":"task.start","task":"Editorial","taskAuthorizationMode":"full"}`); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("start inferred authorization: %v", err)
	}
}
