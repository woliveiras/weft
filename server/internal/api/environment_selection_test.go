package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environment"
)

func TestDiscoverReviewAcceptSaveBuild(t *testing.T) {
	root := t.TempDir()
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
	const graphDoc = `apiVersion: weft.io/v1
kind: Graph
metadata: {name: selection}
spec:
  nodes: [{id: worker, runtime: {kind: echo}}]
`
	const base = "/api/v1/graphs/selection"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", graphDoc, nil); code != 201 {
		t.Fatalf("create %d", code)
	}
	dir := filepath.Join(root, "selection")
	if err := os.MkdirAll(filepath.Join(dir, ".weft"), 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, ".weft/environment.yaml")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(source, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("image: alpine\nsetup: [echo ready]")
	var discovery environment.Discovery
	if code := call(t, srv, "GET", base+"/environment/discovery", viewerTok, "", "", &discovery); code != 200 {
		t.Fatalf("discover %d", code)
	}
	selection := environment.Selection{Source: discovery.Source, Path: discovery.Candidates[0].Path, SHA256: discovery.Candidates[0].SHA256}
	body, _ := json.Marshal(selection)
	if code := call(t, srv, "POST", base+"/environment/resolve", "", "application/json", string(body), nil); code != 401 {
		t.Fatalf("anonymous %d", code)
	}
	for _, bad := range []string{`{}`, `{"path":"../outside"}`, string(body) + ` {}`, `{"unknown":true}`} {
		if code := call(t, srv, "POST", base+"/environment/resolve", viewerTok, "application/json", bad, nil); code != 400 {
			t.Fatalf("invalid %s: %d", bad, code)
		}
	}
	var env domain.EnvironmentSpec
	if code := call(t, srv, "POST", base+"/environment/resolve", viewerTok, "application/json", string(body), &env); code != 200 || env.Image != "alpine" || env.Origin == nil {
		t.Fatalf("resolve %d %+v", code, env)
	}
	var saved struct {
		Graph domain.Graph `json:"graph"`
	}
	if code := call(t, srv, "GET", base, viewerTok, "", "", &saved); code != 200 || saved.Graph.Spec.Environment != nil {
		t.Fatalf("preview mutated graph %d %+v", code, saved)
	}
	saved.Graph.Spec.Environment = &env
	graphJSON, _ := json.Marshal(saved.Graph)
	if code := callIfMatch(t, srv, "PUT", base, viewerTok, string(graphJSON), 1, nil); code != 403 {
		t.Fatalf("viewer save %d", code)
	}
	write("image: debian")
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(graphJSON), 1, nil); code != 400 {
		t.Fatalf("stale review save %d", code)
	}
	write("image: alpine\nsetup: [echo ready]")
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(graphJSON), 1, &saved); code != 200 {
		t.Fatalf("accept save %d", code)
	}
	if saved.Graph.Spec.Environment.Origin.SHA256 != selection.SHA256 || saved.Graph.Metadata.Version != 2 {
		t.Fatalf("origin/version not persisted %+v", saved)
	}
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(graphJSON), 1, nil); code != 409 {
		t.Fatalf("concurrent save %d", code)
	}
	var state envView
	if code := call(t, srv, "GET", base+"/environment", viewerTok, "", "", &state); code != 200 || state.State != "absent" {
		t.Fatalf("accept started build %d %+v", code, state)
	}
	write("image: debian")
	if code := call(t, srv, "POST", base+"/environment/build", operatorTok, "application/json", `{}`, nil); code != 400 {
		t.Fatalf("stale build %d", code)
	}
	if data, err := os.ReadFile(log); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("Docker called before validation: %s", data)
	}
	write("image: alpine\nsetup: [echo ready]")
	if code := call(t, srv, "POST", base+"/environment/build", operatorTok, "application/json", `{}`, &state); code != 202 {
		t.Fatalf("build %d %+v", code, state)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		call(t, srv, "GET", base+"/environment", viewerTok, "", "", &state)
		if state.State != "building" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state.State != "ready" {
		t.Fatalf("build %+v", state)
	}
	write("image: debian")
	if code := call(t, srv, "POST", base+"/environment/build", operatorTok, "application/json", `{}`, nil); code != 400 {
		t.Fatalf("cached build bypassed validation %d", code)
	}
}

