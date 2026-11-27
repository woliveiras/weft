package api_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatTaskStartFixture struct{}

func (chatTaskStartFixture) Name() string { return "chat-fixture" }

func (chatTaskStartFixture) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }

func (chatTaskStartFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var selector string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "Start %s", &selector); err != nil {
		return nil, err
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":"task.start","task":%q}`, selector)}}, nil
}

func TestChatTaskStartKeepsApprovalAndTracksTheObservedResult(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatTaskStartFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const graph = `{"metadata":{"id":"needs-approval"},"spec":{
		"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],
		"channels":[{"id":"requests","type":"queue","to":["worker"],"approval":true}]}}`
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("workflow fixture: %d", status)
	}
	drafts := taskconfig.Manager{Store: app.Store}
	draft := func(title string) *taskconfig.Draft {
		t.Helper()
		d, err := drafts.Save(t.Context(), "", taskconfig.Input{Title: title, Request: title, GraphID: "needs-approval", GraphVersion: 1,
			NodeID: "worker", Outcome: "freeform", ChannelID: "requests", MessageType: "fixture.request", PayloadJSON: "{}"}, 0, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	propose := func(number int, id, token string) chat.Session {
		t.Helper()
		var session chat.Session
		body := fmt.Sprintf(`{"requestId":"%032x","text":"Start %s"}`, number, id)
		if status := call(t, srv, "POST", "/api/v1/chat/sessions", token, "application/json", body, &session); status != http.StatusOK || len(session.Messages) != 2 {
			t.Fatalf("proposal HTTP %d %+v", status, session)
		}
		return session
	}
	confirm := func(pending chat.Session) chat.Session {
		t.Helper()
		op := pending.Messages[1].Review
		var session chat.Session
		path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", pending.ID, op.ID)
		if status := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, op.Digest), &session); status != http.StatusOK {
			t.Fatalf("confirmation: %d", status)
		}
		return session
	}

	task := draft("First approved start")
	if operator := propose(1, task.ID, operatorTok); operator.Messages[1].Review != nil {
		t.Fatal("operator received an administrative start proposal")
	}
	pending := propose(2, task.ID, adminTok)
	review := pending.Messages[1].Review
	if review == nil || review.TaskStart == nil || review.TaskStart.Consent.Mode != taskauth.Ask || review.TaskStart.Revision != task.Revision {
		t.Fatalf("invalid start review: %+v", review)
	}
	if unchanged, err := drafts.Get(t.Context(), task.ID); err != nil || unchanged.Status != "draft" || unchanged.ExecutionGraphID != "" {
		t.Fatalf("proposal already admitted the task: %+v %v", unchanged, err)
	}
	first := confirm(pending)
	second := confirm(pending)
	for _, result := range []chat.Session{first, second} {
		if result.Messages[1].Review.State != chat.ReviewAccepted || len(result.Watches) != 1 ||
			result.Watches[0].Task.ID != task.ID || !result.Watches[0].Task.DecisionNeeded {
			t.Fatalf("expected one watch of the pending approval: %+v", result)
		}
	}
	admitted, err := drafts.Get(t.Context(), task.ID)
	if err != nil || admitted.StartOperation != review.ID || admitted.ExecutionGraphID == "" {
		t.Fatalf("missing durable admission receipt: %+v %v", admitted, err)
	}
	var progress struct {
		PendingApprovals []*domain.Approval
	}
	if status := call(t, srv, "GET", "/api/v1/tasks/"+task.ID+"/progress", adminTok, "", "", &progress); status != http.StatusOK ||
		len(progress.PendingApprovals) != 1 {
		t.Fatalf("existing approval was not preserved: %d %+v", status, progress)
	}
	if status := call(t, srv, "POST", "/api/v1/approvals/"+progress.PendingApprovals[0].ID+"/approve", operatorTok, "application/json", "{}", nil); status != http.StatusOK {
		t.Fatalf("approval: %d", status)
	}
	var session chat.Session
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if status := call(t, srv, "GET", "/api/v1/chat/sessions/"+pending.ID, adminTok, "", "", &session); status != http.StatusOK {
			t.Fatalf("session observation: %d", status)
		}
		if len(session.Watches) == 1 && session.Watches[0].Complete {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	last := session.Messages[len(session.Messages)-1]
	if last.Task == nil || last.Task.State != "succeeded" || last.Task.ExecutionID == "" || !last.Unread ||
		session.Messages[1].Review.State != chat.ReviewAccepted {
		t.Fatalf("admission was confused with the real task outcome: %+v", session)
	}

	stale := draft("Authorization changes")
	proposal := propose(3, stale.ID, adminTok)
	auth := taskauth.Manager{Store: app.Store}
	current, err := auth.Review(t.Context(), stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Save(t.Context(), stale.ID, taskauth.Workspace, current.Scope, 0, "fixture", true); err != nil {
		t.Fatal(err)
	}
	declined := confirm(proposal)
	if decline := declined.Messages[1].Review; decline.State != chat.ReviewConflict || len(declined.Watches) != 0 {
		t.Fatalf("changed authorization was not fenced: %+v", declined)
	}
	if untouched, err := drafts.Get(t.Context(), stale.ID); err != nil || untouched.StartOperation != "" ||
		!strings.EqualFold(untouched.Status, "draft") {
		t.Fatalf("stale task was admitted: %+v %v", untouched, err)
	}
}

func TestChatReviewSurvivesControllerAndRunnerChange(t *testing.T) {
	for _, backends := range [][2]string{{"direct", "eino"}, {"eino", "direct"}} {
		t.Run(backends[0]+"_to_"+backends[1], func(t *testing.T) {
			dir := t.TempDir()
			configure := func(cfg *config.Config) {
				cfg.Storage.Dir = dir
				cfg.Workspaces.Root = filepath.Join(dir, "ws")
				cfg.Queue = config.Queue{Type: "fs", Dir: filepath.Join(dir, "queue")}
			}
			var session chat.Session
			var id string
			if !t.Run("propose", func(t *testing.T) {
				t.Setenv("WEFT_CHAT_ORCHESTRATOR", backends[0])
				srv, app := setupApp(t, configure)
				app.Models.Add(llm.ProviderConfig{Name: "chat-fixture", Type: "mock", DefaultModel: "fixture"}, chatTaskStartFixture{})
				if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
					t.Fatal(err)
				}
				workflow := `{"metadata":{"id":"restart-flow"},"spec":{"nodes":[{"id":"w","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"in","to":["w"],"approval":true}]}}`
				if s := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", workflow, nil); s != 201 {
					t.Fatal(s)
				}
				drafts := taskconfig.Manager{Store: app.Store}
				d, err := drafts.Save(t.Context(), "", taskconfig.Input{Title: "Restart review", Request: "Start fixture", GraphID: "restart-flow", GraphVersion: 1, NodeID: "w", ChannelID: "in", Outcome: "freeform", MessageType: "fixture", PayloadJSON: "{}"}, 0, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				id = d.ID
				body := fmt.Sprintf(`{"requestId":"%032x","text":"Start %s"}`, 42, id)
				if s := call(t, srv, "POST", "/api/v1/chat/sessions", adminTok, "application/json", body, &session); s != 200 || len(session.Messages) != 2 || session.Messages[1].Review == nil {
					t.Fatal("missing review", s, session)
				}
				current, err := drafts.Get(t.Context(), id)
				if err != nil || current.Status != "draft" || current.ExecutionGraphID != "" {
					t.Fatal(current, err)
				}
			}) {
				return
			}
			t.Run("confirm_after_restart", func(t *testing.T) {
				t.Setenv("WEFT_CHAT_ORCHESTRATOR", backends[1])
				srv, app := setupApp(t, configure)
				// The first instance's model fixture was never registered here.
				review := session.Messages[1].Review
				path := fmt.Sprintf("/api/v1/chat/sessions/%s/operations/%s/confirm", session.ID, review.ID)
				for range 2 {
					var settled chat.Session
					if s := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &settled); s != 200 || len(settled.Messages) < 2 || settled.Messages[1].Review.State != chat.ReviewAccepted || len(settled.Watches) != 1 {
						t.Fatal(s, settled)
					}
				}
				drafts := taskconfig.Manager{Store: app.Store}
				task, err := drafts.Get(t.Context(), id)
				if err != nil || task.StartOperation != review.ID || task.ExecutionGraphID == "" {
					t.Fatal(task, err)
				}
				pending, err := app.Store.ListApprovals(t.Context(), storage.ApprovalFilter{GraphID: task.ExecutionGraphID, Status: domain.ApprovalPending})
				if err != nil || len(pending) != 1 {
					t.Fatal("expected one task input after replay", pending, err)
				}
			})
		})
	}
}
