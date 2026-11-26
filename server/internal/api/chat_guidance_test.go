package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatGuidanceFixture struct{ chatAgentFixture }

func (chatGuidanceFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"node.guide","guideWorkflow":"review-queue","guideNode":"reader","guideReplica":"0","guidance":"Focus on the tests.","title":"Guide reviewer"}`}}, nil
}

func TestChatGuidanceObservesOperatorReceiptAndRefusesAReplacedRun(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: chatGuidanceFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	gp := "/api/v1/graphs/review-queue"
	graph := `{"metadata":{"id":"review-queue"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"input","to":["reader"]}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != 201 {
		t.Fatalf("graph: %d", code)
	}
	act := func(action string) {
		t.Helper()
		if code := call(t, srv, "POST", gp+"/"+action, adminTok, "", "", nil); code != 200 {
			t.Fatalf("graph %s: %d", action, code)
		}
	}
	act("start")
	ask := func(id, token string) *chat.OperationReview {
		t.Helper()
		var s chat.Session
		body := fmt.Sprintf(`{"requestId":%q,"text":"Ask replica zero of reader in review-queue to focus on tests for this run."}`, id)
		if code := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", body, &s); code != 200 || len(s.Messages) != 2 {
			t.Fatalf("request %d %+v", code, s.Messages)
		}
		return s.Messages[1].Review
	}
	const id = "6334bcfa09134567aef46254553a636c"
	if ask(id, viewerTok) != nil {
		t.Fatal("viewer received a send action")
	}
	r := ask(id, operatorTok)
	if r == nil || r.Guidance == nil {
		t.Fatal("operator review missing")
	}
	confirm := func(id string, r *chat.OperationReview) *chat.OperationReview {
		t.Helper()
		var s chat.Session
		path := "/api/v1/chat/sessions/" + id + "/operations/" + r.ID + "/confirm"
		if code := call(t, srv, "POST", path, operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &s); code != http.StatusOK {
			t.Fatalf("confirm: %d", code)
		}
		return s.Messages[1].Review
	}
	if got := confirm(id, r); got.State != chat.ReviewQueued && got.State != chat.ReviewConsumed {
		t.Fatalf("not queued: %+v", got)
	}
	stored, err := app.Store.GetOperatorMessage(t.Context(), r.ID)
	if err != nil || stored.RunID != r.Guidance.Run || stored.Text != r.Guidance.Text {
		t.Fatalf("receipt: %+v %v", stored, err)
	}
	if _, err := app.Store.ConsumeOperatorMessagesForRun(t.Context(), "review-queue", "reader", 0, r.Guidance.Run); err != nil {
		t.Fatal(err)
	}
	var s chat.Session
	if code := call(t, srv, "GET", "/api/v1/chat/sessions/"+id, operatorTok, "", "", &s); code != 200 || s.Messages[1].Review.State != chat.ReviewConsumed {
		t.Fatalf("pickup: %d %+v", code, s.Messages)
	}
	if got := confirm(id, r); got.State != chat.ReviewConsumed {
		t.Fatalf("duplicate send: %+v", got)
	}
	const other = "2334bcfa09134567aef46254553a636c"
	stale := ask(other, operatorTok)
	act("stop")
	act("start")
	if got := confirm(other, stale); got.State != chat.ReviewConflict {
		t.Fatalf("replaced run: %+v", got)
	}
	messages, err := app.Store.ListOperatorMessages(t.Context(), "review-queue", "reader", 10)
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].Text, "tests") {
		t.Fatalf("unexpected sends: %d %v", len(messages), err)
	}
}
