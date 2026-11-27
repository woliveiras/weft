package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type chatTaskQueryModel struct {
	chatAgentFixture
	arguments map[string]string
	mu        sync.Mutex
	calls     []llm.ChatRequest
}

// The fixture behaves like a function-calling model: it chooses an operation
// from the question and composes its answer from the tool's actual response.
func (m *chatTaskQueryModel) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var captured llm.ChatRequest
	if err := json.Unmarshal(raw, &captured); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.calls = append(m.calls, captured)
	m.mu.Unlock()
	if len(req.Tools) < 1 || req.Tools[0].Name != "query_tasks" || !json.Valid(req.Tools[0].Parameters) || req.JSON || req.Model != "fixture" || len(req.Messages) == 0 {
		return nil, fmt.Errorf("missing task query function declaration")
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role == llm.RoleUser {
		arguments, ok := m.arguments[last.Content]
		if !ok {
			return nil, fmt.Errorf("unexpected fixture query: %q", last.Content)
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "fixture-task-lookup", Name: "query_tasks", Arguments: json.RawMessage(arguments), ProviderData: json.RawMessage(`{"signature":"fixture-call-signature"}`),
		}}}, StopReason: "tool_calls"}, nil
	}
	if last.Role != llm.RoleTool || last.Name != "query_tasks" || last.ToolCallID != "fixture-task-lookup" {
		return nil, fmt.Errorf("task query response is not linked to the function call")
	}
	var data struct {
		Mode, Status string
		Total        int
		Items        []struct{ ID, Title string }
	}
	if err := json.Unmarshal([]byte(last.Content), &data); err != nil {
		return nil, err
	}
	var answer string
	switch data.Mode {
	case "count":
		answer = fmt.Sprintf("Há %d tarefas em execução.", data.Total)
	case "exists":
		if data.Total == 0 {
			answer = "Não, nenhuma tarefa foi encontrada."
		} else {
			answer = "Sim, há tarefa concluída com sucesso."
		}
	case "list":
		titles := make([]string, 0, len(data.Items))
		for _, row := range data.Items {
			titles = append(titles, fmt.Sprintf("%s (`%s`)", row.Title, row.ID))
		}
		answer = "Em execução, da mais recente: " + strings.Join(titles, ", ") + "."
	default:
		return nil, fmt.Errorf("unexpected result mode: %q", data.Mode)
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: answer}}, nil
}

func (m *chatTaskQueryModel) requests() []llm.ChatRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

func TestChatQueriesTasksThroughModelAndSQLite(t *testing.T) {
	for _, backend := range []string{"direct", "eino"} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv("WEFT_CHAT_ORCHESTRATOR", backend)
			exerciseTaskQueries(t)
		})
	}
}

