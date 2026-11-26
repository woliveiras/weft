package api_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatSkillFixture struct{ chatAgentFixture }

func (chatSkillFixture) Chat(_ context.Context, q *llm.ChatRequest) (*llm.ChatResponse, error) {
	action := "create"
	if strings.Contains(q.Messages[len(q.Messages)-1].Content, "Revise") {
		action = "revise"
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: fmt.Sprintf(`{"kind":"skill.save","skillAction":%q,"skillId":"review-changes","skillTitle":"Review changes","skillBody":"Check the empty-input case.","title":"Prepare review skill"}`, action)}}, nil
}
func TestChatManagedSkillUsesCanonicalRevisionAndHistory(t *testing.T) {
	srv, a := setupApp(t, nil)
	if err := a.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatSkillFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	ask := func(key int, token, txt string) chat.Session {
		t.Helper()
		var s chat.Session
		if code := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, key, txt), &s); code != 200 || len(s.Messages) != 2 {
			t.Fatalf("request %d", code)
		}
		return s
	}
	confirm := func(s chat.Session) *chat.OperationReview {
		t.Helper()
		r := s.Messages[1].Review
		if r == nil {
			t.Fatal(s.Messages[1])
		}
		var result chat.Session
		if code := call(t, srv, "POST", "/api/v1/chat/sessions/"+s.ID+"/operations/"+r.ID+"/confirm", adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &result); code != 200 {
			t.Fatal(code)
		}
		return result.Messages[1].Review
	}
	if ask(81, operatorTok, "Create a skill").Messages[1].Review != nil {
		t.Fatal("operator can save skill")
	}
	first := ask(81, adminTok, "Create a skill")
	for range 2 {
		if r := confirm(first); r.State != chat.ReviewApplied || r.ResultVersion != 1 {
			t.Fatal(r)
		}
	}
	pending := ask(82, adminTok, "Revise the skill")
	m := instructions.Manager{Store: a.Store}
	skill, err := m.Get(t.Context(), "review-changes")
	if err != nil {
		t.Fatal(err)
	}
	skill.Body = "Manual changes."
	skill.Global = true
	skill.Enabled = true
	if _, err = m.Save(t.Context(), skill.Input, 1, "manual", false); err != nil {
		t.Fatal(err)
	}
	if r := confirm(pending); r.State != chat.ReviewConflict {
		t.Fatal(r)
	}
	latest := ask(83, adminTok, "Revise the skill")
	if r := confirm(latest); r.State != chat.ReviewApplied || r.ResultVersion != 3 {
		t.Fatal(r)
	}
	skill, err = m.Get(t.Context(), "review-changes")
	if err != nil || !skill.Enabled || !skill.Global || len(skill.History) != 2 || skill.History[1].Body != "Manual changes." || skill.Operation != latest.Messages[1].Review.ID {
		t.Fatal(skill, err)
	}
}
