package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatApprovalFixture struct{ chatAgentFixture }

func (chatApprovalFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var id string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Answer approval %s", &id); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"approval.decide","approvalId":%q,"approvalDecision":"approve","approvalAnswer":"Use the current template."}`, id)}}, nil
}

func TestChatApprovalBindsPendingRequestAndRecordsOneDecision(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatApprovalFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const question = "Which template should I use for the editorial checklist?"
	deadline := time.Now().Add(time.Minute).UTC()
	makeApproval := func() *domain.Approval {
		t.Helper()
		a, err := app.API.Approvals.Create(t.Context(), &domain.Approval{
			GraphID: "editorial", Kind: domain.ApprovalQuestion, Title: "Editorial question", ExpiresAt: &deadline,
			Detail: json.RawMessage(fmt.Sprintf(`{"question":%q}`, question)),
		})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	propose := func(n int, id, token string) chat.Session {
		t.Helper()
		var session chat.Session
		input := fmt.Sprintf(`{"requestId":"%032x","text":"Answer approval %s with the current template."}`, n, id)
		if code := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", input, &session); code != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("review HTTP %d %+v", code, session)
		}
		return session
	}
	confirm := func(pending chat.Session) *chat.OperationReview {
		t.Helper()
		review := pending.Messages[1].Review
		if review == nil {
			t.Fatalf("no decision review: %+v", pending.Messages)
		}
		var session chat.Session
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", pending.ID, review.ID)
		if code := call(t, srv, "POST", path, operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &session); code != http.StatusOK {
			t.Fatalf("confirm HTTP %d", code)
		}
		return session.Messages[1].Review
	}
	first := makeApproval()
	if denied := propose(1, first.ID, viewerTok); denied.Messages[1].Review != nil {
		t.Fatal("viewer received a decision review")
	}
	pending := propose(2, first.ID, operatorTok)
	review := pending.Messages[1].Review
	if review == nil || review.Approval == nil || review.Approval.Title != question ||
		!review.ExpiresAt.Equal(deadline) || review.Approval.ID != first.ID {
		t.Fatalf("wrong review scope: %+v", review)
	}
	if state, err := app.Store.GetApproval(t.Context(), first.ID); err != nil || state.Status != domain.ApprovalPending {
		t.Fatalf("planning decided the approval: %+v %v", state, err)
	}
	for range 2 {
		if outcome := confirm(pending); outcome.State != chat.ReviewApplied {
			t.Fatalf("decision result: %+v", outcome)
		}
	}
	recorded, err := app.Store.GetApproval(t.Context(), first.ID)
	if err != nil || recorded.Answer != review.Approval.Answer || recorded.Status != domain.ApprovalApproved ||
		recorded.DecisionOperation != review.ID || recorded.DecidedBy != "operator" {
		t.Fatalf("native receipt: %+v %v", recorded, err)
	}

	second := makeApproval()
	old := propose(3, second.ID, operatorTok)
	if status := call(t, srv, "POST", "/api/v1/approvals/"+second.ID+"/reject", adminTok, "application/json", "{}", nil); status != http.StatusOK {
		t.Fatalf("manual decision: %d", status)
	}
	if outcome := confirm(old); outcome.State != chat.ReviewConflict {
		t.Fatalf("stale review overwrote the decision: %+v", outcome)
	}
	rejected, err := app.Store.GetApproval(t.Context(), second.ID)
	if err != nil || rejected.Status != domain.ApprovalRejected || rejected.DecisionOperation != "" {
		t.Fatalf("manual decision was replaced: %+v %v", rejected, err)
	}
}

type chatTaskApprovalFixture struct{ chatAgentFixture }

func (chatTaskApprovalFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var id string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Decide for %s", &id); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"approval.decide","task":%q,"approvalDecision":"reject","approvalComment":"Needs edits."}`, id)}}, nil
}

func TestChatSelectsOnlyAPendingApprovalOfTheRequestedTask(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatTaskApprovalFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const graph = `{"metadata":{"id":"review-input"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","type":"queue","to":["worker"],"approval":true}]}}`
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); c != 201 {
		t.Fatal(c)
	}
	manager := taskconfig.Manager{Store: app.Store}
	task, err := manager.Save(t.Context(), "", taskconfig.Input{Title: "Approval choices", Request: "Review", GraphID: "review-input", GraphVersion: 1,
		NodeID: "worker", Outcome: "freeform", ChannelID: "requests", MessageType: "fixture.request", PayloadJSON: "{}"}, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if c := callIfMatch(t, srv, "POST", "/api/v1/tasks/"+task.ID+"/start", adminTok, `{"confirm":true}`, 1, &task); c != http.StatusAccepted {
		t.Fatal(c)
	}
	extra, err := app.API.Approvals.Create(t.Context(), &domain.Approval{GraphID: task.ExecutionGraphID, NodeID: "worker", Kind: domain.ApprovalQuestion,
		Title: "Escolher o estorno", Detail: json.RawMessage(`{"question":"Which refund?"}`)})
	if err != nil {
		t.Fatal(err)
	}
	const sessions = "/api/v1/chat/sessions"
	ask := func(path, input string, sequence int) chat.Session {
		t.Helper()
		var v chat.Session
		if c := call(t, srv, "POST", path, operatorTok, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, sequence, input), &v); c != 200 {
			t.Fatal(c)
		}
		return v
	}
	lookup := ask(sessions, "/find approvals estorno", 5)
	if m := lookup.Messages[1]; !strings.Contains(m.Text, extra.Title) || !strings.Contains(m.Text, extra.ID) || strings.Contains(m.Text, "Which refund?") {
		t.Fatal(m)
	}
	v := ask(sessions, "Decide for "+task.ID, 1)
	if m := v.Messages[1]; m.State != "discovery" || !strings.Contains(m.Text, extra.ID) || strings.Contains(m.Text, "Which refund?") {
		t.Fatal(m)
	}
	stale := ask(sessions, "Decide for "+task.ID, 2)
	chosen := ask(sessions+"/"+v.ID+"/messages", "o do estorno", 3)
	r := chosen.Messages[len(chosen.Messages)-1].Review
	if r == nil || r.Approval == nil || r.Approval.ID != extra.ID || r.Approval.TaskID != task.ID || r.Approval.Decision != "reject" {
		t.Fatal(chosen.Messages)
	}
	if a, err := app.Store.GetApproval(t.Context(), extra.ID); err != nil || a.Status != domain.ApprovalPending {
		t.Fatal(a, err)
	}
	if _, err := app.API.Approvals.CancelPending(t.Context(), extra.ID); err != nil {
		t.Fatal(err)
	}
	resumed := ask(sessions+"/"+stale.ID+"/messages", "o do estorno", 4)
	if m := resumed.Messages[len(resumed.Messages)-1]; m.Review != nil || !strings.Contains(m.Text, "no matching pending") {
		t.Fatal(m)
	}
}
