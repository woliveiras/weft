package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

type admissionModelFixture struct {
	chatAgentFixture
	started chan struct{}
	release chan struct{}
}

func (m admissionModelFixture) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if strings.Contains(req.Messages[len(req.Messages)-1].Content, "hold this call") {
		m.started <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.release:
		}
	}
	return m.chatAgentFixture.Chat(ctx, req)
}

func TestOneChatOwnerCannotOccupyAllInferenceSlots(t *testing.T) {
	srv, app := setupApp(t, nil)
	fixture := admissionModelFixture{started: make(chan struct{}, 2), release: make(chan struct{})}
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: fixture}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	req := func(n int, token, text string) chat.Session {
		var result chat.Session
		status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, n, text), &result)
		if status != http.StatusOK {
			t.Errorf("chat %d", status)
		}
		return result
	}
	done := make(chan struct{}, 2)
	defer func() {
		close(fixture.release)
		for range 2 {
			<-done
		}
	}()
	for i := range 2 {
		go func() { defer func() { done <- struct{}{} }(); req(i+1, adminTok, "Please hold this call.") }()
	}
	for range 2 {
		select {
		case <-fixture.started:
		case <-time.After(5 * time.Second):
			t.Fatal("provider did not receive the two held requests")
		}
	}
	denied := req(3, adminTok, "Create another reader")
	if len(denied.Messages) != 2 || denied.Messages[0].State != "unavailable" || !strings.Contains(denied.Messages[1].Text, "maximum number") {
		t.Fatalf("saturated owner %+v", denied)
	}
	other := req(4, operatorTok, "Create a reader")
	if len(other.Messages) != 2 || other.Messages[0].State != "interpreted" {
		t.Fatalf("other owner's call %+v", other)
	}
}
