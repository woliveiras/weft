package api_test

import (
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"github.com/woliveiras/weft/server/internal/workspace"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectTaskWorktreeThroughAPI(t *testing.T) {
	srv, _ := setupApp(t, nil)
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
	var ps []projectconfig.Project
	if c := call(t, srv, "GET", "/api/v1/projects", viewerTok, "", "", &ps); c != 200 || len(ps) != 1 || ps[0].ID != p.ID {
		t.Fatal(c, ps)
	}
	if c := call(t, srv, "POST", "/api/v1/projects", adminTok, "application/json", `{"name":"x","path":"relative"}`, nil); c != 400 {
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
	var review taskrun.Review
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", adminTok, "", "", &review); c != 200 || !review.CanStart || review.Project == nil {
		t.Fatal(c, review)
	}
	if !strings.Contains(review.WorkspacePath, "task-run-") {
		t.Fatal(review.WorkspacePath)
	}
	if c := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); c != 202 {
		t.Fatal(c)
	}
	waitFor(t, 5*time.Second, "project task", func() bool {
		var v taskrun.View
		call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", adminTok, "", "", &v)
		return v.State == "succeeded" && v.WorkspacePath == review.WorkspacePath
	})
	if _, err := os.Stat(filepath.Join(review.WorkspacePath, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(review.WorkspacePath, "new-file"), []byte("task change"), 0600); err != nil {
		t.Fatal(err)
	}
	var inspection workspace.Inspection
	endpoint := "/api/v1/graphs/" + taskrun.ExecutionID(d.ID) + "/git/status"
	if code := call(t, srv, "GET", endpoint, viewerTok, "", "", &inspection); code != 200 || !inspection.Repository || !strings.Contains(inspection.Content, "new-file") {
		t.Fatalf("task inspection: %d %+v", code, inspection)
	}

	var progress taskrun.View
	if code := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress); code != 200 || len(progress.Workspaces) != 1 {
		t.Fatal(code, progress)
	}
	scope := progress.Workspaces[0]
	if scope.InspectionNode != input.NodeID || scope.Path == review.WorkspacePath || scope.Branch == "" {
		t.Fatal(scope)
	}
	if err := os.WriteFile(filepath.Join(scope.Path, "agent-file"), []byte("agent change"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", endpoint+"?node="+input.NodeID+"&replica=0", viewerTok, "", "", &inspection); code != 200 || !strings.Contains(inspection.Content, "agent-file") || strings.Contains(inspection.Content, "new-file") {
		t.Fatal(code, inspection)
	}
	if code := call(t, srv, "GET", endpoint+"?node=missing&replica=0", viewerTok, "", "", nil); code == 200 {
		t.Fatal("unknown scope accepted")
	}
	// A modified .git pointer must not expose a different repository.
	if err := os.WriteFile(filepath.Join(review.WorkspacePath, ".git"), []byte("gitdir: /outside/fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", endpoint, viewerTok, "", "", nil); code == 200 {
		t.Fatal("redirected task metadata accepted")
	}
	if b, err := os.ReadFile(filepath.Join(repo, "uncommitted")); err != nil || string(b) != "preserve" {
		t.Fatal("dirty source changed", err)
	}
}

func TestTaskCleanupRequiresStoppedScopeAndReview(t *testing.T) {
	srv, _ := setupApp(t, nil)
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
	}
	b, _ := json.Marshal(projectconfig.Input{Name: "Cleanup fixture", Path: repo})
	var p projectconfig.Project
	if code := call(t, srv, "POST", "/api/v1/projects", adminTok, "application/json", string(b), &p); code != 201 {
		t.Fatal(code)
	}
	call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil)
	var input taskconfig.Input
	if err := json.Unmarshal([]byte(taskBody), &input); err != nil {
		t.Fatal(err)
	}
	input.ProjectID = p.ID
	input.BaseCommit = p.Head
	input.ResultNodeID = input.NodeID
	b, _ = json.Marshal(input)
	var d taskconfig.Draft
	call(t, srv, "POST", "/api/v1/tasks", adminTok, "application/json", string(b), &d)
	if code := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); code != 202 {
		t.Fatal(code)
	}
	waitFor(t, 5*time.Second, "completion", func() bool {
		var v taskrun.View
		call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", adminTok, "", "", &v)
		return v.State == "succeeded"
	})
	endpoint := "/api/v1/tasks/" + d.ID + "/cleanup"
	if code := call(t, srv, "GET", endpoint, adminTok, "", "", nil); code != 409 {
		t.Fatal("running graph cleanup", code)
	}
	call(t, srv, "POST", "/api/v1/tasks/"+d.ID+"/cancel", adminTok, "application/json", `{"confirm":true}`, nil)
	waitFor(t, 5*time.Second, "cancelled", func() bool {
		var v taskrun.View
		call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", adminTok, "", "", &v)
		return v.State == "cancelled"
	})
	var preview projectconfig.CleanupPreview
	if code := call(t, srv, "GET", endpoint, adminTok, "", "", &preview); code != 200 || !preview.Clean {
		t.Fatal(code, preview)
	}
	body := `{"confirm":true,"fingerprint":"` + preview.Fingerprint + `"}`
	for _, tok := range []string{operatorTok, viewerTok} {
		if code := call(t, srv, "POST", endpoint, tok, "application/json", body, nil); code != 403 {
			t.Fatal(code)
		}
	}
	if code := call(t, srv, "POST", endpoint, adminTok, "application/json", `{"confirm":false}`, nil); code != 400 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", endpoint+"?node=unknown", adminTok, "", "", nil); code != 404 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", endpoint, adminTok, "application/json", body, nil); code != 200 {
		t.Fatal(code)
	}
	if _, err := os.Stat(preview.Path); !os.IsNotExist(err) {
		t.Fatal("checkout not removed", err)
	}
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "refs/heads/"+preview.Branch)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal("branch lost", e, string(b))
	}
}

