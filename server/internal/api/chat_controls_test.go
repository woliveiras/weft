package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
)

type chatControlsFixture struct{}

func (chatControlsFixture) Name() string { return "chat-fixture" }

func (chatControlsFixture) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }

func (chatControlsFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	request := req.Messages[len(req.Messages)-1].Content
	action, _, _ := strings.Cut(request, " ")
	node := ""
	if strings.Contains(request, "reviewer node") {
		node = "reviewer"
	}
	raw := fmt.Sprintf(`{"kind":"workflow.control","controlWorkflow":"Review workflow","controlAction":%q,"controlNode":%q}`, strings.ToLower(action), node)
	if request == "Deactivate the reviewer." {
		raw = `{"kind":"clarify","question":"Do you want to pause trigger intake, pause new turns, drain the node or interrupt its current turns?"}`
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: raw}}, nil
}

func TestChatWorkflowControlsRespectScopeAndObserveRuntime(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatControlsFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const graph = `{"metadata":{"id":"review","name":"Review workflow"},"spec":{"nodes":[
		{"id":"reviewer","runtime":{"kind":"echo"},"loop":{"mode":"event"}},
		{"id":"observer","runtime":{"kind":"echo"},"loop":{"mode":"event"}}]}}`
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("workflow fixture: %d", status)
	}
	propose := func(i int, token, text string) chat.Session {
		t.Helper()
		body := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, i, text)
		var session chat.Session
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", body, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("review HTTP %d %+v", status, session)
		}
		return session
	}
	find := func(session chat.Session) *chat.OperationReview {
		t.Helper()
		r := session.Messages[1].Review
		if r == nil || r.WorkflowControl == nil {
			t.Fatalf("operation %s has no review: %+v", session.ID, session.Messages[1])
		}
		return r
	}
	confirm := func(pending chat.Session) chat.Session {
		t.Helper()
		review := find(pending)
		var session chat.Session
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", pending.ID, review.ID)
		if status := call(t, srv, "POST", path, operatorTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &session); status != http.StatusOK {
			t.Fatalf("confirmation HTTP %d", status)
		}
		return session
	}
	waitSettled := func(pending chat.Session, desired string) *engine.WorkflowControlObservation {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		var current chat.Session
		for time.Now().Before(deadline) {
			if status := call(t, srv, "GET", "/api/v1/chat/sessions/"+pending.ID, operatorTok, "", "", &current); status != http.StatusOK {
				t.Fatalf("read review HTTP %d", status)
			}
			r := find(current)
			if r.State == chat.ReviewApplied && r.ControlResult != nil && r.ControlResult.Desired == desired && r.ControlResult.Settled {
				return r.ControlResult
			}
			if r.State != chat.ReviewApplying && r.State != chat.ReviewAccepted {
				t.Fatalf("unexpected control result: %+v", r)
			}
			time.Sleep(60 * time.Millisecond)
		}
		t.Fatalf("control did not settle: %+v", find(current))
		return nil
	}

	ambiguous := propose(1, operatorTok, "Deactivate the reviewer.")
	if message := ambiguous.Messages[1]; message.Review != nil || !strings.Contains(message.Text, "trigger intake") {
		t.Fatalf("ambiguous command could mutate runtime: %+v", message)
	}
	viewer := propose(2, viewerTok, "Start the Review workflow.")
	if viewer.Messages[1].Review != nil {
		t.Fatal("viewer received executable review")
	}
	start := propose(3, operatorTok, "Start the Review workflow.")
	before, err := app.Store.GetGraphRecord(t.Context(), "review")
	if err != nil || before.RunID != "" || before.DesiredState != domain.GraphStopped {
		t.Fatalf("workflow changed before confirmation: %+v %v", before, err)
	}
	confirmed := confirm(start)
	if got := find(confirmed).State; got != chat.ReviewAccepted && got != chat.ReviewApplied {
		t.Fatalf("start state = %s", got)
	}
	newRun := waitSettled(start, "running").RunID
	if record, err := app.Store.GetGraphRecord(t.Context(), "review"); err != nil || record.ControlOperation != find(start).ID ||
		record.RunID != newRun {
		t.Fatalf("durable start receipt missing: %+v %v", record, err)
	}
	replayed := confirm(start)
	if got := find(replayed).ControlResult; got == nil || got.RunID != newRun {
		t.Fatalf("replay changed the run: %+v", got)
	}

	pause := propose(4, operatorTok, "Pause the reviewer node in Review workflow.")
	if find(pause).WorkflowControl.Scope.NodeID != "reviewer" {
		t.Fatalf("wrong review scope: %+v", find(pause))
	}
	confirm(pause)
	waitSettled(pause, "paused")
	st, err := app.Engine.Status(t.Context(), "review")
	if err != nil || st.DesiredState != domain.GraphRunning || !st.Nodes[0].Paused || st.Nodes[1].Paused {
		t.Fatalf("the reviewer pause changed another scope: %+v %v", st, err)
	}
	resume := propose(5, operatorTok, "Resume the reviewer node in Review workflow.")
	if code := callIfMatch(t, srv, "PUT", "/api/v1/graphs/review", adminTok, graph, 1, nil); code != http.StatusOK {
		t.Fatalf("concurrent editor version: %d", code)
	}
	if r := find(confirm(resume)); r.State != chat.ReviewConflict {
		t.Fatalf("stale node review applied: %+v", r)
	}
	resume = propose(6, operatorTok, "Resume the reviewer node in Review workflow.")
	confirm(resume)
	waitSettled(resume, "running")

	stop := propose(7, operatorTok, "Stop the Review workflow gracefully.")
	confirm(stop)
	waitSettled(stop, "stopped")
}
