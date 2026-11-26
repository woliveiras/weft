package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
)

func TestWorkflowScaleSealsModeRunAndReplicaCount(t *testing.T) {
	const raw = `{"kind":"workflow.scale","scaleWorkflow":"news","scaleNode":"reader","scaleSize":"2","scaleMode":"autoscale"}`
	plan, err := decodePlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	scope := engine.WorkflowScaleScope{WorkflowControlScope: engine.WorkflowControlScope{
		WorkflowID: "news", NodeID: "reader", Version: 1, RunID: "run-1", DesiredState: domain.GraphRunning,
	}, Autoscale: true, Minimum: 1, Current: 3}
	if count, ok := plan.Scale.SizeFor(scope); !ok || count != 2 {
		t.Fatalf("size did not resolve: %d %t", count, ok)
	}
	for _, size := range []string{"+1", "01", "1.5", "-1", "257"} {
		if (WorkflowScaleDraft{Workflow: "news", Node: "reader", Size: size, Mode: "autoscale"}).valid() {
			t.Errorf("invalid size accepted: %s", size)
		}
	}
	if _, ok := (WorkflowScaleDraft{Workflow: "news", Node: "reader", Size: "0", Mode: "autoscale"}).SizeFor(scope); ok {
		t.Fatal("reviewed a ceiling below the minimum")
	}
	fixed := scope
	fixed.Autoscale = false
	if _, ok := plan.Scale.SizeFor(fixed); ok {
		t.Fatal("an autoscale request could change a fixed cluster")
	}
	now := time.Now()
	review, err := NewWorkflowScaleReview("owner", "session", WorkflowScale{Scope: scope, Size: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	changed := *review.WorkflowScale
	changed.Scope.RunID = "other-run"
	review.WorkflowScale = &changed
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("review accepted a different run: %v", err)
	}
}
