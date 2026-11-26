package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatCancelTaskFixture struct{ chatAgentFixture }

func (chatCancelTaskFixture) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var command, id string
	if _, err := fmt.Sscanf(req.Messages[len(req.Messages)-1].Content, "%s %s", &command, &id); err != nil {
		return nil, err
	}
	kind := "task.cancel"
	if command == "Follow" {
		kind = "task.watch"
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf(
		`{"kind":%q,"task":%q}`, kind, id)}}, nil
}

func TestChatCancellationTracksOnlyTheReviewedTaskRun(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatCancelTaskFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	const graph = `{"metadata":{"id":"cancel-echo"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","type":"queue","to":["worker"],"approval":true}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != http.StatusCreated {
		t.Fatalf("workflow fixture: %d", code)
	}
	manager := taskconfig.Manager{Store: app.Store}
	start := func(title string) (*taskconfig.Draft, string) {
		t.Helper()
		d, err := manager.Save(t.Context(), "", taskconfig.Input{Title: title, Request: title, GraphID: "cancel-echo", GraphVersion: 1,
			NodeID: "worker", Outcome: "freeform", ChannelID: "requests", MessageType: "fixture.request", PayloadJSON: "{}"}, 0, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if status := callIfMatch(t, srv, "POST", "/api/v1/tasks/"+d.ID+"/start", adminTok, `{"confirm":true}`, 1, &d); status != http.StatusAccepted {
			t.Fatalf("fixture start: %d", status)
		}
		record, err := app.Store.GetGraphRecord(t.Context(), d.ExecutionGraphID)
		if err != nil || record.RunID == "" {
			t.Fatalf("missing active run: %+v %v", record, err)
		}
		return d, record.RunID
	}
	send := func(path string, i int, text string) chat.Session {
		t.Helper()
		input := fmt.Sprintf(`{"requestId":"%032x","text":%q}`, i, text)
		var result chat.Session
		if status := call(t, srv, "POST", path, adminTok, "application/json", input, &result); status != http.StatusOK {
			t.Fatalf("Chat request: %d", status)
		}
		return result
	}
	task, run := start("Cancel this exact task")
	sibling, siblingRun := start("Leave this task running")
	first := send("/api/v1/chat/sessions", 1, "Follow "+task.ID)
	if len(first.Watches) != 1 {
		t.Fatalf("cannot observe original task: %+v", first)
	}
	path := "/api/v1/chat/sessions/" + first.ID
	request := send(path+"/messages", 2, "Cancel "+task.ID)
	review := request.Messages[len(request.Messages)-1].Review
	if review == nil || review.TaskCancel == nil || review.TaskCancel.RunID != run ||
		review.TaskCancel.ExecutionGraphID != task.ExecutionGraphID || review.TaskCancel.PendingApprovals != 1 {
		t.Fatalf("cancellation review scope: %+v", review)
	}
	if task, err := manager.Get(t.Context(), task.ID); err != nil || task.Status != "running" {
		t.Fatalf("preparing the review stopped the task: %+v %v", task, err)
	}
	apply := path + "/operations/" + review.ID + "/confirm"
	for range 2 {
		var result chat.Session
		if status := call(t, srv, "POST", apply, adminTok, "application/json", fmt.Sprintf(`{"digest":%q}`, review.Digest), &result); status != http.StatusOK ||
			len(result.Watches) != 1 || result.Messages[3].Review.State != chat.ReviewAccepted {
			t.Fatalf("cancellation claim: %d %+v", status, result)
		}
	}
	waitFor(t, 8*time.Second, "durable cancelled task", func() bool {
		taskState, err := manager.Get(t.Context(), task.ID)
		return err == nil && taskState.Status == "cancelled" && taskState.CancelOperation == review.ID && taskState.CancelRunID == run
	})
	var observed chat.Session
	if status := call(t, srv, "GET", path, adminTok, "", "", &observed); status != http.StatusOK {
		t.Fatalf("post-cancel read: %d", status)
	}
	last := observed.Messages[len(observed.Messages)-1]
	if last.Task == nil || last.Task.State != "cancelled" || !last.Unread {
		t.Fatalf("missing observed cancellation: %+v", observed)
	}
	settled, err := app.Store.ListApprovals(t.Context(), storage.ApprovalFilter{GraphID: task.ExecutionGraphID})
	if err != nil || len(settled) != 1 || settled[0].Status != domain.ApprovalExpired {
		t.Fatalf("task input remained pending: %+v %v", settled, err)
	}
	r, err := app.Store.GetGraphRecord(t.Context(), sibling.ExecutionGraphID)
	if err != nil || r.RunID != siblingRun || r.DesiredState != domain.GraphRunning {
		t.Fatalf("sibling execution changed: %+v %v", r, err)
	}
	remaining, err := app.Store.ListApprovals(t.Context(), storage.ApprovalFilter{GraphID: sibling.ExecutionGraphID, Status: domain.ApprovalPending})
	if err != nil || len(remaining) != 1 {
		t.Fatalf("sibling approval changed: %+v %v", remaining, err)
	}

	recovery, recoveryRun := start("Recover recorded cancellation")
	recovery.CancelOperation, recovery.CancelRunID = "chatop_recovery_fixture", recoveryRun
	if _, err := manager.Transition(t.Context(), recovery, "cancel_requested", "admin"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 8*time.Second, "controller resumed a recorded cancellation", func() bool {
		state, err := manager.Get(t.Context(), recovery.ID)
		return err == nil && state.Status == "cancelled" && state.CancelOperation == recovery.CancelOperation
	})
}
