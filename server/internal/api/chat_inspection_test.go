package api_test

import (
	"context"
	"fmt"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"strings"
	"testing"
)

type chatInspectFixture struct{ chatAgentFixture }

func (chatInspectFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"kind":"console.inspect","inspectArea":"workflows","title":"Inspect workflows"}`}}, nil
}

func TestConsoleInspectionReadsCanonicalStateAndNeverCallsAModelForShortcuts(t *testing.T) {
	srv, a := setupApp(t, nil)
	graph := `{"metadata":{"id":"inspect-sample"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"}}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != 201 {
		t.Fatalf("graph %d", code)
	}
	ask := func(n int, text string) chat.Message {
		t.Helper()
		var session chat.Session
		if code := call(t, srv, "POST", "/api/v1/chat/sessions", viewerTok, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, n, text), &session); code != 200 || len(session.Messages) != 2 {
			t.Fatalf("response %d %+v", code, session.Messages)
		}
		return session.Messages[1]
	}
	for i, area := range strings.Fields("tasks workflows agents projects skills approvals activity triggers review-routes mcp scm issue-connections models chat-model plugins tools network retention usage prices budgets") {
		m := ask(i+1, "/inspect "+area)
		if m.Review != nil || m.Destination != chat.InspectionDestination(area) || !strings.HasPrefix(m.Text, area+" · ") {
			t.Fatalf("%s: %+v", area, m)
		}
		if area == "workflows" && !strings.Contains(m.Text, "inspect-sample · version 1 · stopped") {
			t.Fatal(m.Text)
		}
	}
	if err := a.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatInspectFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if m := ask(99, "Which workflows are currently running?"); m.Review != nil || !strings.Contains(m.Text, "inspect-sample · version 1 · stopped") {
		t.Fatal(m)
	}
}
