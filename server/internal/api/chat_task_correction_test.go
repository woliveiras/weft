package api_test

import (
	"context"

	"fmt"
	"net/http"

	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatCorrectionFixture struct{ chatAgentFixture }

func (chatCorrectionFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var selector string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Correction %s", &selector); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(`{"kind":"task.edit","task":%q,"newRequest":"Explain the data flow"}`, selector)}}, nil
}

func TestChatTaskCorrectionRequiresCurrentRevisionAndReplaysOnce(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatCorrectionFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); status != http.StatusCreated {
		t.Fatalf("workflow fixture HTTP %d", status)
	}
	var source taskconfig.Draft
	if status := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", taskBody, &source); status != http.StatusCreated {
		t.Fatalf("task fixture HTTP %d", status)
	}
	manager := taskconfig.Manager{Store: app.Store}
	propose := func(index int, token string) chat.Session {
		t.Helper()
		input := fmt.Sprintf(`{"requestId":"%032x","text":"Correction %s"}`, index, source.ID)
		var session chat.Session
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", input, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("proposal HTTP %d %+v", status, session)
		}
		return session
	}
	confirm := func(pending chat.Session) chat.Session {
		t.Helper()
		review := pending.Messages[1].Review
		var session chat.Session
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", pending.ID, review.ID)
		if status := call(t, srv, "POST", path, operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &session); status != http.StatusOK {
			t.Fatalf("confirmation HTTP %d", status)
		}
		return session
	}
	if viewer := propose(1, viewerTok); viewer.Messages[1].Review != nil {
		t.Fatal("viewer received an executable action")
	}
	pending := propose(2, operatorTok)
	stale := propose(3, operatorTok)
	for range 2 {
		r := confirm(pending).Messages[1].Review
		if r.State != chat.ReviewApplied || r.ResultVersion != 2 {
			t.Fatal(r)
		}
	}
	d, err := manager.Get(t.Context(), source.ID)
	if err != nil || d.Revision != 2 || d.Request != "Explain the data flow" || d.CorrectionOperation != pending.Messages[1].Review.ID || d.Status != "draft" || d.GraphVersion != source.GraphVersion {
		t.Fatal(d, err)
	}
	if r := confirm(stale).Messages[1].Review; r.State != chat.ReviewConflict {
		t.Fatal(r)
	}
}
