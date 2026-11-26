package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
)

func TestWorkflowControlReviewBindsActionNodeAndRun(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"workflow.control","controlWorkflow":"Review pipeline","controlAction":"pause","controlNode":"reviewer"}`)
	if err != nil || plan.Kind != ControlWorkflow || plan.Control.Node != "reviewer" || plan.Control.Action != "pause" {
		t.Fatalf("plan = %+v %v", plan, err)
	}
	n := time.Now()
	scope := engine.WorkflowControlScope{
		WorkflowID: "review", NodeID: "reviewer", Version: 3, RunID: "first-run", DesiredState: domain.GraphRunning,
	}
	review, err := NewWorkflowControlReview("owner", "session", WorkflowControl{Scope: scope, Action: "pause"}, n)
	if err != nil {
		t.Fatal(err)
	}
	if err := review.ValidateConfirmation("owner", "session", review.Digest, n); err != nil {
		t.Fatal(err)
	}
	review.WorkflowControl.Scope.RunID = "second-run"
	if err := review.ValidateConfirmation("owner", "session", review.Digest, n); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("a different run passed the review: %v", err)
	}
	scope.NodePaused = true
	if !scope.CanApply("resume") || scope.CanApply("pause") {
		t.Fatalf("invalid paused-node actions: %+v", scope)
	}
	scope.NodeID, scope.DesiredState = "", domain.GraphStopped
	if scope.CanApply("start") {
		t.Fatal("a draining graph was allowed to start")
	}
	scope.StoppedAndIdle = true
	if !scope.CanApply("start") || scope.CanApply("kill") {
		t.Fatalf("invalid stopped graph actions: %+v", scope)
	}
	for _, input := range []string{
		`{"kind":"workflow.control","controlWorkflow":"Review","controlAction":"disable"}`,
		`{"kind":"workflow.control","controlWorkflow":"Review","controlAction":"stop","task":"tsk_test"}`,
		`{"kind":"workflow.control","controlWorkflow":"Review","controlAction":"start","confirmed":"yes"}`,
	} {
		if _, err := decodePlan(input); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("accepted unscoped plan %s: %v", input, err)
		}
	}
}