func TestRepositoryContextAcceptanceAndStaleBuild(t *testing.T) {
	root := t.TempDir()
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
	const base = "/api/v1/graphs/context"
	const doc = "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: context}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatalf("create %d", code)
	}
	dir := filepath.Join(root, "context")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile", "FROM alpine\nCOPY app /app\n")
	write("app", "one")
	var discovery environment.Discovery
	if code := call(t, srv, "GET", base+"/environment/discovery", viewerTok, "", "", &discovery); code != 200 {
		t.Fatalf("discover %d", code)
	}
	selection, _ := json.Marshal(environment.Selection{Source: discovery.Source, Path: discovery.Candidates[0].Path, SHA256: discovery.Candidates[0].SHA256})
	var env domain.EnvironmentSpec
	if code := call(t, srv, "POST", base+"/environment/resolve", viewerTok, "application/json", string(selection), &env); code != 200 || env.Context != "." || env.Origin == nil || env.Origin.ContextSHA256 == "" {
		t.Fatalf("resolve %d %+v", code, env)
	}
	var saved struct {
		Graph domain.Graph `json:"graph"`
	}
	if code := call(t, srv, "GET", base, viewerTok, "", "", &saved); code != 200 {
		t.Fatalf("get %d", code)
	}
	saved.Graph.Spec.Environment = &env
	body, _ := json.Marshal(saved.Graph)
	write("app", "changed")
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(body), 1, nil); code != 400 {
		t.Fatalf("stale context save %d", code)
	}
	write("app", "one")
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(body), 1, &saved); code != 200 || saved.Graph.Spec.Environment.Origin.ContextSHA256 != env.Origin.ContextSHA256 {
		t.Fatalf("save %d %+v", code, saved)
	}
	write("app", "changed after save")
	if code := call(t, srv, "POST", base+"/environment/build", operatorTok, "application/json", `{}`, nil); code != 400 {
		t.Fatalf("stale context build %d", code)
	}
	if data, err := os.ReadFile(log); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("Docker called before context validation: %s", data)
	}
}

func TestBuildTargetReviewPersistenceAndValidation(t *testing.T) {
	root := t.TempDir()
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
	const base = "/api/v1/graphs/targets"
	doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: targets}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatalf("create %d", code)
	}
	dir := filepath.Join(root, "targets")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	source := "FROM alpine AS build\nRUN echo hi > /artifact\nFROM alpine AS runtime\nCOPY --from=build /artifact /artifact\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	var discovery environment.Discovery
	if code := call(t, srv, "GET", base+"/environment/discovery", viewerTok, "", "", &discovery); code != 200 || len(discovery.Candidates[0].Targets) != 2 {
		t.Fatalf("discover %d %+v", code, discovery)
	}
	selection := environment.Selection{Source: discovery.Source, Path: discovery.Candidates[0].Path, SHA256: discovery.Candidates[0].SHA256, Target: "missing"}
	payload, _ := json.Marshal(selection)
	if code := call(t, srv, "POST", base+"/environment/resolve", viewerTok, "application/json", string(payload), nil); code != 400 {
		t.Fatalf("unknown stage %d", code)
	}
	selection.Target = "build"
	payload, _ = json.Marshal(selection)
	var env domain.EnvironmentSpec
	if code := call(t, srv, "POST", base+"/environment/resolve", viewerTok, "application/json", string(payload), &env); code != 200 || env.Target != "build" || env.Origin.Target != "build" {
		t.Fatalf("review %d %+v", code, env)
	}
	var saved struct {
		Graph domain.Graph `json:"graph"`
	}
	if code := call(t, srv, "GET", base, viewerTok, "", "", &saved); code != 200 {
		t.Fatalf("get %d", code)
	}
	saved.Graph.Spec.Environment = &env
	env.Target = "runtime"
	body, _ := json.Marshal(saved.Graph)
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(body), 1, nil); code != 400 {
		t.Fatalf("target tampering %d", code)
	}
	env.Target = "build"
	body, _ = json.Marshal(saved.Graph)
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(body), 1, &saved); code != 200 || saved.Graph.Spec.Environment.Target != "build" {
		t.Fatalf("save %d %+v", code, saved)
	}
	// Removing/renaming the selected stage invalidates the accepted source before Docker.
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine AS renamed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "POST", base+"/environment/build", operatorTok, "application/json", `{}`, nil); code != 400 {
		t.Fatalf("stale stage build %d", code)
	}
	if data, err := os.ReadFile(log); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("Docker called before target validation: %s", data)
	}
}

