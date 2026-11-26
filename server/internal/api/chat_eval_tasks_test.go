package api_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type evalTask struct{ id, title string }

// Expectations are newest first. Fixed timestamps make storage iteration order irrelevant.
func seedEvalTasks(t *testing.T, e *chatEval, status string, rows []evalTask) {
	t.Helper()
	for i, row := range rows {
		stamp := time.Date(2025, 1, 20-i, 12, 0, 0, 0, time.UTC)
		draft := taskconfig.Draft{Input: taskconfig.Input{Title: row.title}, ID: row.id, Status: status, UpdatedAt: stamp, CreatedAt: stamp}
		b, err := json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.app.Store.PutSetting(t.Context(), "tasks/drafts/"+row.id, b, 0, "eval"); err != nil {
			t.Fatal(err)
		}
	}
}

var taskIDInAnswer = regexp.MustCompile(`\btsk_[[:alnum:]_-]+\b`)
var listNumber = regexp.MustCompile(`(?m)^\s*[0-9]+[.)]\s+`)

func gradeTaskList(answer string, totalPattern *regexp.Regexp, rows []evalTask) error {
	// Remove list markers before checking totals: item number two is not a count.
	plain := strings.NewReplacer("*", "", "`", "").Replace(answer)
	if !totalPattern.MatchString(listNumber.ReplaceAllString(plain, "")) {
		return fmt.Errorf("missing matching total")
	}
	want := make([]string, 0, len(rows))
	rest := plain
	for _, row := range rows {
		want = append(want, row.id)
		_, after, ok := strings.Cut(rest, row.title)
		if !ok {
			return fmt.Errorf("missing or misplaced title %q", row.title)
		}
		rest = after
	}
	if !slices.Equal(want, taskIDInAnswer.FindAllString(answer, -1)) {
		return fmt.Errorf("task IDs or their order differ from %v", want)
	}
	return nil
}

func TestChatEvalTaskFollowup(t *testing.T) {
	const first = "Quantas tarefas terminaram com sucesso? Liste as três atualizadas mais recentemente, com título e ID. Se houver menos, mostre as disponíveis."
	const follow = "E quais falharam? Utilize o mesmo padrão de resposta."
	e := newChatEval(t, func(r *llm.ChatRequest) (*llm.ChatResponse, error) {
		last := r.Messages[len(r.Messages)-1]
		if last.Role == llm.RoleUser {
			status := "succeeded"
			if last.Content == follow {
				status = "failed"
			} else if last.Content != first {
				return nil, fmt.Errorf("unexpected fixture question")
			}
			return evalTool("query_tasks", chat.TaskQuery{Mode: "list", Status: status, Limit: 3})
		}
		var result chat.TaskQueryResult
		if last.Role != llm.RoleTool || last.Name != "query_tasks" {
			return nil, fmt.Errorf("missing task observation")
		}
		if err := json.Unmarshal([]byte(last.Content), &result); err != nil {
			return nil, err
		}
		lines := []string{fmt.Sprintf("Total: %d.", result.Total)}
		for _, row := range result.Items {
			lines = append(lines, row.Title+" — "+row.ID)
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: strings.Join(lines, "\n")}}, nil
	})
	done := []evalTask{{"tsk_amber", "Guia de revisão"}, {"tsk_sable", "Resumo editorial"}}
	failed := []evalTask{{"tsk_coral", "Entrega de mapas"}, {"tsk_lilac", "Entrega de rotas"}, {"tsk_olive", "Entrega de índices"}, {"tsk_ochre", "Entrega de imagens"}, {"tsk_onyx", "Entrega de fontes"}}
	seedEvalTasks(t, e, "succeeded", done)
	seedEvalTasks(t, e, "failed", failed)
	seedEvalTasks(t, e, "draft", []evalTask{{"tsk_other", "Rascunho pendente"}})
	session := ""
	for _, step := range []struct {
		prompt, status, total string
		rows                  []evalTask
	}{
		{first, "succeeded", `(?i)\b(2|duas|dois)\b`, done},
		{follow, "failed", `(?i)\b(5|cinco)\b`, failed[:3]},
	} {
		before := len(e.calls())
		response := e.ask(t, session, viewerTok, step.prompt)
		session = response.ID
		fresh := false
		for _, c := range e.calls()[before:] {
			q, err := chat.ParseTaskQuery(c.Arguments)
			if c.Name == "query_tasks" && err == nil && q.Mode == "list" && q.Status == step.status {
				fresh = true
			}
		}
		reply := response.Messages[len(response.Messages)-1]
		if !fresh || reply.State != "grounded" {
			t.Fatalf("expected fresh %s list and grounded answer", step.status)
		}
		if err := gradeTaskList(reply.Text, regexp.MustCompile(step.total), step.rows); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChatEvalUnavailableRead(t *testing.T) {
	e := newChatEval(t, func(*llm.ChatRequest) (*llm.ChatResponse, error) {
		return evalTool("query_tasks", chat.TaskQuery{Mode: "exists", Status: "running"})
	})
	if _, err := e.app.Store.PutSetting(t.Context(), "tasks/drafts/tsk_unreadable", []byte("invalid fixture JSON"), 0, "eval"); err != nil {
		t.Fatal(err)
	}
	result := e.ask(t, "", viewerTok, "Tem alguma task rodando agora?")
	calls := e.calls()
	if len(calls) == 0 {
		t.Fatal("missing task query")
	}
	last := calls[len(calls)-1]
	args, err := chat.ParseTaskQuery(last.Arguments)
	if last.Name != "query_tasks" || err != nil || args.Status != "running" {
		t.Fatal("missing current running task lookup")
	}
	reply := result.Messages[len(result.Messages)-1]
	if result.Messages[0].State != "unavailable" || reply.Inventory != nil || reply.Review != nil || !strings.Contains(reply.Text, "could not verify the task records") {
		t.Fatalf("expected an unavailable read with a diagnostic; got %q", reply.Text)
	}
}

func TestTaskListGrader(t *testing.T) {
	rows := []evalTask{{"tsk_red", "Atlas"}, {"tsk_blue", "Orion"}}
	total := regexp.MustCompile(`(?i)\b(2|duas)\b`)
	for _, test := range []struct {
		answer string
		pass   bool
	}{
		{"Duas tarefas.\n1. Atlas (tsk_red)\n2. Orion (tsk_blue)", true},
		{"Zero tarefas.\n1. Atlas (tsk_red)\n2. Orion (tsk_blue)", false},
		{"Total: 2. Orion (tsk_blue), Atlas (tsk_red).", false},
		{"Total: 2. Atlas (tsk_red), Orion (tsk_wrong).", false},
		{"Total: 2. Atlas (tsk_red).", false},
		{"Total: 2. Atlas (tsk_red), Orion (tsk_blue), tsk_extra.", false},
	} {
		if got := gradeTaskList(test.answer, total, rows) == nil; got != test.pass {
			t.Errorf("%q: pass=%v want %v", test.answer, got, test.pass)
		}
	}
}
