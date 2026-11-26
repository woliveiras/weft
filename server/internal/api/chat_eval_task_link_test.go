package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestChatEvalTaskLink(t *testing.T) {
	const missing = "tsk_linkdoesnotexist"
	const referential = "Me envia o link dela de novo."
	question := func(id string) string { return "Me de o link para a task " + id + "." }
	var taskID string
	e := newChatEval(t, func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
		last := req.Messages[len(req.Messages)-1]
		if last.Role != llm.RoleUser {
			return nil, fmt.Errorf("unexpected fixture request to link a task")
		}
		selector := taskID
		switch last.Content {
		case question(missing):
			selector = missing
		case question(taskID), referential:
		default:
			return nil, fmt.Errorf("unexpected user task link query")
		}
		return evalTool("inspect_task", map[string]string{"task": selector, "taskView": "link"})
	})
	const graph = `{"metadata":{"id":"eval-task-link"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"},"loop":{"mode":"event"}}]}}`
	if status := call(t, e.srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("create task workflow: %d", status)
	}
	createTask := func(title string) taskconfig.Draft {
		t.Helper()
		var draft taskconfig.Draft
		body := fmt.Sprintf(`{"title":%q,"request":"Review the release notes.","outcome":"review","graphId":"eval-task-link","graphVersion":1,"nodeId":"reader"}`, title)
		if status := call(t, e.srv, "POST", "/api/v1/tasks", operatorTok, "application/json", body, &draft); status != http.StatusCreated || draft.ID == "" {
			t.Fatalf("create test task: %d, id=%q", status, draft.ID)
		}
		return draft
	}
	row := createTask("Revisão de notas de versão")
	taskID = row.ID
	if status := call(t, e.srv, "POST", "/api/v1/chat/sessions", "", "application/json",
		fmt.Sprintf(`{"requestId":"%032x","text":%q}`, 80, question(taskID)), nil); status != http.StatusUnauthorized || len(e.calls()) != 0 {
		t.Fatalf("unauthenticated task link reached interpretation: %d", status)
	}
	// An exact task-ID lookup must not substitute a different task by title.
	other := createTask(missing)

	var sessionID string
	for _, step := range []struct {
		prompt, selector string
		found            bool
	}{
		{question(taskID), taskID, true},
		{referential, taskID, true},
		{question(missing), missing, false},
	} {
		previous := len(e.calls())
		s := e.ask(t, sessionID, viewerTok, step.prompt)
		sessionID = s.ID
		calls := e.calls()[previous:]
		if len(calls) == 0 || len(calls) > 2 {
			t.Fatalf("task link requires a native call with at most one correction, got %d calls", len(calls))
		}
		for _, invocation := range calls {
			if invocation.Name != "inspect_task" {
				t.Fatalf("unexpected operation for a task details link: %s", invocation.Name)
			}
		}
		plan, yes, err := chat.NativeOperationPlan(calls[len(calls)-1])
		if err != nil || !yes || plan.Kind != chat.InspectTask || plan.TaskView != chat.TaskLinkView || plan.Task != step.selector {
			t.Fatalf("final task link call did not request the exact reference: %+v, %v", plan, err)
		}
		last := s.Messages[len(s.Messages)-1]
		if last.Review != nil || len(s.Watches) != 0 || last.Destination != "" || strings.Contains(last.Text, "#/tasks/") {
			t.Fatalf("link request prepared work or returned an unstructured URL: %+v", last)
		}
		if step.found {
			if last.Task == nil || !last.Task.LinkOnly || last.Task.ID != taskID || last.Task.State != "" || last.Task.ExecutionID != "" {
				t.Fatalf("expected only a verified task reference for its detail link: %+v", last)
			}
			var found taskconfig.Draft
			if status := call(t, e.srv, "GET", "/api/v1/tasks/"+last.Task.ID, viewerTok, "", "", &found); status != http.StatusOK || found.ID != row.ID || found.Title != row.Title || found.Revision != row.Revision {
				t.Fatalf("returned reference does not identify the current task: %d %+v", status, found)
			}
		} else if last.Task != nil || !strings.Contains(last.Text, "No task was found") || strings.Contains(last.Text, other.ID) {
			t.Fatalf("missing ID produced a spurious link or status: %+v", last)
		}
	}
}
