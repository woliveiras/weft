package chat

import (
	"errors"
	"testing"
	"time"
)

func TestWorkflowEnvironmentReviewBindsSavedVersionAndHash(t *testing.T) {
	for _, invalid := range []string{
		`{"kind":"workflow.prepare_environment","environmentWorkflow":"demo","force":"true"}`,
		`{"kind":"workflow.prepare_environment","environmentWorkflow":"demo","task":"tsk_other"}`,
		`{"kind":"workflow.control","controlWorkflow":"demo","controlAction":"start","environmentWorkflow":"demo"}`,
	} {
		if _, err := decodePlan(invalid); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("unsupported build intent: %v", err)
		}
	}
	const request = `{"kind":"workflow.prepare_environment","environmentWorkflow":"demo"}`
	if plan, err := decodePlan(request); err != nil || plan.EnvironmentWorkflow != "demo" {
		t.Fatalf("could not read planned build: %+v %v", plan, err)
	}
	input := WorkflowEnvironment{Workflow: "demo", Version: 2, WorkflowDigest: "workflow-digest", DefinitionHash: "environment-hash", Definition: `{"image":"fixture"}`}
	now := time.Now()
	review, err := NewWorkflowEnvironmentReview("owner", "session", input, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*WorkflowEnvironment){
		func(p *WorkflowEnvironment) { p.Version++ },
		func(p *WorkflowEnvironment) { p.DefinitionHash = "different" },
	} {
		changed, payload := *review, input
		change(&payload)
		changed.WorkflowEnvironment = &payload
		if err := changed.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
			t.Fatalf("review did not bind exact saved definition: %v", err)
		}
	}
	input.Definition = `{"setup":["token=synthetic-invalid-definition"]}`
	if _, err := NewWorkflowEnvironmentReview("owner", "session", input, now); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("credential-shaped definition crossed the Chat boundary: %v", err)
	}
}
