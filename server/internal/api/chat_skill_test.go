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
	input := q.Messages[len(q.Messages)-1].Content
	for _, command := range []string{"enable", "disable", "make-default", "remove-default", "delete"} {
		if strings.HasPrefix(input, command+" ") {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: fmt.Sprintf(`{"kind":"skill.save","skillAction":%q,"skillId":%q}`, command, strings.TrimPrefix(input, command+" "))}}, nil
		}
	}
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
	removeDefault := ask(84, adminTok, "remove-default Review changes")
	if r := confirm(removeDefault); r.State != chat.ReviewApplied || r.Skill.After.Global || !r.Skill.After.Enabled || r.Skill.After.Body != skill.Body {
		t.Fatal(r)
	}
	disable := ask(85, adminTok, "disable review-changes")
	if r := confirm(disable); r.State != chat.ReviewApplied || r.Skill.After.Enabled || r.Skill.After.Description != skill.Description {
		t.Fatal(r)
	}
	if r := ask(86, operatorTok, "delete review-changes").Messages[1].Review; r != nil {
		t.Fatal("operator received deletion review")
	}
	deletion := ask(87, adminTok, "delete review-changes")
	const graph = `{"metadata":{"id":"referenced-skill"},"spec":{"instructions":{"skillIds":["review-changes"]},"nodes":[{"id":"reader","runtime":{"kind":"echo"}}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != 201 {
		t.Fatalf("workflow with managed skill: %d", code)
	}
	if r := confirm(deletion); r.State != chat.ReviewConflict {
		t.Fatalf("new workflow reference must prevent deletion: %+v", r)
	}
	if r := ask(88, adminTok, "delete review-changes").Messages[1].Review; r != nil {
		t.Fatal("referenced skill received deletion review")
	}
	if code := call(t, srv, "DELETE", "/api/v1/graphs/referenced-skill", adminTok, "", "", nil); code != 200 && code != 204 {
		t.Fatal(code)
	}
	deletion = ask(89, adminTok, "delete review-changes")
	for range 2 {
		if r := confirm(deletion); r.State != chat.ReviewApplied || !r.Skill.Delete {
			t.Fatal(r)
		}
	}
	deleted, err := m.Get(t.Context(), "review-changes")
	if err != nil || !deleted.Deleted || deleted.Enabled || deleted.Revision != 6 || len(deleted.History) != 5 || deleted.Operation != deletion.Messages[1].Review.ID {
		t.Fatal(deleted, err)
	}

}
