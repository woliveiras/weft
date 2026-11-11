package api_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestWorkspaceInspectionAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "inspect", Name: "inspect"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}}}
	if _, _, err := a.Engine.SaveGraph(context.Background(), g, 0, "test"); err != nil {
		t.Fatal(err)
	}
	root, err := a.API.Workspaces.Ensure(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "/api/v1/graphs/inspect/"
	var result workspace.Inspection
	if c := call(t, srv, "GET", prefix+"git/status", viewerTok, "", "", &result); c != 200 || result.Repository {
		t.Fatalf("no Git: %d %+v", c, result)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "fixture baseline")
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"status", "diff", "history"} {
		if c := call(t, srv, "GET", prefix+"git/"+view, viewerTok, "", "", &result); c != 200 || !result.Repository || result.Content == "" {
			t.Fatalf("%s: %d %+v", view, c, result)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private"), []byte("outside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, ".git"), filepath.Join(root, "metadata")); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"git/status?node=missing", "git/status?replica=1", "git/reset", "files?node=missing", "files/content?path=.", "files/content?path=.git/config", "files/content?path=metadata/config"} {
		if c := call(t, srv, "GET", prefix+suffix, viewerTok, "", "", nil); c != 400 {
			t.Errorf("%s: %d", suffix, c)
		}
	}
	for _, path := range []string{"../private", "escape/private", "x/../../private", ".git/config"} {
		for _, endpoint := range []string{"files", "files/content", "git/diff"} {
			if c := call(t, srv, "GET", prefix+endpoint+"?path="+url.QueryEscape(path), viewerTok, "", "", nil); c != 400 {
				t.Errorf("%s %s: %d", endpoint, path, c)
			}
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, endpoint := range []string{"files", "files/content", "git/status", "git/diff", "git/history"} {
			if c := call(t, srv, method, prefix+endpoint, adminTok, "application/json", `{}`, nil); c != 405 && c != 404 {
				t.Errorf("mutation %s %s: %d", method, endpoint, c)
			}
		}
	}
	if c := call(t, srv, "GET", prefix+"git/status", "", "", "", nil); c != 401 {
		t.Fatal(c)
	}
	a.API.Workspaces.GitBin = "/nonexistent-git-fixture"
	if c := call(t, srv, "GET", prefix+"git/status", viewerTok, "", "", nil); c != 500 {
		t.Fatalf("Git failure: %d", c)
	}
	if data, err := os.ReadFile(filepath.Join(root, "result.txt")); err != nil || strings.TrimSpace(string(data)) != "after" {
		t.Fatal("read modified workspace")
	}
}

func TestWorkspaceFileContentLimitAndInvalidReplica(t *testing.T) {
	srv, a := setupApp(t, nil)
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "limits", Name: "limits"}, Spec: domain.GraphSpec{Workspace: domain.WorkspaceSpec{Git: true}, Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}}}}
	if _, _, err := a.Engine.SaveGraph(context.Background(), g, 0, "test"); err != nil {
		t.Fatal(err)
	}
	root, err := a.API.Workspaces.Ensure(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Repeat("x", (4<<20)+10)), 0600); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("GET", srv.URL+"/api/v1/graphs/limits/files/content?path=large.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+viewerTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(data) != 4<<20 || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("content limit %d %d", resp.StatusCode, len(data))
	}
	for _, rep := range []string{"-1", "x", "999999999999999999999999999"} {
		if c := call(t, srv, "GET", "/api/v1/graphs/limits/git/status?node=worker&replica="+rep, viewerTok, "", "", nil); c != 400 {
			t.Errorf("replica %s: %d", rep, c)
		}
	}
	if _, _, err := a.API.Workspaces.Replica(context.Background(), g, &g.Spec.Nodes[0], 0); err != nil {
		t.Fatal(err)
	}
	var inspection workspace.Inspection
	if c := call(t, srv, "GET", "/api/v1/graphs/limits/git/status?node=worker&replica=0", viewerTok, "", "", &inspection); c != 200 || inspection.NodeID != "worker" || inspection.Replica == nil || *inspection.Replica != 0 {
		t.Fatalf("worktree metadata: %d %+v", c, inspection)
	}
}
