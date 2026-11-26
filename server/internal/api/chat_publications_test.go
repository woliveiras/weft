package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatPublicationFixture struct{ chatAgentFixture }

func (chatPublicationFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	change := "Read the revised notes."
	if strings.Contains(req.Messages[len(req.Messages)-1].Content, "cross-check") {
		change = "Cross-check the notes after reading."
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"agent.publish","publishAgent":"Editorial reader","sharedInstructions":%q}`, change)}}, nil
}

func TestChatPublicationAddsOneVersionAndLeavesTheWorkflowPin(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatPublicationFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const agentPath = "/api/v1/agents/editorial-reader"
	body := func(instructions string) string {
		t.Helper()
		d := domain.AgentDefinition{
			Name: "Editorial reader", Prompt: instructions, ReadOnly: true,
			Model: domain.ModelRef{Provider: "chat-fixture", Name: "fixture"}, Runtime: domain.RuntimeRef{Kind: "native"},
		}
		raw, err := json.Marshal(map[string]any{"id": "editorial-reader", "definition": d})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if code := call(t, srv, "POST", "/api/v1/agents", adminTok, "application/json", body("Read the notes."), nil); code != http.StatusCreated {
		t.Fatalf("initial catalog: %d", code)
	}
	const graph = `{"metadata":{"id":"editorial"},"spec":{"nodes":[{"id":"worker","agent":{"id":"editorial-reader","version":1},"localInstructions":"Keep the local constraint."}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != http.StatusCreated {
		t.Fatalf("initial workflow: %d", code)
	}
	propose := func(id int, actor, text string) chat.Session {
		t.Helper()
		var session chat.Session
		request := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, id, text)
		code := call(t, srv, "POST", "/api/v1/chat/sessions", actor, "application/json", request, &session)
		if code != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("proposal HTTP %d %+v", code, session)
		}
		return session
	}
	confirm := func(pending chat.Session) *chat.OperationReview {
		t.Helper()
		review := pending.Messages[1].Review
		if review == nil || review.Publication == nil {
			t.Fatalf("missing publication: %+v", pending.Messages)
		}
		var session chat.Session
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", pending.ID, review.ID)
		if code := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &session); code != http.StatusOK {
			t.Fatalf("confirm HTTP %d", code)
		}
		return session.Messages[1].Review
	}
	const requested = "Publish a new shared version of Editorial reader to read revised notes."
	if denied := propose(1, operatorTok, requested); denied.Messages[1].Review != nil {
		t.Fatal("operator could propose a catalog write")
	}
	pending := propose(2, adminTok, requested)
	if p := pending.Messages[1].Review; p == nil || p.Publication == nil ||
		p.Publication.PreviousVersion != 1 || p.Publication.Before != "Read the notes." || p.Publication.After != "Read the revised notes." {
		t.Fatalf("invalid scope: %+v", p)
	}
	var catalog agentcatalog.Agent
	if code := call(t, srv, "GET", agentPath, adminTok, "", "", &catalog); code != http.StatusOK || len(catalog.Versions) != 1 {
		t.Fatalf("catalog changed before confirmation: %d %+v", code, catalog)
	}
	for range 2 {
		if result := confirm(pending); result.State != chat.ReviewCreated || result.ResultVersion != 2 {
			t.Fatalf("publication did not pin one result: %+v", result)
		}
	}
	if code := call(t, srv, "GET", agentPath, adminTok, "", "", &catalog); code != http.StatusOK || len(catalog.Versions) != 2 ||
		catalog.Versions[1].Operation != pending.Messages[1].Review.ID || !catalog.Versions[1].Definition.ReadOnly {
		t.Fatalf("durable publication: %d %+v", code, catalog)
	}
	saved, err := app.Store.GetGraph(t.Context(), "editorial", 0)
	if err != nil || saved.Metadata.Version != 1 || saved.Spec.Nodes[0].Agent.Version != 1 ||
		saved.Spec.Nodes[0].Prompt != catalog.Versions[0].Definition.Prompt || saved.Spec.Nodes[0].LocalInstructions != "Keep the local constraint." {
		t.Fatalf("publication changed an existing workflow: %+v %v", saved, err)
	}
	stale := propose(3, adminTok, "Publish shared instructions to cross-check the notes.")
	if code := callIfMatch(t, srv, "PUT", agentPath, adminTok, body("Keep the intervening manual publication."), 2, nil); code != http.StatusOK {
		t.Fatalf("manual publication HTTP %d", code)
	}
	if result := confirm(stale); result.State != chat.ReviewConflict {
		t.Fatalf("stale publication was applied: %+v", result)
	}
}
