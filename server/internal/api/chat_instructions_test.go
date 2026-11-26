package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatInstructionsFixture struct{ chatAgentFixture }

func (chatInstructionsFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"workflow.instructions","instructionWorkflow":"edit-guide","instructionNode":"reader","localInstructions":"Inspect the test changes.","title":"Adjust reviewer instructions"}`}}, nil
}

func TestChatWorkflowInstructionsRetainTheCatalogPinAndRejectConcurrentEdits(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatInstructionsFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	reusable := domain.AgentDefinition{Name: "Reader", Prompt: "Read the release.", Runtime: domain.RuntimeRef{Kind: "echo"}}
	catalog := agentcatalog.Manager{Store: app.Store}
	if _, err := catalog.Publish(t.Context(), "release-reader", 0, reusable, "fixture"); err != nil {
		t.Fatal(err)
	}
	graph := `{"metadata":{"id":"edit-guide"},"spec":{"nodes":[{"id":"reader","agent":{"id":"release-reader","version":1},"localInstructions":"Inspect notes."}],"channels":[{"id":"input","to":["reader"]}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != 201 {
		t.Fatalf("graph status %d", code)
	}
	get := func(v int) *domain.Graph {
		t.Helper()
		g, err := app.Store.GetGraph(t.Context(), "edit-guide", v)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	ask := func(id, token string) *chat.OperationReview {
		t.Helper()
		var s chat.Session
		body := fmt.Sprintf(`{"requestId":%q,"text":"Replace the workflow-local instructions of reader in edit-guide."}`, id)
		if code := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", body, &s); code != 200 || len(s.Messages) != 2 {
			t.Fatalf("submit %d %+v", code, s.Messages)
		}
		return s.Messages[1].Review
	}
	confirm := func(id string, r *chat.OperationReview) *chat.OperationReview {
		t.Helper()
		var s chat.Session
		if code := call(t, srv, "POST", "/api/v1/chat/sessions/"+id+"/operations/"+r.ID+"/confirm", adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &s); code != 200 {
			t.Fatalf("confirm %d", code)
		}
		return s.Messages[1].Review
	}
	const first = "6334bcfa09134567aef46254553a616a"
	if ask(first, operatorTok) != nil {
		t.Fatal("operator received administrative edit")
	}
	r := ask(first, adminTok)
	if r == nil || r.Instructions.CatalogVersion != 1 || r.Instructions.Before != "Inspect notes." {
		t.Fatalf("review: %+v", r)
	}
	g := get(0)
	g.Metadata.Name = "Revised in editor"
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if code := callIfMatch(t, srv, "PUT", "/api/v1/graphs/edit-guide", adminTok, string(raw), 1, nil); code != 200 {
		t.Fatalf("manual edit: %d", code)
	}
	if confirm(first, r).State != chat.ReviewConflict {
		t.Fatal("stale workflow edit applied")
	}
	const second = "9334bcfa09134567aef46254553a616a"
	r = ask(second, adminTok)
	for range 2 {
		if got := confirm(second, r); got.State != chat.ReviewApplied || got.ResultVersion != 3 {
			t.Fatalf("save: %+v", got)
		}
	}
	saved := get(0)
	if saved.Metadata.Name != "Revised in editor" || saved.Metadata.Version != 3 || saved.Node("reader").Agent.Version != 1 || saved.Node("reader").LocalInstructions != "Inspect the test changes." || get(1).Node("reader").LocalInstructions != "Inspect notes." {
		t.Fatal("local edit changed the wrong settings")
	}
	agent, err := catalog.Get(t.Context(), "release-reader")
	if err != nil || len(agent.Versions) != 1 || agent.Versions[0].Definition.Prompt != reusable.Prompt {
		t.Fatalf("catalog changed: %+v %v", agent, err)
	}
}
