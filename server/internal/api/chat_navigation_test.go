package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatNavigationFixture struct{ chatAgentFixture }

func (chatNavigationFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"manual.open","destination":"connection-setup","title":"Connect a service"}`}}, nil
}

func TestChatManualHandoffPersistsRoleAppropriateDestination(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatNavigationFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/chat/sessions"
	const id = "6324bcfa09134567aef46254553a636c"
	for _, tc := range []struct{ token, destination string }{{adminTok, "connection-setup"}, {viewerTok, "connections"}} {
		body := `{"requestId":"` + id + `","text":"Where do I configure an MCP connection?"}`
		if status := call(t, srv, "POST", path, tc.token, "application/json", body, nil); status != http.StatusOK {
			t.Fatalf("submit status %d", status)
		}
		var stored chat.Session
		if status := call(t, srv, "GET", path+"/"+id, tc.token, "", "", &stored); status != http.StatusOK {
			t.Fatalf("load status %d", status)
		}
		if len(stored.Messages) != 2 || stored.Messages[1].Destination != tc.destination || stored.Messages[1].Review != nil {
			t.Fatalf("unexpected persisted handoff: %+v", stored.Messages)
		}
	}
}
