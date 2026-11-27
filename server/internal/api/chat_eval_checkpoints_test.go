package api_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestChatEvalWorkspaceCheckpoints(t *testing.T) {
	e := newChatEval(t, func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == llm.RoleUser {
			return evalTool("list_checkpoints", map[string]string{"target": "cp-api"})
		}
		var result struct{ Latest []chat.CheckpointRow }
		if err := json.Unmarshal([]byte(last.Content), &result); err != nil {
			return nil, err
		}
		if len(result.Latest) == 0 {
			return nil, fmt.Errorf("missing checkpoint")
		}
		cp := result.Latest[0]
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: fmt.Sprintf("%s · %s", cp.ID, cp.Tool)}}, nil
	})
	if status := call(t, e.srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", checkpointDoc, nil); status != 201 {
		t.Fatalf("graph: %d", status)
	}
	ctx := t.Context()
	g, _, err := e.app.Engine.GetGraph(ctx, "cp-api", 0)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.NewManager(e.app.Config.Workspaces.Root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := ws.Ensure(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	cp, _, err := ws.RecordCheckpoint(ctx, workspace.CheckpointScope{GraphID: "cp-api", NodeID: "dev", Dir: dir, Base: dir}, workspace.CheckpointMeta{Reason: workspace.CheckpointBeforeEdit, Tool: "filesystem.write"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	s := e.ask(t, "", viewerTok, "Qual é o ID do checkpoint mais recente do workflow cp-api e antes de qual ferramenta ele foi registrado?")
	msg := s.Messages[len(s.Messages)-1]
	if msg.Review != nil || msg.Destination != "workflows" || !strings.Contains(msg.Text, cp.ID) || !strings.Contains(msg.Text, "filesystem.write") {
		t.Fatalf("ungrounded checkpoint: %+v", msg)
	}
	calls := e.calls()
	if len(calls) == 0 {
		t.Fatal("missing checkpoint read")
	}
	for _, c := range calls {
		q, err := chat.ParseCheckpointQuery(c.Arguments)
		if c.Name != "list_checkpoints" || err != nil || q.Target != "cp-api" {
			t.Fatalf("unexpected call: %+v", c)
		}
	}
	if contents, err := os.ReadFile(file); err != nil || string(contents) != "second" {
		t.Fatalf("workspace changed: %q %v", contents, err)
	}
}

func TestChatCheckpointQueriesDistinguishMissingAttemptAndRepository(t *testing.T) {
	const task = "tsk_docs"
	var args map[string]string
	e := newChatEval(t, func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == llm.RoleUser {
			return evalTool("list_checkpoints", args)
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: last.Content}}, nil
	})
	ctx := t.Context()
	draft := taskconfig.Draft{ID: task, Status: "draft"}
	save := func(rev int64) {
		t.Helper()
		b, err := json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.app.Store.PutSetting(ctx, "tasks/drafts/"+task, b, rev, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	save(0)
	if status := call(t, e.srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", checkpointDoc, nil); status != 201 {
		t.Fatal(status)
	}
	g, _, err := e.app.Engine.GetGraph(ctx, "cp-api", 0)
	if err != nil {
		t.Fatal(err)
	}
	g.Spec.Workspace.Repositories = []domain.TaskRepository{{Alias: "docs"}, {Alias: "website"}}
	if _, err := e.app.Store.SaveGraph(ctx, g, 1); err != nil {
		t.Fatal(err)
	}
	check := func(target, alias, want string) {
		t.Helper()
		args = map[string]string{"target": target}
		if alias != "" {
			args["repository"] = alias
		}
		s := e.ask(t, "", viewerTok, "List workspace checkpoints.")
		last := s.Messages[len(s.Messages)-1]
		var observed struct {
			Status           string
			AvailableAliases []string
		}
		if err := json.Unmarshal([]byte(last.Text), &observed); err != nil || observed.Status != want {
			t.Fatalf("expected %s: %+v", want, last)
		}
		if strings.Contains(want, "repository") && strings.Join(observed.AvailableAliases, ",") != "docs,website" {
			t.Fatalf("missing aliases: %+v", observed)
		}
	}
	check("tsk_missing", "", "task_not_found")
	check(task, "", "task_has_no_execution")
	check("missing-flow", "", "workflow_not_found")
	draft.ExecutionGraphID = "cp-api"
	save(1)
	check(task, "", "repository_selection_required")
	check(task, "other", "unknown_repository_alias")
}
