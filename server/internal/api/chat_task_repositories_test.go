package api_test

import (
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestChatProjectAdmissionPinsCommitAndRejectsProjectDrift(t *testing.T) {
	srv, app := setupApp(t, nil)
	if err := app.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "chat-fixture", Type: "openai"}, Provider: chatTaskStartFixture{}}}, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.PutSetting(t.Context(), "chat/model", []byte(`{"provider":"chat-fixture","name":"fixture"}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "uncommitted"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(projectconfig.Input{Name: "Project", Path: repo})
	for _, tok := range []string{viewerTok, operatorTok} {
		if c := call(t, srv, "POST", "/api/v1/projects", tok, "application/json", string(body), nil); c != 403 {
			t.Fatal(c)
		}
	}
	var p projectconfig.Project
	if c := call(t, srv, "POST", "/api/v1/projects", adminTok, "application/json", string(body), &p); c != 201 {
		t.Fatal(c)
	}
	var workflow domain.Graph
	if err := json.Unmarshal([]byte(taskGraph), &workflow); err != nil {
		t.Fatal(err)
	}
	workflow.Spec.Nodes[0].Workspace.Mode = domain.WorkspaceWorktree
	graphBody, _ := json.Marshal(workflow)
	call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(graphBody), nil)
	var input taskconfig.Input
	if err := json.Unmarshal([]byte(taskBody), &input); err != nil {
		t.Fatal(err)
	}
	input.ProjectID = p.ID
	input.BaseCommit = p.Head
	input.ResultNodeID = input.NodeID
	b, _ := json.Marshal(input)
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", adminTok, "application/json", string(b), &d); c != 201 {
		t.Fatal(c)
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

	stale := propose(1, d.ID, adminTok)
	r := stale.Messages[1].Review
	if r == nil || r.TaskStart == nil || len(r.TaskStart.Repositories) != 1 || r.TaskStart.Repositories[0].Commit != p.Head || r.TaskStart.Repositories[0].Project != p.ID {
		t.Fatal(stale.Messages)
	}
	m := projectconfig.Manager{Store: app.Store}
	if _, err := m.SetChecks(t.Context(), p.ID, []domain.Check{{Name: "test", Command: "true"}}, p.Revision, "fixture"); err != nil {
		t.Fatal(err)
	}
	if result := confirm(stale); result.Messages[1].Review.State != chat.ReviewConflict {
		t.Fatal(result.Messages)
	}
	next := propose(2, d.ID, adminTok)
	r = next.Messages[1].Review
	if r == nil || r.TaskStart.Repositories[0].Revision == p.Revision || len(r.TaskStart.Repositories[0].Checks) != 1 {
		t.Fatal(next.Messages)
	}
	for range 2 {
		if result := confirm(next); result.Messages[1].Review.State != chat.ReviewAccepted {
			t.Fatal(result.Messages)
		}
	}
	waitFor(t, 5*time.Second, "admitted project task", func() bool {
		var progress taskrun.View
		return call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress) == 200 && progress.State == "succeeded" && progress.WorkspacePath != ""
	})
	if saved, err := os.ReadFile(filepath.Join(repo, "uncommitted")); err != nil || string(saved) != "preserve" {
		t.Fatal("changed source", err)
	}
}
