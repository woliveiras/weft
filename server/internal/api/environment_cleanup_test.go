package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/environment"
)

func TestSharedCleanupAPI(t *testing.T) {
	bin, _ := fakeDaemonCLI(t)
	root := t.TempDir()
	srv, _ := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin; c.Workspaces.Root = root })
	endpoint := "/api/v1/environments/cleanup"
	for _, token := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "POST", endpoint, token, "application/json", `{}`, nil); code != 403 {
			t.Fatal(code)
		}
	}
	for _, body := range []string{`{"dryRun":false}`, `{"dryRun":false,"generations":["user-data"]}`, `{"force":true}`, `{} {}`} {
		if code := call(t, srv, "POST", endpoint, adminTok, "application/json", body, nil); code != 400 {
			t.Fatalf("%s: %d", body, code)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "cleanup", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	doc := `apiVersion: weft.io/v1
kind: Graph
metadata: {name: cleanup}
spec:
  workspace: {repo: "https://example.test/cleanup.git"}
  environment: {image: "alpine:3.22", caches: ["/home/sandbox/.cache/pip"]}
  nodes: [{id: worker, runtime: {kind: echo}}]
`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", "/api/v1/graphs/cleanup/environment/build", operatorTok, "application/json", `{}`, nil); code != 202 {
		t.Fatal(code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var v environment.View
		if code := call(t, srv, "GET", "/api/v1/graphs/cleanup/environment", viewerTok, "", "", &v); code != 200 {
			t.Fatal(code)
		}
		if v.State == environment.StateFailed {
			t.Fatal(v.Error)
		}
		if v.State == environment.StateReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("build timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var preview environment.CleanupResult
	if code := call(t, srv, "POST", endpoint, adminTok, "application/json", `{}`, &preview); code != 200 {
		t.Fatal(code)
	}
	if !preview.DryRun || len(preview.Generations) != 1 || preview.Generations[0].Eligible {
		t.Fatalf("%+v", preview)
	}
	id := preview.Generations[0].ID
	body, err := json.Marshal(map[string]any{"dryRun": false, "generations": []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	var result environment.CleanupResult
	if code := call(t, srv, "POST", endpoint, adminTok, "application/json", string(body), &result); code != 200 || result.Generations[0].Removed {
		t.Fatalf("%d %+v", code, result)
	}
	if code := call(t, srv, "POST", "/api/v1/graphs/cleanup/environment/reset", adminTok, "application/json", `{}`, nil); code != 200 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", endpoint, adminTok, "application/json", string(body), &result); code != 200 || !result.Generations[0].Removed {
		t.Fatalf("%d %+v", code, result)
	}
	if code := call(t, srv, "POST", endpoint, adminTok, "application/json", string(body), &result); code != 200 || !result.Generations[0].Missing {
		t.Fatalf("retry %d %+v", code, result)
	}
}
