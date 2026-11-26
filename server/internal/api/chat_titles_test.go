package api_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

type titleRetryModel struct {
	chatAgentFixture
	titleCalls atomic.Int32
}

func (m *titleRetryModel) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if !strings.HasPrefix(req.System, "Write a short neutral title") {
		return m.chatAgentFixture.Chat(ctx, req)
	}
	m.titleCalls.Add(1)
	return &llm.ChatResponse{
		Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"title":"Release reader proposal"}`},
		Usage:   llm.Usage{InputTokens: 15, OutputTokens: 6},
	}, nil
}

func TestChatTitleRetryRetitlesWithoutRepeatingTheOriginalAction(t *testing.T) {
	srv, app := setupApp(t, nil)
	model := &titleRetryModel{}
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: model}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	const setting = `{"provider":"chat-fixture","name":"fixture"}`
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(setting), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const id = "1a154ae9cee7436ca37f848ef78c410e"
	const route = "/api/v1/chat/sessions"
	var original chat.Session
	if status := call(t, srv, "POST", route, adminTok, "application/json",
		`{"requestId":"`+id+`","text":"Create a release reader to summarize notes."}`, &original); status != http.StatusOK {
		t.Fatalf("submit: %d", status)
	}
	if original.Title != "Untitled chat" || len(original.Messages) != 2 || original.Messages[1].Review == nil {
		t.Fatalf("original fallback and review: %+v", original)
	}
	retry := route + "/" + id + "/title/retry"
	if status := call(t, srv, "POST", retry, viewerTok, "", "", nil); status != http.StatusNotFound {
		t.Fatalf("cross-owner retry: %d", status)
	}
	for range 2 {
		var next chat.Session
		if status := call(t, srv, "POST", retry, adminTok, "", "", &next); status != http.StatusOK {
			t.Fatalf("title retry: %d", status)
		}
		if next.Title != "Release reader proposal" || next.TitleRetryState != "completed" ||
			!next.LastActivity.Equal(original.LastActivity) || len(next.Messages) != 2 ||
			next.Messages[1].Review.ID != original.Messages[1].Review.ID ||
			next.Messages[1].Review.State != chat.ReviewAwaiting {
			t.Fatalf("title-only result: %+v", next)
		}
	}
	if model.titleCalls.Load() != 1 {
		t.Fatalf("title calls=%d", model.titleCalls.Load())
	}
	if status := call(t, srv, "GET", "/api/v1/agents/release-reader", adminTok, "", "", nil); status != http.StatusNotFound {
		t.Fatalf("title invoked the original write: %d", status)
	}
}
