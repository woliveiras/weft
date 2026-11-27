package api_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatEvalTopicSwitch(t *testing.T) {
	const id = "tsk_coral"
	const budgets = "Abra a página de orçamentos."
	const link = "Me dê o link para a task " + id + "."
	const spend = "Qual provedor e modelo teve o maior gasto no mês passado? Quanto foi?"
	const checkpoints = "Qual foi o último checkpoint da tarefa " + id + " e antes de qual ferramenta ele foi registrado?"
	const failed = "Liste as tarefas que deram erro, com seus IDs."
	e := newChatEval(t, func(r *llm.ChatRequest) (*llm.ChatResponse, error) {
		last := r.Messages[len(r.Messages)-1]
		if last.Role == llm.RoleUser {
			switch last.Content {
			case budgets:
				return evalTool("open_console_page", map[string]string{"destination": "budgets"})
			case link:
				return evalTool("inspect_task", map[string]string{"task": id, "taskView": "link"})
			case spend:
				return evalTool("query_spend", map[string]string{"groupBy": "provider", "period": "previous"})
			case checkpoints:
				return evalTool("list_checkpoints", map[string]string{"target": id})
			case failed:
				return evalTool("query_tasks", map[string]string{"mode": "list", "status": "failed"})
			}
		}
		// Render only data supplied by the actual read service.
		switch last.Name {
		case "query_spend":
			var result chat.SpendObservation
			if err := json.Unmarshal([]byte(last.Content), &result); err != nil {
				return nil, err
			}
			if len(result.Rows) != 1 {
				return nil, fmt.Errorf("unexpected number of provider groups")
			}
			row := result.Rows[0]
			return fixtureTopicAnswer(fmt.Sprintf("%s / %s: %.2f %s", row.Key, row.Model, float64(row.CostMicros)/1e6, result.Currency)), nil
		case "list_checkpoints":
			var result struct{ Status string }
			if err := json.Unmarshal([]byte(last.Content), &result); err != nil || result.Status != "task_has_no_execution" {
				return nil, fmt.Errorf("unexpected checkpoint observation")
			}
			return fixtureTopicAnswer("A tarefa ainda não tem execução nem checkpoint."), nil
		case "query_tasks":
			var result chat.TaskQueryResult
			if err := json.Unmarshal([]byte(last.Content), &result); err != nil || len(result.Items) != 1 {
				return nil, fmt.Errorf("unexpected task observation")
			}
			return fixtureTopicAnswer(result.Items[0].ID), nil
		}
		return nil, fmt.Errorf("unexpected fixture request")
	})
	seedEvalTasks(t, e, "failed", []evalTask{{id, "Revisão editorial"}})
	event := domain.NewEvent(domain.EvCostRecorded, "", cost.Record{Source: "gateway", RateSource: "catalogue", Provider: "atlas", Model: "silk-small", Currency: "USD", Priced: true, CostMicros: 7500000})
	now := time.Now().UTC()
	event.Time = time.Date(now.Year(), now.Month()-1, 5, 12, 0, 0, 0, time.UTC)
	if err := e.app.Store.AppendEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	var session string
	for _, step := range []struct{ prompt, tool, dest string }{
		{budgets, "open_console_page", "budgets"}, {link, "inspect_task", ""}, {spend, "query_spend", "usage"}, {checkpoints, "list_checkpoints", "tasks"}, {failed, "query_tasks", "tasks"},
	} {
		before := len(e.calls())
		s := e.ask(t, session, viewerTok, step.prompt)
		session = s.ID
		answer := s.Messages[len(s.Messages)-1]
		calls := e.calls()[before:]
		if len(calls) == 0 || answer.Destination != step.dest || s.Messages[len(s.Messages)-2].State != "interpreted" || answer.Review != nil {
			t.Fatalf("wrong reply for %q: %+v", step.prompt, answer)
		}
		for _, call := range calls {
			if call.Name != step.tool {
				t.Fatalf("%q called %s", step.prompt, call.Name)
			}
		}
		switch step.prompt {
		case link:
			if answer.Task == nil || !answer.Task.LinkOnly || answer.Task.ID != id {
				t.Fatal("wrong task link")
			}
		case spend:
			a := strings.ToLower(strings.ReplaceAll(answer.Text, ",", "."))
			if !strings.Contains(a, "atlas") || !strings.Contains(a, "silk-small") || !strings.Contains(a, "7.5") {
				t.Fatalf("incorrect spend answer: %s", a)
			}
		case checkpoints:
			q, err := chat.ParseCheckpointQuery(calls[len(calls)-1].Arguments)
			if err != nil || q.Target != id || !strings.Contains(strings.ToLower(answer.Text), "execu") {
				t.Fatalf("incorrect checkpoint answer: %s", answer.Text)
			}
		case failed:
			if !strings.Contains(answer.Text, id) {
				t.Fatalf("missing failed task: %s", answer.Text)
			}
		}
	}
}

func fixtureTopicAnswer(answer string) *llm.ChatResponse {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: answer}}
}
