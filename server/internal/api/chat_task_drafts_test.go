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

type chatTaskDraftFixture struct{}

func (chatTaskDraftFixture) Name() string { return "chat-fixture" }

func (chatTaskDraftFixture) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }

func (chatTaskDraftFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"task.create","taskTitle":"Summarize notes","workflow":"Editorial check","workflowVersion":"1","request":"Summarize the style notes.","outcome":"understand"}`}}, nil
}

func TestChatTaskDraftOperatorSavesOnceAndKeepsTheReviewedWorkflow(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{
		{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatTaskDraftFixture{}},
	}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const graph = `{"metadata":{"id":"editorial-check","name":"Editorial check"},"spec":{
	  "task":{"nodeId":"reader","resultNodeId":"reader","channelId":"requests","messageType":"work.request"},
	  "nodes":[{"id":"reader","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],
	  "channels":[{"id":"requests","type":"queue","to":["reader"]}]}}`
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("workflow fixture HTTP %d", status)
	}
	request := func(id int, token string) chat.Session {
		t.Helper()
		body := fmt.Sprintf(`{"requestId":"%032x","text":"Draft a task to summarize style notes using version one of Editorial check."}`, id)
		var session chat.Session
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", body, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("review request: %d %+v", status, session)
		}
		return session
	}
	decide := func(s chat.Session, token string) (int, chat.Session) {
		t.Helper()
		review := s.Messages[1].Review
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", s.ID, review.ID)
		var result chat.Session
		status := call(t, srv, "POST", path, token, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result)
		return status, result
	}
	if viewer := request(1, viewerTok); viewer.Messages[1].Review != nil {
		t.Fatal("viewer received actionable draft")
	}
	ready := request(2, operatorTok)
	review := ready.Messages[1].Review
	if review == nil || review.TaskDraft == nil || review.TaskDraft.Input.GraphVersion != 1 ||
		review.TaskDraft.Input.ChannelID != "requests" || review.TaskDraft.Input.MessageType != "work.request" {
		t.Fatalf("incomplete review: %+v", review)
	}
	manager := taskconfig.Manager{Store: app.Store}
	if page, err := manager.List(t.Context(), "", 10); err != nil || len(page.Items) != 0 {
		t.Fatalf("draft was stored before confirmation: %+v %v", page, err)
	}
	if status, _ := decide(ready, viewerTok); status != http.StatusForbidden {
		t.Fatalf("viewer decision status: %d", status)
	}
	if status, _ := decide(ready, adminTok); status != http.StatusNotFound {
		t.Fatalf("administrator accessed another owner: %d", status)
	}
	var taskID string
	for range 2 {
		status, result := decide(ready, operatorTok)
		settled := result.Messages[1].Review
		if status != http.StatusOK || settled.State != chat.ReviewCreated || settled.ResultID == "" || settled.ResultVersion != 1 ||
			(taskID != "" && taskID != settled.ResultID) {
			t.Fatalf("wrong result: %d %+v", status, settled)
		}
		taskID = settled.ResultID
	}
	draft, err := manager.CreationForOperation(t.Context(), review.ID)
	if err != nil || draft.ID != taskID || draft.Status != "draft" || draft.GraphVersion != 1 || draft.ExecutionGraphID != "" ||
		draft.ProjectID != "" || len(draft.Repositories) != 0 || draft.SCMConnection != "" {
		t.Fatalf("invalid scope or receipt: %+v %v", draft, err)
	}
	if status := call(t, srv, "GET", "/api/v1/tasks/"+draft.ID, operatorTok, "", "", nil); status != http.StatusOK {
		t.Fatalf("manual task route: %d", status)
	}
	pending := request(3, operatorTok)
	if pending.Messages[1].Review == nil {
		t.Fatal("concurrent workflow fixture proposal missing")
	}
	code, _, raw := rawCall(t, "PUT", srv.URL+"/api/v1/graphs/editorial-check", adminTok, graph, map[string]string{"If-Match": "1"})
	if code != http.StatusOK {
		t.Fatalf("concurrent editor save: %d %s", code, raw)
	}
	if status, result := decide(pending, operatorTok); status != http.StatusOK || result.Messages[1].Review.State != chat.ReviewConflict {
		t.Fatalf("stale workflow was not fenced: %d %+v", status, result.Messages)
	}
	if page, err := manager.List(t.Context(), "", 10); err != nil || len(page.Items) != 1 {
		t.Fatalf("duplicate or unauthorized draft: %+v %v", page, err)
	}
}
