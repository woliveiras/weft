package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatTaskAuthorizationFixture struct{ chatAgentFixture }

func (chatTaskAuthorizationFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var id, mode string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Set %s to %s", &id, &mode); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"task.authorize","task":%q,"taskAuthorizationMode":%q}`, id, mode)}}, nil
}

func TestChatTaskAuthorizationKeepsPolicyAndRevisionChecks(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatTaskAuthorizationFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const graph = `{"metadata":{"id":"consent-workflow"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","type":"queue","to":["worker"],"approval":true}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != http.StatusCreated {
		t.Fatalf("create workflow: %d", code)
	}
	draft, err := (taskconfig.Manager{Store: app.Store}).Save(t.Context(), "", taskconfig.Input{
		Title: "Review style notes", Request: "Review style notes.", Outcome: "review", GraphID: "consent-workflow",
		GraphVersion: 1, NodeID: "worker", ChannelID: "requests", MessageType: "request", PayloadJSON: "{}",
	}, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	const base = "/api/v1/chat/sessions"
	propose := func(index int, token string, mode taskauth.Mode) chat.Session {
		t.Helper()
		input := fmt.Sprintf(`{"requestId":"%032x","text":"Set %s to %s"}`, index, draft.ID, mode)
		var result chat.Session
		if status := call(t, srv, "POST", base, token, "application/json", input, &result); status != http.StatusOK || len(result.Messages) != 2 {
			t.Fatalf("review: %d %+v", status, result)
		}
		return result
	}
	confirm := func(session chat.Session) *chat.OperationReview {
		t.Helper()
		review := session.Messages[1].Review
		if review == nil || review.TaskAuthorization == nil {
			t.Fatalf("missing authorization review: %+v", session.Messages)
		}
		path := fmt.Sprintf("%s/%s/operations/%s/confirm", base, session.ID, review.ID)
		var result chat.Session
		if status := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result); status != http.StatusOK {
			t.Fatalf("confirmation: %d", status)
		}
		return result.Messages[1].Review
	}
	if denied := propose(1, operatorTok, taskauth.Ask); denied.Messages[1].Review != nil {
		t.Fatal("operator received an administrative authorization change")
	}
	stale := propose(2, adminTok, taskauth.Workspace)
	manager := taskauth.Manager{Store: app.Store, Tools: app.Tools}
	current, err := manager.Review(t.Context(), draft.ID)
	if err != nil || current.Record != nil {
		t.Fatalf("review performed a settings write: %+v %v", current, err)
	}
	if _, err := manager.Save(t.Context(), draft.ID, taskauth.Ask, current.Scope, 0, "fixture", true); err != nil {
		t.Fatal(err)
	}
	if settled := confirm(stale); settled.State != chat.ReviewConflict {
		t.Fatalf("stale authorization review applied: %+v", settled)
	}
	full := propose(3, adminTok, taskauth.Full)
	for range 2 {
		if saved := confirm(full); saved.State != chat.ReviewApplied || saved.ResultVersion != 2 {
			t.Fatalf("authorization result: %+v", saved)
		}
	}
	current, err = manager.Review(t.Context(), draft.ID)
	if err != nil || current.Record == nil || current.Record.Mode != taskauth.Full ||
		current.Record.Operation != full.Messages[1].Review.ID || current.Record.Scope != current.Scope {
		t.Fatalf("native record: %+v %v", current, err)
	}
	if status := callIfMatch(t, srv, "POST", "/api/v1/tasks/"+draft.ID+"/start", adminTok, `{"confirm":true}`, 1, nil); status != http.StatusAccepted {
		t.Fatalf("start task fixture: %d", status)
	}
	ask := propose(4, adminTok, taskauth.Ask)
	if result := confirm(ask); result.State != chat.ReviewApplied || result.ResultVersion <= 2 {
		t.Fatalf("live reduction failed: %+v", result)
	}
	extended := propose(5, adminTok, taskauth.Full).Messages[1]
	if extended.Review != nil || !strings.Contains(extended.Text, "only narrow") {
		t.Fatalf("running task could expand authorization: %+v", extended)
	}
}
