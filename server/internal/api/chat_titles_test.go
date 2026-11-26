package api_test

import (
	"context"
	"encoding/json"
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

func TestArchivedChatMustBeRestoredBeforeSending(t *testing.T) {
	srv, _ := setupApp(t, nil)
	const id = "a4154ae9cee7436ca37f848ef78c410e"
	const sessions = "/api/v1/chat/sessions"
	const path = sessions + "/" + id
	const message = `{"requestId":"b4154ae9cee7436ca37f848ef78c410e","text":"/inspect tasks"}`
	if code := call(t, srv, "POST", sessions, adminTok, "application/json", `{"requestId":"`+id+`","text":"/inspect projects"}`, nil); code != 200 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", path+"/archive", adminTok, "", "", nil); code != 200 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", path+"/messages", adminTok, "application/json", message, nil); code != 409 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", path+"/restore", adminTok, "", "", nil); code != 200 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", path+"/messages", adminTok, "application/json", message, nil); code != 200 {
		t.Fatal(code)
	}
}

func TestChatRenameUpdatesOwnedHistoryAndRejectsStaleEdits(t *testing.T) {
	srv, _ := setupApp(t, nil)
	const id = "d4154ae9cee7436ca37f848ef78c410e"
	const sessions = "/api/v1/chat/sessions"
	const url = sessions + "/" + id + "/title"
	var original chat.Session
	if code := call(t, srv, "POST", sessions, adminTok, "application/json", `{"requestId":"`+id+`","text":"/inspect projects"}`, &original); code != 200 {
		t.Fatal(code)
	}
	body, err := json.Marshal(map[string]string{"title": "Release notes", "previousTitle": original.Title})
	if err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "PUT", url, viewerTok, "application/json", string(body), nil); code != 404 {
		t.Fatal(code)
	}
	for range 2 {
		var saved chat.Session
		if code := call(t, srv, "PUT", url, adminTok, "application/json", string(body), &saved); code != 200 {
			t.Fatal(code)
		}
		if saved.Title != "Release notes" || !saved.LastActivity.Equal(original.LastActivity) || len(saved.Messages) != len(original.Messages) {
			t.Fatal("unexpected rename result")
		}
	}
	if code := call(t, srv, "PUT", url, adminTok, "application/json", `{"title":"A stale change","previousTitle":"Former title"}`, nil); code != 409 {
		t.Fatal(code)
	}
	if code := call(t, srv, "PUT", url, adminTok, "application/json", `{"title":" ","previousTitle":"Release notes"}`, nil); code != 400 {
		t.Fatal(code)
	}
	var page struct{ Items []struct{ Title string } }
	if code := call(t, srv, "GET", sessions, adminTok, "", "", &page); code != 200 || len(page.Items) != 1 || page.Items[0].Title != "Release notes" {
		t.Fatal("history did not reflect rename")
	}
}