func exerciseTaskQueries(t *testing.T) {
	srv, app := setupApp(t, nil)
	const (
		graph        = `{"metadata":{"id":"query-task-flow"},"spec":{"nodes":[{"id":"worker","runtime":{"kind":"echo"},"loop":{"mode":"event"}}],"channels":[{"id":"in","to":["worker"],"approval":true}]}}`
		privateInput = "fixture-private-draft-message-241"
		privateJSON  = "fixture-private-task-payload-897"
		sessionsPath = "/api/v1/chat/sessions"
	)
	if status := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); status != http.StatusCreated {
		t.Fatalf("fixture workflow: %d", status)
	}
	newTask := func(title string) *taskconfig.Draft {
		t.Helper()
		payload, err := json.Marshal(taskconfig.Input{Title: title, Request: privateInput, Outcome: "freeform", GraphID: "query-task-flow", GraphVersion: 1,
			NodeID: "worker", ResultNodeID: "worker", ChannelID: "in", MessageType: "feature.request", PayloadJSON: fmt.Sprintf(`{"payload":%q}`, privateJSON)})
		if err != nil {
			t.Fatal(err)
		}
		var drafted taskconfig.Draft
		if status := call(t, srv, "POST", "/api/v1/tasks", operatorTok, "application/json", string(payload), &drafted); status != http.StatusCreated || drafted.ID == "" {
			t.Fatalf("create fixture task %q: %d", title, status)
		}
		return &drafted
	}
	manager := taskconfig.Manager{Store: app.Store}
	var running []*taskconfig.Draft
	for _, title := range []string{"Fila antiga", "Relatório intermediário", "Revisão mais recente"} {
		d := newTask(title)
		observed, err := manager.Transition(t.Context(), d, "running", "fixture")
		if err != nil {
			t.Fatal(err)
		}
		running = append(running, observed)
	}
	completed := newTask("Tarefa executada pelo fluxo")
	taskPath := "/api/v1/tasks/" + completed.ID
	start, err := http.NewRequestWithContext(t.Context(), "POST", srv.URL+taskPath+"/start", strings.NewReader(`{"confirm":true}`))
	if err != nil {
		t.Fatal(err)
	}
	start.Header.Set("Authorization", "Bearer "+adminTok)
	start.Header.Set("Content-Type", "application/json")
	start.Header.Set("If-Match", strconv.FormatInt(completed.Revision, 10))
	started, err := http.DefaultClient.Do(start)
	if err != nil {
		t.Fatal(err)
	}
	started.Body.Close()
	if started.StatusCode != http.StatusAccepted {
		t.Fatalf("start fixture task: %d", started.StatusCode)
	}
	var approvalID string
	waitFor(t, 8*time.Second, "a real task waits at the approval gate", func() bool {
		var progress struct{ PendingApprovals []struct{ ID string } }
		if call(t, srv, "GET", taskPath+"/progress", viewerTok, "", "", &progress) != http.StatusOK || len(progress.PendingApprovals) != 1 {
			return false
		}
		approvalID = progress.PendingApprovals[0].ID
		return approvalID != ""
	})
	if status := call(t, srv, "POST", "/api/v1/approvals/"+approvalID+"/approve", operatorTok, "", "", nil); status != http.StatusOK {
		t.Fatalf("approve fixture task: %d", status)
	}
	waitFor(t, 8*time.Second, "the task outcome is succeeded", func() bool {
		var progress struct{ State string }
		return call(t, srv, "GET", taskPath+"/progress", viewerTok, "", "", &progress) == http.StatusOK && progress.State == "succeeded"
	})
	// Seed a retained nonterminal receipt for a finished execution. Waiting for
	// settlement first makes both the timestamp order and the stale receipt
	// independent of the engine's background reconciliation interval.
	waitFor(t, 10*time.Second, "the engine persists the terminal receipt", func() bool {
		draft, err := manager.Get(t.Context(), completed.ID)
		return err == nil && draft.Status == "succeeded"
	})
	settled, err := manager.Get(t.Context(), completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Transition(t.Context(), settled, "starting", "fixture"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 52; i++ {
		newTask(fmt.Sprintf("Rascunho recente %02d", i))
	}
	var firstPage taskconfig.Page
	if status := call(t, srv, "GET", "/api/v1/tasks?order=updated_desc", viewerTok, "", "", &firstPage); status != http.StatusOK || len(firstPage.Items) != 50 || firstPage.Next == "" {
		t.Fatalf("first task page should not exhaust fixture: %d, items=%d, next=%q", status, len(firstPage.Items), firstPage.Next)
	}
	for _, row := range firstPage.Items {
		if row.Status != "draft" {
			t.Fatalf("matching tasks must be behind the first page: %+v", row)
		}
	}
	const (
		countQuestion = "Quantas tarefas estão em execução?"
		listQuestion  = "Quais são só as duas mais recentes desse grupo?"
		doneQuestion  = "Existe alguma tarefa que terminou com sucesso?"
		zeroQuestion  = "Alguma tarefa já foi cancelada?"
		failQuestion  = "Pode conferir de novo quantas tarefas estão em execução?"
	)
	model := &chatTaskQueryModel{arguments: map[string]string{
		countQuestion: `{"mode":"count","status":"running","title":"Tarefas em execução"}`,
		listQuestion:  `{"mode":"list","status":"running","limit":2}`,
		doneQuestion:  `{"mode":"exists","status":"succeeded"}`,
		zeroQuestion:  `{"mode":"exists","status":"cancelled"}`,
		failQuestion:  `{"mode":"count","status":"running"}`,
	}}
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: model}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	ask := func(sequence int, sessionID, question string) chat.Session {
		t.Helper()
		path := sessionsPath
		if sessionID != "" {
			path += "/" + sessionID + "/messages"
		}
		var result chat.Session
		if status := call(t, srv, "POST", path, viewerTok, "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, sequence, question), &result); status != http.StatusOK || len(result.Messages) < 2 {
			t.Fatalf("Chat query %q: %d, replies=%d", question, status, len(result.Messages))
		}
		return result
	}

	if status := call(t, srv, "POST", sessionsPath, "", "application/json", fmt.Sprintf(`{"requestId":"%032x","text":%q}`, 1, countQuestion), nil); status != http.StatusUnauthorized || len(model.requests()) != 0 {
		t.Fatalf("anonymous query reached Chat: %d", status)
	}
	inspection := ask(1, "", "/inspect tasks")
	inspectReply := inspection.Messages[1]
	if len(model.requests()) != 0 || !strings.HasPrefix(inspectReply.Text, "tasks · ") || !strings.Contains(inspectReply.Text, "Rascunho recente 52") || inspectReply.Inventory == nil || inspectReply.Inventory.Total != nil || len(inspectReply.Inventory.Items) != chat.InspectionLimit {
		t.Fatalf("explicit inspection did not remain a bounded model-free overview: %+v", inspectReply)
	}

	result := ask(2, "", countQuestion)
	if result.Title != "Tarefas em execução" {
		t.Fatalf("query title was not used: %q", result.Title)
	}
	assertReply := func(session chat.Session, text, mode, status string, total int) {
		t.Helper()
		message := session.Messages[len(session.Messages)-1]
		seen := message.Inventory
		if message.Text != text || message.Review != nil || message.Destination != "tasks" || seen == nil || seen.Total == nil || *seen.Total != total || seen.TaskMode != mode || seen.TaskStatus != status || len(seen.Items) != 0 {
			t.Fatalf("incorrect Chat query result: %+v inventory=%+v", message, seen)
		}
	}
	assertReply(result, "Há 3 tarefas em execução.", "count", "running", 3)
	result = ask(3, result.ID, listQuestion)
	listed := fmt.Sprintf("Em execução, da mais recente: Revisão mais recente (`%s`), Relatório intermediário (`%s`).", running[2].ID, running[1].ID)
	assertReply(result, listed, "list", "running", 3)
	assertReply(ask(4, "", doneQuestion), "Sim, há tarefa concluída com sucesso.", "exists", "succeeded", 1)
	assertReply(ask(5, "", zeroQuestion), "Não, nenhuma tarefa foi encontrada.", "exists", "cancelled", 0)

	requests := model.requests()
	if len(requests) != 8 {
		t.Fatalf("each of four queries should interpret and then answer: model had %d requests", len(requests))
	}
	for _, req := range requests {
		wire, err := json.Marshal(req)
		if err != nil || strings.Contains(string(wire), privateInput) || strings.Contains(string(wire), privateJSON) {
			t.Fatal("private task data appeared in the model request")
		}
	}
	for i, expected := range []struct {
		question, mode, status string
		total                  int
		items                  int
	}{{countQuestion, "count", "running", 3, 0}, {listQuestion, "list", "running", 3, 2},
		{doneQuestion, "exists", "succeeded", 1, 0}, {zeroQuestion, "exists", "cancelled", 0, 0}} {
		interpret, answer := requests[2*i], requests[2*i+1]
		if interpret.Messages[len(interpret.Messages)-1].Content != expected.question || len(answer.Messages) < 3 {
			t.Fatalf("provider did not receive the question and function exchange for %q", expected.question)
		}
		invocation := answer.Messages[len(answer.Messages)-2]
		tool := answer.Messages[len(answer.Messages)-1]
		if invocation.Role != llm.RoleAssistant || len(invocation.ToolCalls) != 1 || string(invocation.ToolCalls[0].ProviderData) != `{"signature":"fixture-call-signature"}` || tool.Role != llm.RoleTool || tool.ToolCallID != invocation.ToolCalls[0].ID {
			t.Fatalf("provider call and result were not associated for %q", expected.question)
		}
		var payload chat.TaskQueryResult
		decode := json.NewDecoder(strings.NewReader(tool.Content))
		decode.DisallowUnknownFields()
		if err := decode.Decode(&payload); err != nil || payload.Mode != expected.mode || payload.Status != expected.status || payload.Total != expected.total || len(payload.Items) != expected.items {
			t.Fatalf("wrong safe result received by model for %q: %s (%v)", expected.question, tool.Content, err)
		}
		if expected.items > 0 && (payload.Items[0].ID != running[2].ID || payload.Items[1].ID != running[1].ID) {
			t.Fatalf("model saw the wrong task order: %+v", payload.Items)
		}
	}
	if !slices.ContainsFunc(requests[2].Messages, func(m llm.ChatMessage) bool {
		return m.Role == llm.RoleAssistant && strings.Contains(m.Content, `mode=count, status="running", total=3`) && strings.Contains(m.Content, "Há 3 tarefas em execução.")
	}) {
		t.Fatal("contextual list query did not receive the previous verified snapshot")
	}

	// An undecodable row in the real SQLite collection must not turn into a
	// partial count or be echoed to the model as tool evidence.
	const corruptRow = "fixture-private-corrupt-task-record-991"
	if _, err := app.Store.PutSetting(t.Context(), "tasks/drafts/tsk_corrupt_query_fixture", []byte(corruptRow), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	failure := ask(6, "", failQuestion)
	failed := failure.Messages[1]
	if failure.Messages[0].State != "unavailable" || failed.Inventory != nil || failed.Review != nil || !strings.Contains(failed.Text, "could not verify the task records") || strings.Contains(failed.Text, corruptRow) || len(model.requests()) != 9 {
		t.Fatalf("failed task read should stop before sending tool evidence: %+v, model calls=%d", failed, len(model.requests()))
	}
}
