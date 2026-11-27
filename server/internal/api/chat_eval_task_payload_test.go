package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestChatEvalTaskPayload(t *testing.T) {
	const original = `{"section":"old-fixture-section","includeExamples":false}`
	const replacement = `{"section":"upgrade","includeExamples":true}`
	const graphID = "eval-task-payload"
	const privateRequest = "Fixture task description. Do not send this stored text to the model."
	var taskID, question string
	e := newChatEval(t, func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
		if last := req.Messages[len(req.Messages)-1]; last.Role != llm.RoleUser || last.Content != question {
			return nil, fmt.Errorf("unexpected request in task payload eval")
		}
		for _, message := range req.Messages {
			if strings.Contains(message.Content, "old-fixture-section") || strings.Contains(message.Content, privateRequest) {
				return nil, fmt.Errorf("stored draft leaked into model context")
			}
		}
		return evalTool("edit_task_draft", map[string]string{"task": taskID, "newPayloadJson": replacement})
	})
	const graph = `{"metadata":{"id":"eval-task-payload"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"requests","to":["reader"],"types":["review.request"]}]}}`
	if status := call(t, e.srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("create task workflow: %d", status)
	}
	const tasks = "/api/v1/tasks"
	input := taskconfig.Input{Title: "Release notes", Request: privateRequest, Outcome: "review", GraphID: graphID, GraphVersion: 1,
		NodeID: "reader", ResultNodeID: "reader", ChannelID: "requests", MessageType: "review.request", ExecutionMode: "workflow", PayloadJSON: original,
		Repositories: []domain.TaskRepository{{Alias: "docs", ProjectID: "prj_docs", BaseCommit: strings.Repeat("a", 40)},
			{Alias: "source", ProjectID: "prj_source", BaseCommit: strings.Repeat("b", 40)}}}
	encode := func(v taskconfig.Input) string {
		t.Helper()
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	var draft taskconfig.Draft
	if status := call(t, e.srv, "POST", tasks, operatorTok, "application/json", encode(input), &draft); status != http.StatusCreated || draft.ID == "" {
		t.Fatalf("create an editable task draft: %d, id=%q", status, draft.ID)
	}
	taskID = draft.ID
	question = fmt.Sprintf("Substitua somente o payload JSON completo do rascunho %s por `%s`. Mantenha o canal, tipo da mensagem, título e todos os outros campos.", taskID, replacement)
	read := func() taskconfig.Draft {
		t.Helper()
		var got taskconfig.Draft
		if status := call(t, e.srv, "GET", tasks+"/"+taskID, viewerTok, "", "", &got); status != http.StatusOK {
			t.Fatalf("read task: %d", status)
		}
		return got
	}
	propose := func(want taskconfig.Draft) (chat.Session, *chat.OperationReview) {
		t.Helper()
		previous := len(e.calls())
		s := e.ask(t, "", operatorTok, question)
		calls := e.calls()[previous:]
		if len(calls) == 0 || len(calls) > 2 {
			t.Fatalf("expected one native edit with at most one correction, got %d calls", len(calls))
		}
		for _, invocation := range calls {
			if invocation.Name != "edit_task_draft" {
				t.Fatalf("unexpected native operation: %s", invocation.Name)
			}
		}
		p, ok, err := chat.NativeOperationPlan(calls[len(calls)-1])
		if err != nil || !ok || p.Kind != chat.CorrectTask || p.Task != taskID || !equalEvalTaskPayload(p.CorrectionPayload, replacement) {
			t.Fatalf("model did not propose the requested payload replacement: %+v, %v", p, err)
		}
		r := s.Messages[len(s.Messages)-1].Review
		if r == nil || r.State != chat.ReviewAwaiting || r.TaskCorrection == nil || r.TaskCorrection.Payload == nil || r.Digest == "" {
			t.Fatalf("missing bounded payload review: %+v", s.Messages[len(s.Messages)-1])
		}
		edit := r.TaskCorrection
		if edit.Task != taskID || edit.Revision != want.Revision || edit.Workflow != graphID || edit.WorkflowVersion != want.GraphVersion || edit.Before != nil || edit.After != nil ||
			edit.Payload.Channel != want.ChannelID || edit.Payload.MessageType != want.MessageType || edit.Payload.Before != want.PayloadJSON || edit.Payload.After != p.CorrectionPayload {
			t.Fatalf("review differs from saved and proposed payload scope: %+v", edit)
		}
		if got := read(); !reflect.DeepEqual(got, want) {
			t.Fatal("preparing a review changed the saved task")
		}
		return s, r
	}
	confirm := func(s chat.Session, r *chat.OperationReview, token string, want int) chat.Session {
		t.Helper()
		var got chat.Session
		endpoint := "/api/v1/chat/sessions/" + s.ID + "/operations/" + r.ID + "/confirm"
		if status := call(t, e.srv, "POST", endpoint, token, "application/json", fmt.Sprintf(`{"digest":%q}`, r.Digest), &got); status != want {
			t.Fatalf("confirm payload review: %d, want %d", status, want)
		}
		return got
	}

	staleSession, stale := propose(draft)
	confirm(staleSession, stale, viewerTok, http.StatusForbidden)
	manual := draft.Input
	manual.Outcome = "investigate"
	var latest taskconfig.Draft
	if status := callIfMatch(t, e.srv, "PUT", tasks+"/"+taskID, operatorTok, encode(manual), int(draft.Revision), &latest); status != http.StatusOK {
		t.Fatalf("manual concurrent change: %d", status)
	}
	staleResult := confirm(staleSession, stale, operatorTok, http.StatusOK)
	if r := staleResult.Messages[1].Review; r == nil || r.State != chat.ReviewConflict || !reflect.DeepEqual(read(), latest) {
		t.Fatalf("stale draft payload change was not refused: %+v", r)
	}

	session, review := propose(latest)
	called := len(e.calls())
	applied := e.ask(t, session.ID, operatorTok, "Confirme esta proposta.")
	for _, s := range []chat.Session{applied, confirm(session, review, operatorTok, http.StatusOK)} {
		r := s.Messages[1].Review
		if r == nil || r.ID != review.ID || r.State != chat.ReviewApplied || r.ResultVersion != int(latest.Revision)+1 {
			t.Fatalf("missing one idempotent save receipt: %+v", r)
		}
	}
	saved := read()
	expected := latest.Input
	expected.PayloadJSON = review.TaskCorrection.Payload.After
	if !reflect.DeepEqual(saved.Input, expected) || saved.Revision != latest.Revision+1 || saved.RepositoryRevision != latest.RepositoryRevision || saved.Status != "draft" || saved.ExecutionGraphID != "" ||
		saved.CorrectionOperation != review.ID || saved.CorrectionRevision != saved.Revision || len(e.calls()) != called {
		t.Fatalf("confirmed replacement changed unrelated state or repeated a write: revision=%d, receipt=%q", saved.Revision, saved.CorrectionOperation)
	}
}

func equalEvalTaskPayload(a, b string) bool {
	var one, two any
	return json.Unmarshal([]byte(a), &one) == nil && json.Unmarshal([]byte(b), &two) == nil && reflect.DeepEqual(one, two)
}
