package api_test

import (
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/environment"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentDiscoveryAPI(t *testing.T) {
	root := t.TempDir()
	srv, _ := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root })
	const graphDoc = `apiVersion: weft.io/v1
kind: Graph
metadata: {name: discovery}
spec:
  nodes: [{id: worker, runtime: {kind: echo}}]
`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", graphDoc, nil); code != 201 {
		t.Fatalf("create %d", code)
	}
	endpoint := "/api/v1/graphs/discovery/environment/discovery"
	if code := call(t, srv, "GET", endpoint, "", "", "", nil); code != 401 {
		t.Fatalf("anonymous %d", code)
	}
	if code := call(t, srv, "GET", endpoint, viewerTok, "", "", nil); code != 400 {
		t.Fatalf("missing workspace %d", code)
	}
	dir := filepath.Join(root, "discovery")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var result environment.Discovery
	if code := call(t, srv, "GET", endpoint, viewerTok, "", "", &result); code != 200 || result.Status != "proposal_required" {
		t.Fatalf("empty %d %+v", code, result)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {web: {image: node}, db: {image: postgres}}"), 0644); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", endpoint, viewerTok, "", "", &result); code != 200 || result.Source != "compose" || len(result.Candidates[0].Services) != 2 {
		t.Fatalf("compose %d %+v", code, result)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0644); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", endpoint, viewerTok, "", "", &result); code != 200 || result.Source != "dockerfile" {
		t.Fatalf("dockerfile %d %+v", code, result)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".weft"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".weft/environment.yaml"), []byte("image: [invalid]"), 0644); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "GET", endpoint, viewerTok, "", "", &result); code != 200 || result.Source != "weft" || result.Status != "invalid" {
		t.Fatalf("invalid priority %d %+v", code, result)
	}

	// Discovery must not persist an environment or start a build.
	var state struct {
		Defined bool
		State   string
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/discovery/environment", viewerTok, "", "", &state); code != 200 || state.Defined || state.State != "absent" {
		t.Fatalf("mutated %d %+v", code, state)
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/missing/environment/discovery", viewerTok, "", "", nil); code != 404 {
		t.Fatalf("unknown %d", code)
	}
	configured := `apiVersion: weft.io/v1
kind: Graph
metadata: {name: configured}
spec:
  environment: {image: alpine}
  nodes: [{id: worker, runtime: {kind: echo}}]
`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", configured, nil); code != 201 {
		t.Fatalf("create configured %d", code)
	}
	if code := call(t, srv, "GET", "/api/v1/graphs/configured/environment/discovery", viewerTok, "", "", &result); code != 200 || result.Source != "interface" || result.Status != "configured" || len(result.Candidates) != 0 {
		t.Fatalf("override %d %+v", code, result)
	}

}
