package chat

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestWorkflowReviewBindsPersistedDraftAndCatalogPin(t *testing.T) {
	t.Parallel()
	const input = `{"kind":"workflow.create","workflowId":"release","workflowName":"Release notes","agent":"Reader","agentVersion":"1"}`
	plan, err := decodePlan(input)
	if err != nil || plan.Kind != CreateWorkflow || plan.Workflow.AgentVersion != "1" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	now := time.Now().UTC()
	graph := plan.Workflow.Graph(domain.AgentBinding{ID: "reader", Version: 1})
	review, err := NewWorkflowReview("owner-a", "session-a", WorkflowCreation{Graph: graph, AgentName: "Reader", Runtime: "native", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	var stored OperationReview
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.AgentCreation != nil || stored.ValidateConfirmation("owner-a", "session-a", review.Digest, now) != nil {
		t.Fatalf("workflow review changed across serialization: %s", body)
	}
	stored.Workflow.Graph.Spec.Nodes[0].Agent.Version = 2
	if err := stored.ValidateConfirmation("owner-a", "session-a", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed agent version passed review: %v", err)
	}
}

func TestWorkflowDraftRejectsUnscopedModelParameters(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"kind":"workflow.create","workflowId":"task-run-owned","workflowName":"Release","agent":"reader"}`,
		`{"kind":"workflow.create","workflowId":"release","workflowName":"Release","agent":"reader","agentVersion":"-1"}`,
		`{"kind":"workflow.create","workflowId":"release","workflowName":"Release","agent":"reader","agentVersion":1}`,
		`{"kind":"workflow.create","workflowId":"release","workflowName":"Release","agent":"reader","agentVersion":null}`,
		`{"kind":"workflow.create","workflowId":"release","workflowName":"Release","agent":"reader","runtime":"shell"}`,
		`{"kind":"workflow.create","workflowId":"release","workflowName":"Release","agent":"reader","task":"task"}`,
		`{"kind":"workflow.create","workflowId":"release","workflowName":"Release","agent":"reader","agentId":"another"}`,
		`{"kind":"agent.create","agentId":"reader","agentName":"Reader","instructions":"Read a note.","workflowId":"release"}`,
	} {
		if _, err := decodePlan(body); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("accepted %s: %v", body, err)
		}
	}
}
