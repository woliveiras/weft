package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatAdoptionFixture struct{ chatAgentFixture }

func (chatAdoptionFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var version string
	request := req.Messages[len(req.Messages)-1].Content
	if _, err := fmt.Sscanf(request, "Adopt version %s", &version); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"workflow.adopt_agent","adoptWorkflow":"Editorial","adoptNode":"reader","adoptVersion":%q}`, version)}}, nil
}

func TestChatAdoptionPinsOneNodeAndPreservesWorkflowOverrides(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatAdoptionFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	agentBody := func(prompt string, readOnly bool) string {
		t.Helper()
		definition := domain.AgentDefinition{
			Name: "Catalog reader", Prompt: prompt, ReadOnly: readOnly,
			Model: domain.ModelRef{Provider: "chat-fixture", Name: "fixture"}, Runtime: domain.RuntimeRef{Kind: "native"},
			Parameters: []domain.AgentParameter{{Name: "source", Default: "notes"}},
		}
		body, err := json.Marshal(map[string]any{"id": "catalog-reader", "definition": definition})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	const agentPath = "/api/v1/agents/catalog-reader"
	if status := call(t, srv, "POST", "/api/v1/agents", adminTok, "application/json", agentBody("Read original notes.", true), nil); status != http.StatusCreated {
		t.Fatalf("catalog create: %d", status)
	}
	if status := callIfMatch(t, srv, "PUT", agentPath, adminTok, agentBody("Read revised notes.", true), 1, nil); status != http.StatusOK {
		t.Fatalf("catalog version 2: %d", status)
	}
	const graph = `{"metadata":{"id":"editorial","name":"Editorial"},"spec":{"nodes":[
		{"id":"reader","agent":{"id":"catalog-reader","version":1,"parameters":{"source":"review brief"}},"localInstructions":"Keep the workflow restriction."},
		{"id":"sibling","agent":{"id":"catalog-reader","version":1}}]}}`
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("workflow create: %d", status)
	}
	proposal := func(index, version int, actor string) chat.Session {
		t.Helper()
		input := fmt.Sprintf(`{"requestId":"%032x","text":"Adopt version %d for the reader in Editorial."}`, index, version)
		var session chat.Session
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", actor, "application/json", input, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("review HTTP %d %+v", status, session)
		}
		return session
	}
	confirm := func(session chat.Session) *chat.OperationReview {
		t.Helper()
		r := session.Messages[1].Review
		if r == nil || r.Adoption == nil {
			t.Fatalf("adoption missing: %+v", session.Messages)
		}
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", session.ID, r.ID)
		var result chat.Session
		if status := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &result); status != http.StatusOK {
			t.Fatalf("confirmation HTTP %d", status)
		}
		return result.Messages[1].Review
	}
	if viewer := proposal(1, 2, operatorTok); viewer.Messages[1].Review != nil {
		t.Fatal("non-admin could propose adoption")
	}
	pending := proposal(2, 2, adminTok)
	r := pending.Messages[1].Review
	if r == nil || r.Adoption == nil || r.Adoption.CurrentVersion != 1 || r.Adoption.TargetVersion != 2 ||
		r.Adoption.WorkflowVersion != 1 || r.Adoption.After != "Read revised notes." {
		t.Fatalf("unexpected adoption: %+v", r)
	}
	initial, err := app.Store.GetGraph(t.Context(), "editorial", 0)
	if err != nil || initial.Spec.Nodes[0].Agent.Version != 1 {
		t.Fatalf("preparing the review modified the workflow: %+v %v", initial, err)
	}
	if status := callIfMatch(t, srv, "PUT", agentPath, adminTok, agentBody("Read and write revised notes.", false), 2, nil); status != http.StatusOK {
		t.Fatalf("catalog version 3: %d", status)
	}
	for range 2 {
		if result := confirm(pending); result.State != chat.ReviewCreated || result.ResultVersion != 2 {
			t.Fatalf("adoption did not return one saved version: %+v", result)
		}
	}
	saved, record, err := app.Engine.GetGraphDocument(t.Context(), "editorial", 0)
	if err != nil || saved.Metadata.Version != 2 || record.ChangeOperation != r.ID ||
		saved.Spec.Nodes[0].Agent.Version != 2 || saved.Spec.Nodes[0].LocalInstructions != "Keep the workflow restriction." ||
		saved.Spec.Nodes[0].Agent.Parameters["source"] != "review brief" ||
		saved.Spec.Nodes[1].Agent.Version != 1 || saved.Spec.Nodes[0].Prompt != "Read revised notes." {
		t.Fatalf("workflow pin or overrides changed: %+v %+v %v", saved, record, err)
	}
	if rejection := proposal(3, 3, adminTok).Messages[1]; rejection.Review != nil || !strings.Contains(rejection.Text, "other shared settings") {
		t.Fatalf("other settings were allowed through instruction-only review: %+v", rejection)
	}
	wrong := engine.GraphSaveReview{CreatedAt: saved.Metadata.CreatedAt.Add(time.Second), Operation: "stale-identity"}
	if _, _, err := app.Engine.SaveReviewedGraph(t.Context(), saved, 2, "fixture", wrong); !errors.Is(err, engine.ErrConflict) {
		t.Fatalf("another workflow identity could pass compare-and-set: %v", err)
	}
}
