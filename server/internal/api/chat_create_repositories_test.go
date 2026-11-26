package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type draftRepoFixture struct{ chatAgentFixture }

func (draftRepoFixture) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: `{"kind":"task.create","taskTitle":"Read projects","workflow":"task-fixture","request":"Compare the examples.","outcome":"understand","repositories":[{"project":"One","alias":"app"},{"project":"Two","alias":"tests"}]}`}}, nil
}

func TestChatCreatesMultiProjectDraftAndRejectsStaleRepository(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "mock"}, Provider: draftRepoFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	projects := projectconfig.Manager{Store: app.Store}
	var selected *projectconfig.Project
	for _, name := range []string{"One", "Two"} {
		dir := t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatal(string(out), err)
			}
		}
		var err error
		selected, err = projects.Create(t.Context(), projectconfig.Input{Name: name, Path: dir}, "fixture")
		if err != nil {
			t.Fatal(err)
		}
	}
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); code != 201 {
		t.Fatal(code)
	}
	const route = "/api/v1/chat/sessions"
	propose := func(i int) (string, *chat.OperationReview) {
		t.Helper()
		id := fmt.Sprintf("%032x", i)
		var session chat.Session
		if code := call(t, srv, "POST", route, operatorTok, "application/json", fmt.Sprintf(`{"requestId":%q,"text":"Compare both projects."}`, id), &session); code != 200 || len(session.Messages) != 2 || session.Messages[1].Review == nil {
			t.Fatal(code, session)
		}
		r := session.Messages[1].Review
		if len(r.TaskDraft.Repositories) != 2 || r.TaskDraft.Repositories[1].Commit != selected.Head {
			t.Fatal(r)
		}
		return id, r
	}
	confirm := func(id string, r *chat.OperationReview) *chat.OperationReview {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"digest": r.Digest})
		var session chat.Session
		if c := call(t, srv, "POST", route+"/"+id+"/operations/"+r.ID+"/confirm", operatorTok, "application/json", string(b), &session); c != 200 {
			t.Fatal(c)
		}
		return session.Messages[1].Review
	}
	id, r := propose(771)
	saved := confirm(id, r)
	if saved.State != chat.ReviewCreated {
		t.Fatal(saved)
	}
	manager := taskconfig.Manager{Store: app.Store}
	draft, err := manager.Get(t.Context(), saved.ResultID)
	if err != nil || draft.Status != "draft" || len(draft.Repositories) != 2 || draft.Repositories[1].ProjectID != selected.ID || draft.Repositories[1].BaseCommit != selected.Head {
		t.Fatal(draft, err)
	}
	id, r = propose(772)
	if _, err := projects.SetExecutionPreference(t.Context(), selected.ID, "sandbox", selected.Revision, "fixture"); err != nil {
		t.Fatal(err)
	}
	if stale := confirm(id, r); stale.State != chat.ReviewConflict || stale.ResultID != "" {
		t.Fatal(stale)
	}
}
