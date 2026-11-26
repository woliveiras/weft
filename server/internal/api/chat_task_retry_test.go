package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatRetryFixture struct{ chatAgentFixture }

func (chatRetryFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var selector string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Retry %s", &selector); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(`{"kind":"task.retry","task":%q}`, selector)}}, nil
}

func TestChatTaskRetryCreatesOneLinkedDraftWithoutConsentOrRun(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatRetryFixture{}}}, "chat-fixture"); err != nil {
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
	auth := taskauth.Manager{Store: app.Store}
	a, err := auth.Review(t.Context(), source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Save(t.Context(), source.ID, taskauth.Full, a.Scope, 0, "fixture", true); err != nil {
		t.Fatal(err)
	}
	failed, err := manager.Transition(t.Context(), &source, "failed", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	propose := func(index int, token string) chat.Session {
		t.Helper()
		input := fmt.Sprintf(`{"requestId":"%032x","text":"Retry %s"}`, index, source.ID)
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
	review := pending.Messages[1].Review
	if review == nil || review.TaskRetry == nil || review.TaskRetry.Revision != failed.Revision {
		t.Fatalf("invalid failed revision: %+v", review)
	}
	if _, err := manager.RetryDraft(t.Context(), source.ID, failed.Revision); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("review created a draft: %v", err)
	}
	var newID string
	for range 2 {
		settled := confirm(pending).Messages[1].Review
		if settled.State != chat.ReviewCreated || settled.ResultID == "" || (newID != "" && newID != settled.ResultID) {
			t.Fatalf("retry review: %+v", settled)
		}
		newID = settled.ResultID
	}
	child, err := manager.RetryDraft(t.Context(), source.ID, failed.Revision)
	if err != nil || child.ID != newID || child.Status != "draft" || !reflect.DeepEqual(child.Input, source.Input) || child.ExecutionGraphID != "" ||
		child.StartOperation != "" {
		t.Fatalf("wrong retry draft: %+v %v", child, err)
	}
	if consent, err := auth.Review(t.Context(), newID); err != nil || consent.Record != nil {
		t.Fatalf("retry took the old task's authorization: %+v %v", consent, err)
	}
	changedProposal := propose(3, operatorTok)
	if _, err := manager.Transition(t.Context(), failed, "failed", "fixture"); err != nil {
		t.Fatal(err)
	}
	changed := confirm(changedProposal)
	if changed.Messages[1].Review.State != chat.ReviewConflict {
		t.Fatalf("stale failed revision approved: %+v", changed)
	}
}
