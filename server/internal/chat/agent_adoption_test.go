package chat

import (
	"errors"
	"testing"
	"time"
)

func TestCatalogAdoptionRequiresExactVersionAndNode(t *testing.T) {
	t.Parallel()
	input := `{"kind":"workflow.adopt_agent","adoptWorkflow":"Editorial","adoptNode":"reader","adoptVersion":"2"}`
	plan, err := decodePlan(input)
	if err != nil || plan.Kind != AdoptAgentVersion || plan.Adoption.Version != "2" {
		t.Fatalf("adoption plan: %+v %v", plan, err)
	}
	now := time.Now()
	a := AgentAdoption{
		WorkflowID: "editorial", NodeID: "reader", WorkflowVersion: 1, WorkflowCreated: now,
		WorkflowDigest: "reviewed-workflow", AgentID: "catalog-reader", CurrentVersion: 1, TargetVersion: 2,
		AgentDigest: "reviewed-agent", Before: "Read notes", After: "Read revised notes",
	}
	review, err := NewAgentAdoptionReview("owner", "session", a, now)
	if err != nil {
		t.Fatal(err)
	}
	review.Adoption.NodeID = "another-node"
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("moved node accepted: %v", err)
	}
	for _, invalid := range []string{
		`{"kind":"workflow.adopt_agent","adoptWorkflow":"Editorial","adoptNode":"reader"}`,
		`{"kind":"workflow.adopt_agent","adoptWorkflow":"Editorial","adoptVersion":"latest"}`,
		`{"kind":"workflow.adopt_agent","adoptWorkflow":"Editorial","adoptNode":"reader","adoptVersion":"-3"}`,
		`{"kind":"workflow.adopt_agent","adoptWorkflow":"Editorial","adoptNode":"reader","adoptVersion":"2","tools":"new"}`,
	} {
		if _, err := decodePlan(invalid); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("accepted %s: %v", invalid, err)
		}
	}
}
