package api_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/environment"
)

func TestRepositoryEnvironmentReuseThroughAPI(t *testing.T) {
	root := t.TempDir()
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
	for _, id := range []string{"first", "second"} {
		if err := os.MkdirAll(filepath.Join(root, id, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, id, "go.mod"), []byte("module fixture\n"), 0644); err != nil {
			t.Fatal(err)
		}
		doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: " + id + "}\nspec:\n  workspace: {repo: 'https://example.test/team/repo.git'}\n  environment:\n    image: alpine:3.22\n    caches: [/home/sandbox/.cache/pip]\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
		if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
			t.Fatal(code)
		}
	}
	build := func(id string) environment.View {
		t.Helper()
		base := "/api/v1/graphs/" + id + "/environment"
		if code := call(t, srv, "POST", base+"/build", operatorTok, "application/json", `{}`, nil); code != 202 {
			t.Fatal(code)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var v environment.View
			if code := call(t, srv, "GET", base, viewerTok, "", "", &v); code != 200 {
				t.Fatal(code)
			}
			if v.State == environment.StateFailed {
				t.Fatal(v.Error)
			}
			if v.State == environment.StateReady {
				return v
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("build timeout")
		return environment.View{}
	}
	first, second := build("first"), build("second")
	if first.Reused || !second.Reused || first.Image != second.Image || first.ReuseKey != second.ReuseKey || first.Caches[0].Volume == second.Caches[0].Volume {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "buildx build --network default --builder ") != 1 {
		t.Fatal(string(data))
	}
	if code := call(t, srv, "POST", "/api/v1/graphs/first/environment/reset", adminTok, "application/json", `{}`, nil); code != 200 {
		t.Fatal(code)
	}
	var secondView environment.View
	if code := call(t, srv, "GET", "/api/v1/graphs/second/environment", viewerTok, "", "", &secondView); code != 200 || !secondView.UpToDate {
		t.Fatalf("reset affected second %d %+v", code, secondView)
	}
	if err := os.WriteFile(filepath.Join(root, "second", "go.mod"), []byte("module changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/second/environment", viewerTok, "", "", &secondView); code != 200 || secondView.UpToDate {
		t.Fatalf("lockfile change ignored %d %+v", code, secondView)
	}
	if v := build("second"); v.Reused || v.ReuseKey == second.ReuseKey {
		t.Fatalf("invalidation %+v", v)
	}
	// Workspace contents remain separate even though the prepared image is shared.
	if err := os.WriteFile(filepath.Join(root, "first", "task-output"), []byte("first only"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "second", "task-output")); !os.IsNotExist(err) {
		t.Fatalf("workspace isolation: %v", err)
	}
}