func TestProjectPreferencesPreserveAuthorizationAndRevision(t *testing.T) {
	srv, _ := setupApp(t, nil)
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
	}
	b, _ := json.Marshal(projectconfig.Input{Name: "Preference fixture", Path: repo})
	var p projectconfig.Project
	call(t, srv, "POST", "/api/v1/projects", adminTok, "application/json", string(b), &p)
	update := func(token, mode string, revision int64) int {
		t.Helper()
		req, err := http.NewRequest("PUT", srv.URL+"/api/v1/projects/"+p.ID+"/preferences", strings.NewReader(`{"executionMode":"`+mode+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("If-Match", fmt.Sprint(revision))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	for _, tok := range []string{operatorTok, viewerTok} {
		if code := update(tok, "sandbox", p.Revision); code != 403 {
			t.Fatal(code)
		}
	}
	if code := update(adminTok, "local", p.Revision); code != 400 {
		t.Fatal("non-native local preference", code)
	}
	if code := update(adminTok, "invalid", p.Revision); code != 400 {
		t.Fatal(code)
	}
	if code := update(adminTok, "sandbox", p.Revision); code != 200 {
		t.Fatal(code)
	}
	if code := update(adminTok, "workflow", p.Revision); code != 409 {
		t.Fatal("stale write", code)
	}
	var projects []projectconfig.Project
	call(t, srv, "GET", "/api/v1/projects", viewerTok, "", "", &projects)
	if len(projects) != 1 || projects[0].ExecutionMode != "sandbox" || projects[0].Path != p.Path || projects[0].Head != p.Head {
		t.Fatal(projects)
	}
}

func TestProjectChecksAreAdminOnlyValidatedAndRevisioned(t *testing.T) {
	srv, _ := setupApp(t, nil)
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
	}
	b, _ := json.Marshal(projectconfig.Input{Name: "Checks fixture", Path: repo})
	var p projectconfig.Project
	call(t, srv, "POST", "/api/v1/projects", adminTok, "application/json", string(b), &p)
	update := func(token, body string, revision int64) int {
		t.Helper()
		req, err := http.NewRequest("PUT", srv.URL+"/api/v1/projects/"+p.ID+"/checks", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if revision > 0 {
			req.Header.Set("If-Match", fmt.Sprint(revision))
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	valid := `{"checks":[{"name":"test","command":"go test ./..."},{"name":"lint","command":"go vet ./...","timeoutSec":120}]}`
	for _, tok := range []string{operatorTok, viewerTok} {
		if code := update(tok, valid, p.Revision); code != 403 {
			t.Fatal(code)
		}
	}
	for _, body := range []string{`{"checks":[{"name":"Bad","command":"x"}]}`, `{"checks":[{"name":"a","command":""}]}`, `{"checks":[],"extra":1}`, `not json`} {
		if code := update(adminTok, body, p.Revision); code != 400 {
			t.Fatal(body, code)
		}
	}
	if code := update(adminTok, valid, 0); code != 400 {
		t.Fatal("If-Match is required", code)
	}
	if code := update(adminTok, valid, p.Revision); code != 200 {
		t.Fatal(code)
	}
	if code := update(adminTok, `{"checks":[]}`, p.Revision); code != 409 {
		t.Fatal("stale write", code)
	}
	var projects []projectconfig.Project
	call(t, srv, "GET", "/api/v1/projects", viewerTok, "", "", &projects)
	if len(projects) != 1 || len(projects[0].Checks) != 2 || projects[0].Checks[1].TimeoutSec != 120 || projects[0].Checks[0].Command != "go test ./..." {
		t.Fatal(projects)
	}
}
