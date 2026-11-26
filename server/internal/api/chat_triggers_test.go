package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type chatTriggerFixture struct{ chatAgentFixture }

func (chatTriggerFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var action string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "%s future issue intake for Ready.", &action); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"trigger.control","triggerTarget":"Ready","triggerAction":%q}`, strings.ToLower(action))}}, nil
}

func TestChatTriggerControlsPreserveAdmittedWorkAndRenewExactConsent(t *testing.T) {
	srv, app := setupTriggerApp(t, nil)
	app.Models.Add(llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, chatTriggerFixture{})
	ctx := t.Context()
	if _, err := app.Store.PutSetting(ctx, "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const workflow = "/api/v1/graphs/task-fixture"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); code != http.StatusCreated {
		t.Fatalf("create graph: %d", code)
	}
	var created trigger.Trigger
	if code := call(t, srv, "POST", "/api/v1/triggers", adminTok, "application/json", triggerBody, &created); code != http.StatusCreated {
		t.Fatalf("create trigger: %d", code)
	}
	manager := app.API.TriggerManager
	const currentTask = "tsk_alreadyadmitted"
	if _, err := manager.Update(ctx, created.ID, "fixture", func(t *trigger.Trigger) error {
		t.CurrentTask = currentTask
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	const base = "/api/v1/chat/sessions"
	propose := func(i int, token, action string) chat.Session {
		t.Helper()
		var session chat.Session
		body := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, i, action+" future issue intake for Ready.")
		if code := call(t, srv, "POST", base, token, "application/json", body, &session); code != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("review: %d %+v", code, session)
		}
		return session
	}
	confirm := func(session chat.Session) *chat.OperationReview {
		t.Helper()
		review := session.Messages[1].Review
		if review == nil || review.TriggerControl == nil || review.TriggerControl.ID != created.ID || review.TriggerControl.CurrentTask != currentTask {
			t.Fatalf("missing expected intake preview: %+v", session.Messages[1])
		}
		path := fmt.Sprintf("%s/%s/operations/%s/confirm", base, session.ID, review.ID)
		var result chat.Session
		if code := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result); code != http.StatusOK {
			t.Fatalf("confirm: %d", code)
		}
		return result.Messages[1].Review
	}
	if denied := propose(1, operatorTok, "Pause"); denied.Messages[1].Review != nil {
		t.Fatal("operator received administrative issue intake review")
	}
	pause := propose(2, adminTok, "Pause")
	if before, err := manager.Get(ctx, created.ID); err != nil || before.State != trigger.Active {
		t.Fatalf("preview paused intake: %+v %v", before, err)
	}
	for range 2 {
		if result := confirm(pause); result.State != chat.ReviewApplied {
			t.Fatalf("pause: %+v", result)
		}
	}
	if paused, err := manager.Get(ctx, created.ID); err != nil || paused.State != trigger.Paused || paused.Consent.Scope != created.Consent.Scope ||
		paused.ControlOperation != pause.Messages[1].Review.ID || paused.CurrentTask != currentTask {
		t.Fatalf("pause withdrew consent or affected an admitted task: %+v %v", paused, err)
	}
	stale := propose(3, adminTok, "Resume")
	if code := callIfMatch(t, srv, "PUT", workflow, adminTok, taskGraph, 1, nil); code != http.StatusOK {
		t.Fatalf("edit workflow: %d", code)
	}
	if result := confirm(stale); result.State != chat.ReviewConflict {
		t.Fatalf("stale workflow consent resumed intake: %+v", result)
	}
	resume := propose(4, adminTok, "Resume")
	for range 2 {
		if result := confirm(resume); result.State != chat.ReviewApplied {
			t.Fatalf("resume: %+v", result)
		}
	}
	after, err := manager.Get(ctx, created.ID)
	if err != nil || after.State != trigger.Active || after.Consent.WorkflowVersion != 2 || after.Consent.Scope == created.Consent.Scope ||
		after.Consent.Actor != "admin" || after.ControlOperation != resume.Messages[1].Review.ID || after.CurrentTask != currentTask {
		t.Fatalf("wrong resumed scope: %+v %v", after, err)
	}
	events, err := app.Store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvTriggerChanged}})
	if err != nil || len(events) != 3 {
		t.Fatalf("expected creation, one pause and one resume: %+v %v", events, err)
	}
}