func TestExternalCopyReviewSaveAndRevalidation(t *testing.T) {
	root := t.TempDir()
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
	const base = "/api/v1/graphs/externalcopy"
	doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: externalcopy}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatalf("create %d", code)
	}
	dir := filepath.Join(root, "externalcopy")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(ref string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\nCOPY --from="+ref+" /etc/alpine-release /release\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	resolve := func(env *domain.EnvironmentSpec) int {
		t.Helper()
		var discovery environment.Discovery
		if code := call(t, srv, "GET", base+"/environment/discovery", viewerTok, "", "", &discovery); code != 200 {
			t.Fatalf("discover %d", code)
		}
		selection, _ := json.Marshal(environment.Selection{Source: discovery.Source, Path: discovery.Candidates[0].Path, SHA256: discovery.Candidates[0].SHA256})
		return call(t, srv, "POST", base+"/environment/resolve", viewerTok, "application/json", string(selection), env)
	}
	write("https://invalid.example/image")
	var env domain.EnvironmentSpec
	if code := resolve(&env); code != 400 {
		t.Fatalf("invalid reference %d", code)
	}
	write("registry.example:5000/team/tools:v1")
	if code := resolve(&env); code != 200 || !strings.Contains(env.Dockerfile, "--from=registry.example:5000/team/tools:v1") {
		t.Fatalf("review %d %+v", code, env)
	}
	var saved struct {
		Graph domain.Graph `json:"graph"`
	}
	if code := call(t, srv, "GET", base, viewerTok, "", "", &saved); code != 200 {
		t.Fatalf("get %d", code)
	}
	saved.Graph.Spec.Environment = &env
	body, _ := json.Marshal(saved.Graph)
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(body), 1, &saved); code != 200 {
		t.Fatalf("save %d", code)
	}
	if saved.Graph.Spec.Environment.Dockerfile != env.Dockerfile {
		t.Fatal("external image reference not persisted")
	}
	write("registry.example:5000/team/tools:v2")
	if code := call(t, srv, "POST", base+"/environment/build", operatorTok, "application/json", `{}`, nil); code != 400 {
		t.Fatalf("changed image bypassed review %d", code)
	}
	if data, err := os.ReadFile(log); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("review/save/stale validation invoked Docker: %s", data)
	}
}

func TestComposeCompanionsReviewAndSaveAPI(t *testing.T) {
	root := t.TempDir()
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
	doc := `apiVersion: weft.io/v1
kind: Graph
metadata: {name: compose-review}
spec: {nodes: [{id: worker, runtime: {kind: echo}}]}
`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatal(code)
	}
	dir := filepath.Join(root, "compose-review")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "compose.yaml")
	body := []byte("services: {dev: {image: alpine, depends_on: [cache]}, cache: {image: alpine, command: [sleep, infinity]}}")
	if err := os.WriteFile(source, body, 0600); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/graphs/compose-review"
	var d environment.Discovery
	if code := call(t, srv, "GET", base+"/environment/discovery", viewerTok, "", "", &d); code != 200 {
		t.Fatal(code)
	}
	sel, _ := json.Marshal(environment.Selection{Source: d.Source, Path: d.Candidates[0].Path, SHA256: d.Candidates[0].SHA256, Service: "dev"})
	var env domain.EnvironmentSpec
	if code := call(t, srv, "POST", base+"/environment/resolve", viewerTok, "application/json", string(sel), &env); code != 200 || len(env.Companions) != 1 {
		t.Fatal(code, env)
	}
	var saved struct {
		Graph domain.Graph `json:"graph"`
	}
	call(t, srv, "GET", base, viewerTok, "", "", &saved)
	if saved.Graph.Spec.Environment != nil {
		t.Fatal("review saved environment")
	}
	saved.Graph.Spec.Environment = &env
	graphJSON, _ := json.Marshal(saved.Graph)
	if code := callIfMatch(t, srv, "PUT", base, viewerTok, string(graphJSON), 1, nil); code != 403 {
		t.Fatal(code)
	}
	if code := callIfMatch(t, srv, "PUT", base, adminTok, string(graphJSON), 1, &saved); code != 200 {
		t.Fatal(code)
	}
	if len(saved.Graph.Spec.Environment.Companions) != 1 {
		t.Fatal("companions lost on persistence")
	}
	if err := os.WriteFile(source, append(body, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "POST", base+"/environment/build", operatorTok, "application/json", "{}", nil); code != 400 {
		t.Fatal("stale helper review built", code)
	}
	if data, err := os.ReadFile(log); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatal("Docker ran during review", string(data))
	}
}
