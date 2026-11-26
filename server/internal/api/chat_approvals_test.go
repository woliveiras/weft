package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
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
