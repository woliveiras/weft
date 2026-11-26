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

func TestOfflineConfigurationDownloadDoesNotCopyYAMLIntoChat(t *testing.T) {
	srv := setup(t)
	const route = "/api/v1/chat/sessions"
	const body = `{"requestId":"8125e80ce56b4b50a83981d63b1a811b","text":"/export"}`
	for _, token := range []string{adminTok, viewerTok} {
		var session chat.Session
		if status := call(t, srv, "POST", route, token, "application/json", body, &session); status != 200 || len(session.Messages) != 2 {
			t.Fatalf("status %d", status)
		}
		m := session.Messages[1]
		if (m.Destination == "configuration-export") != (token == adminTok) || m.Review != nil {
			t.Fatalf("export message %+v", m)
		}
	}
	if code := call(t, srv, "GET", "/api/v1/config/export", viewerTok, "", "", nil); code != 403 {
		t.Fatalf("export accepted viewer: %d", code)
	}
	_ = exportConfig(t, srv)
}
