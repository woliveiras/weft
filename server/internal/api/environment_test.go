package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/storage"
)

// fakeDaemonCLI answers every docker call successfully and logs it.
func fakeDaemonCLI(t *testing.T) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "docker"), filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$1\" in build) cat >/dev/null ;; buildx) [ \"$2\" != build ] || cat >/dev/null ;; version) echo 28.5.2 ;; esac\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

const envGraph = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: env-demo}
spec:
  environment:
    image: golang:1.26
    setup: ["go version"]
    caches: [/home/sandbox/go/pkg/mod]
  defaults: {runtime: {kind: echo}}
  nodes:
    - {id: builder, isolation: {mode: docker}}
    - {id: other}
`

type envView struct {
	Phase         string
	ReuseDecision string
	BuildReasons  []string
	ChangeReasons []string
	State         string
	Defined       bool
	UpToDate      bool
	Image         string
	CurrentHash   string
	Error         string
}

func TestEnvironmentLifecycleThroughTheAPI(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) {
		c.Isolation.Docker.Bin = bin
		c.Isolation.Docker.Host = "tcp://sandbox-daemon:2376"
	})

	// Saving warns that the environment is not built yet.
	var rep struct {
		Valid    bool
		Warnings []struct{ Path, Message string }
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/validate", viewerTok, "application/yaml", envGraph, &rep); c != 200 || !rep.Valid ||
		len(rep.Warnings) == 0 || rep.Warnings[0].Path != "spec.environment" {
		t.Fatalf("validate: %d %+v", c, rep)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); c != 201 {
		t.Fatalf("create: %d", c)
	}
	var v envView
	if c := call(t, srv, "GET", "/api/v1/graphs/env-demo/environment", viewerTok, "", "", &v); c != 200 || v.State != "absent" || !v.Defined || v.CurrentHash == "" {
		t.Fatalf("initial: %d %+v", c, v)
	}

	// Agents that need the environment cannot start before it is built.
	var bad struct {
		Code    string
		Error   string
		Details struct {
			Errors []struct{ Path, Message string }
		}
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/start", operatorTok, "", "", &bad); c != 422 ||
		bad.Details.Errors[0].Path != "spec.environment" || !strings.Contains(bad.Details.Errors[0].Message, "not been built yet") || !strings.HasSuffix(bad.Details.Errors[0].Message, "(needed by builder)") {
		t.Fatalf("start before build: %d %+v", c, bad)
	}
	// A node that does not use it can still start on its own.
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/nodes/other/start", operatorTok, "", "", nil); c != 200 {
		t.Fatalf("start unrelated node: %d", c)
	}
	call(t, srv, "POST", "/api/v1/graphs/env-demo/stop", operatorTok, "", "", nil)

	// Roles: viewers cannot build, operators can.
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/environment/build", viewerTok, "application/json", "{}", nil); c != 403 {
		t.Fatalf("viewer build: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/environment/build", operatorTok, "application/json", "{}", &v); c != 202 || v.State != "building" {
		t.Fatalf("build: %d %+v", c, v)
	}
	if v.Phase != "preparing" || len(v.BuildReasons) != 1 || v.BuildReasons[0] != "not_built" {
		t.Fatalf("build progress API: %+v", v)
	}
	deadline := time.Now().Add(10 * time.Second)
	for v.State == "building" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		call(t, srv, "GET", "/api/v1/graphs/env-demo/environment", viewerTok, "", "", &v)
	}
	if v.State != "ready" || !v.UpToDate || v.Image != "weft-env/env-demo:"+v.CurrentHash {
		t.Fatalf("ready: %+v", v)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/environment/build", operatorTok, "", "", &v); c != 200 || v.State != "ready" {
		t.Fatalf("up-to-date build must be a no-op: %d %+v", c, v)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/start", operatorTok, "", "", nil); c != 200 {
		t.Fatalf("start after build: %d", c)
	}
	call(t, srv, "POST", "/api/v1/graphs/env-demo/stop", operatorTok, "", "", nil)
	waitState(t, srv, "env-demo", "stopped")

	// Changing the definition makes the environment stale.
	changed := strings.Replace(envGraph, `["go version"]`, `["go version", "go env"]`, 1)
	if c := call(t, srv, "PUT", "/api/v1/graphs/env-demo", adminTok, "application/yaml", changed, nil); c != 200 {
		t.Fatalf("update: %d", c)
	}
	call(t, srv, "GET", "/api/v1/graphs/env-demo/environment", viewerTok, "", "", &v)
	if v.State != "ready" || v.UpToDate {
		t.Fatalf("stale: %+v", v)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/start", operatorTok, "", "", &bad); c != 422 || !strings.Contains(bad.Details.Errors[0].Message, "definition changed") {
		t.Fatalf("start with stale environment: %d %+v", c, bad)
	}

	// Reset is admin-only, refused while agents run, and removes the environment.
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/nodes/other/start", operatorTok, "", "", nil); c != 200 {
		t.Fatalf("start other: %d", c)
	}
	waitState(t, srv, "env-demo", "running")
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/environment/reset", adminTok, "", "", &bad); c != 409 || !strings.Contains(bad.Error, "stop it before resetting") {
		t.Fatalf("reset while running: %d %+v", c, bad)
	}
	call(t, srv, "POST", "/api/v1/graphs/env-demo/stop", operatorTok, "", "", nil)
	waitState(t, srv, "env-demo", "stopped")
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/environment/reset", operatorTok, "", "", nil); c != 403 {
		t.Fatalf("operator reset: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/env-demo/environment/reset", adminTok, "", "", &v); c != 200 || v.State != "absent" {
		t.Fatalf("reset: %d %+v", c, v)
	}

	// Build again, then deleting the graph cleans its environment up.
	call(t, srv, "POST", "/api/v1/graphs/env-demo/environment/build", operatorTok, "", "", &v)
	for v.State == "building" {
		time.Sleep(50 * time.Millisecond)
		call(t, srv, "GET", "/api/v1/graphs/env-demo/environment", viewerTok, "", "", &v)
	}
	before, _ := os.ReadFile(log)
	if c := call(t, srv, "DELETE", "/api/v1/graphs/env-demo", adminTok, "", "", nil); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	after, _ := os.ReadFile(log)
	if !strings.Contains(string(after[len(before):]), "volume ls -q --filter label=weft.graph=env-demo") {
		t.Fatalf("deleting the graph must clean its environment:\n%s", after[len(before):])
	}
	if c := call(t, srv, "GET", "/api/v1/graphs/env-demo/environment", viewerTok, "", "", nil); c != 404 {
		t.Fatalf("environment of a deleted graph: %d", c)
	}

	// Graphs without an environment: build is a client error.
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); c != 201 {
		t.Fatalf("create plain graph: %d", c)
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/api-demo/environment/build", operatorTok, "", "", nil); c != 400 {
		t.Fatalf("build without environment: %d", c)
	}
}

func waitState(t *testing.T, srv *httptest.Server, graph, state string) {
	t.Helper()
	var st struct{ State string }
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		call(t, srv, "GET", "/api/v1/graphs/"+graph+"/status", viewerTok, "", "", &st)
		if st.State == state {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("graph %s did not reach %s (now %s)", graph, state, st.State)
}

func TestGraphDeletionWaitsForEnvironmentLease(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const path = "/api/v1/graphs/env-demo"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if code := call(t, srv, "POST", path+"/environment/build", operatorTok, "", "", nil); code != 202 {
		t.Fatalf("build = %d", code)
	}
	waitFor(t, time.Second*5, "environment ready", func() bool {
		var v envView
		call(t, srv, "GET", path+"/environment", viewerTok, "", "", &v)
		return v.State == "ready"
	})
	const key = "environment/env-demo"
	const holder = "environment-reset_api-fixture"
	if ok, err := app.Store.AcquireLease(t.Context(), key, holder, time.Minute); err != nil || !ok {
		t.Fatalf("acquire lease: %v %v", ok, err)
	}
	var conflict struct{ Error string }
	for _, endpoint := range []string{"/build", "/reset"} {
		if code := call(t, srv, "POST", path+"/environment"+endpoint, adminTok, "application/json", `{"force":true}`, &conflict); code != 409 || !strings.Contains(conflict.Error, "reset is already running") {
			t.Fatalf("%s = %d, %+v", endpoint, code, conflict)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), "DELETE", srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+adminTok)
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		response, err := srv.Client().Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		response.Body.Close()
		done <- result{code: response.StatusCode}
	}()
	select {
	case got := <-done:
		t.Fatalf("delete completed under another reset's lease: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
	if code := call(t, srv, "GET", path, viewerTok, "", "", nil); code != 200 {
		t.Fatalf("graph disappeared before cleanup: %d", code)
	}
	if err := app.Store.ReleaseLease(t.Context(), key, holder); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.code != 204 {
			t.Fatalf("delete after reset: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not retry after reset")
	}
	calls, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(calls), "volume ls -q --filter label=weft.graph=env-demo") {
		t.Fatalf("missing cleanup: %v\n%s", err, calls)
	}
	if code := call(t, srv, "POST", path+"/environment/build", operatorTok, "", "", nil); code != 404 {
		t.Fatalf("build after delete: %d", code)
	}
}

func TestGraphDeletionKeepsGraphOnEnvironmentCleanupFailure(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	fail := filepath.Join(filepath.Dir(bin), "fail-cleanup")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"if [ \"$1 $2\" = 'volume ls' ] && [ -f " + fail + " ]; then echo 'daemon unavailable' >&2; exit 1; fi\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	srv, _ := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const path = "/api/v1/graphs/env-demo"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if code := call(t, srv, "POST", path+"/environment/build", operatorTok, "", "", nil); code != 202 {
		t.Fatalf("build = %d", code)
	}
	waitFor(t, 5*time.Second, "environment ready", func() bool {
		var v envView
		call(t, srv, "GET", path+"/environment", viewerTok, "", "", &v)
		return v.State == "ready"
	})
	if err := os.WriteFile(fail, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var conflict struct{ Error string }
	if code := call(t, srv, "DELETE", path, adminTok, "", "", &conflict); code != 409 || !strings.Contains(conflict.Error, "retry graph deletion") {
		t.Fatalf("delete with failed cleanup: %d %+v", code, conflict)
	}
	if code := call(t, srv, "GET", path, viewerTok, "", "", nil); code != 200 {
		t.Fatalf("graph unavailable for retry: %d", code)
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "DELETE", path, adminTok, "", "", nil); code != 204 {
		t.Fatalf("retry delete: %d", code)
	}
}

func TestGraphDeletionWithoutEnvironmentResources(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const path = "/api/v1/graphs/env-demo"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho contacted >> "+log+"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "DELETE", path, adminTok, "", "", nil); code != 204 {
		t.Fatalf("delete without daemon = %d", code)
	}
	if data, _ := os.ReadFile(log); strings.Contains(string(data), "contacted") {
		t.Fatalf("deletion contacted Docker: %s", data)
	}
}

func TestForcedGraphDeletionTracksDeferredCleanup(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const path = "/api/v1/graphs/env-demo"
	const cleanup = "/api/v1/environments/cleanup"
	const retry = `{"dryRun":false,"deferredGraphs":["env-demo"]}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if code := call(t, srv, "POST", path+"/environment/build", operatorTok, "", "", nil); code != 202 {
		t.Fatalf("build = %d", code)
	}
	waitFor(t, 5*time.Second, "environment ready", func() bool {
		var v envView
		call(t, srv, "GET", path+"/environment", viewerTok, "", "", &v)
		return v.State == "ready"
	})
	original, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'daemon offline' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	var problem struct{ Code, Error string }
	if code := call(t, srv, "DELETE", path, adminTok, "", "", &problem); code != 409 || problem.Code != "environment_cleanup_incomplete" || !strings.Contains(problem.Error, "daemon offline") {
		t.Fatalf("normal delete = %d, %+v", code, problem)
	}
	if code := call(t, srv, "DELETE", path+"?force=true", operatorTok, "", "", nil); code != 403 {
		t.Fatalf("operator force = %d", code)
	}
	if code := call(t, srv, "DELETE", path+"?force=unknown", adminTok, "", "", nil); code != 400 {
		t.Fatalf("invalid force = %d", code)
	}
	if code := call(t, srv, "DELETE", path+"?force=true", adminTok, "", "", nil); code != 204 {
		t.Fatalf("admin force = %d", code)
	}
	if code := call(t, srv, "GET", path, viewerTok, "", "", nil); code != 404 {
		t.Fatalf("deleted graph = %d", code)
	}
	var preview environment.CleanupResult
	if code := call(t, srv, "POST", cleanup, adminTok, "application/json", "{}", &preview); code != 200 || len(preview.DeferredGraphs) != 1 || preview.DeferredGraphs[0].Graph != "env-demo" || preview.DeferredGraphs[0].Actor != "admin" {
		t.Fatalf("deferred cleanup = %d, %+v", code, preview)
	}
	if events, err := app.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: "env-demo", Types: []domain.EventType{"environment.graph_delete_forced"}}); err != nil || len(events) != 1 || events[0].Actor != "admin" {
		t.Fatalf("force audit = %+v, %v", events, err)
	}
	if code := call(t, srv, "POST", cleanup, adminTok, "application/json", retry, nil); code != 409 {
		t.Fatalf("retry offline = %d", code)
	}
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); code != 409 {
		t.Fatalf("recreate before cleanup = %d", code)
	}
	if err := os.WriteFile(bin, original, 0755); err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "POST", cleanup, adminTok, "application/json", retry, nil); code != 204 {
		t.Fatalf("retry cleanup = %d", code)
	}
	if data, _ := os.ReadFile(log); !strings.Contains(string(data), "volume ls -q --filter label=weft.graph=env-demo") {
		t.Fatalf("cleanup did not inspect graph volumes: %s", data)
	}
	preview = environment.CleanupResult{}
	if code := call(t, srv, "POST", cleanup, adminTok, "application/json", "{}", &preview); code != 200 || len(preview.DeferredGraphs) != 0 {
		t.Fatalf("deferred cleanup after retry = %d, %+v", code, preview)
	}
}

type resetStatusFailureStore struct{ storage.Store }

func (s resetStatusFailureStore) KVPut(ctx context.Context, ns, key string, value []byte) error {
	if ns == "environment" {
		return errors.New("fixture database unavailable")
	}
	return s.Store.KVPut(ctx, ns, key, value)
}

func TestGraphDeletionStorageFailureIsInternal(t *testing.T) {
	bin, _ := fakeDaemonCLI(t)
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const path = "/api/v1/graphs/env-demo"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", envGraph, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if err := app.Store.KVPut(t.Context(), "environment", "env-demo", []byte(`{"state":"ready"}`)); err != nil {
		t.Fatal(err)
	}
	manager, err := environment.New(environment.Options{Store: resetStatusFailureStore{app.Store}, Workspaces: app.API.Workspaces, DockerBin: bin})
	if err != nil {
		t.Fatal(err)
	}
	app.API.Environments = manager
	var problem struct{ Code, Error string }
	if code := call(t, srv, "DELETE", path, adminTok, "", "", &problem); code != 500 || problem.Code != "internal" || strings.Contains(problem.Error, "fixture") {
		t.Fatalf("delete = %d, %+v", code, problem)
	}
	if code := call(t, srv, "GET", path, viewerTok, "", "", nil); code != 200 {
		t.Fatalf("graph retained = %d", code)
	}
}

func TestGraphStartCannotRaceDeletionCleanup(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	dir := filepath.Dir(bin)
	srv, _ := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const path = "/api/v1/graphs/env-demo"
	// An echo node can start without any environment preflight dependency.
	doc := strings.ReplaceAll(envGraph, "isolation: {mode: docker}", "isolation: {mode: process}")
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if code := call(t, srv, "POST", path+"/environment/build", operatorTok, "", "", nil); code != 202 {
		t.Fatalf("build = %d", code)
	}
	waitFor(t, 5*time.Second, "environment ready", func() bool {
		var v envView
		call(t, srv, "GET", path+"/environment", viewerTok, "", "", &v)
		return v.State == "ready"
	})
	release := filepath.Join(dir, "release")
	entered := filepath.Join(dir, "entered")
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"if [ \"$1 $2\" = 'image ls' ]; then touch " + entered + "; until [ -f " + release + " ]; do sleep .02; done; fi\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "DELETE", srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+adminTok)
	done := make(chan int, 1)
	go func() {
		res, err := srv.Client().Do(req)
		if err != nil {
			done <- 0
			return
		}
		res.Body.Close()
		done <- res.StatusCode
	}()
	waitFor(t, time.Second*3, "cleanup entered", func() bool {
		_, err := os.Stat(entered)
		return err == nil
	})
	for _, start := range []string{"/start", "/nodes/other/start"} {
		if code := call(t, srv, "POST", path+start, operatorTok, "", "", nil); code != 409 {
			t.Fatalf("start during cleanup %s = %d", start, code)
		}
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 204 {
			t.Fatalf("delete = %d", code)
		}
	case <-ctx.Done():
		t.Fatal("deletion stalled")
	}
}

func TestGraphDeletionRechecksStoppedStateAfterWaiting(t *testing.T) {
	bin, log := fakeDaemonCLI(t)
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const path = "/api/v1/graphs/env-demo"
	doc := strings.ReplaceAll(envGraph, "isolation: {mode: docker}", "isolation: {mode: process}")
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if err := app.Store.KVPut(t.Context(), "environment", "env-demo", []byte(`{"state":"ready"}`)); err != nil {
		t.Fatal(err)
	}
	const key, holder = "environment/env-demo", "remote-build"
	if ok, err := app.Store.AcquireLease(t.Context(), key, holder, time.Minute); err != nil || !ok {
		t.Fatalf("seed build lease: %v %v", ok, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "DELETE", srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+adminTok)
	done := make(chan int, 1)
	go func() {
		res, err := srv.Client().Do(req)
		if err != nil {
			done <- 0
			return
		}
		res.Body.Close()
		done <- res.StatusCode
	}()
	select {
	case code := <-done:
		t.Fatalf("delete completed while build held lease: %d", code)
	case <-time.After(150 * time.Millisecond):
	}
	waitFor(t, 2*time.Second, "graph start between delete attempts", func() bool {
		return call(t, srv, "POST", path+"/start", operatorTok, "", "", nil) == 200
	})
	if err := app.Store.ReleaseLease(ctx, key, holder); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 409 {
			t.Fatalf("delete after graph start = %d", code)
		}
	case <-ctx.Done():
		t.Fatal("delete did not recheck graph state")
	}
	if code := call(t, srv, "GET", path, viewerTok, "", "", nil); code != 200 {
		t.Fatalf("running graph disappeared: %d", code)
	}
	if data, _ := os.ReadFile(log); strings.Contains(string(data), "volume ls -q --filter label=weft.graph=env-demo") {
		t.Fatalf("delete cleaned a running graph: %s", data)
	}
}

func TestDeferredCleanupResolvesDifferentDaemonAndInterruptedDeletion(t *testing.T) {
	for _, name := range []string{"different-daemon", "cancel-interrupted", "finish-interrupted"} {
		t.Run(name, func(t *testing.T) {
			interrupted := name != "different-daemon"
			bin, _ := fakeDaemonCLI(t)
			srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
			const graph, cleanup = "/api/v1/graphs/env-demo", "/api/v1/environments/cleanup"
			const create = "/api/v1/graphs"
			if interrupted {
				if code := call(t, srv, "POST", create, adminTok, "application/yaml", envGraph, nil); code != 201 {
					t.Fatalf("create: %d", code)
				}
			}
			if err := app.Store.KVPut(t.Context(), domain.DeferredEnvironmentCleanup, "env-demo", []byte(`{"graph":"env-demo","actor":"original-admin","daemon":"previous-daemon","reason":"Fixture cleanup deferred"}`)); err != nil {
				t.Fatal(err)
			}
			if err := app.Store.KVPut(t.Context(), "environment", "env-demo", []byte(`{"graph":"env-demo","state":"ready"}`)); err != nil {
				t.Fatal(err)
			}
			var preview environment.CleanupResult
			if code := call(t, srv, "POST", cleanup, adminTok, "application/json", "{}", &preview); code != 200 || len(preview.DeferredGraphs) != 1 {
				t.Fatalf("preview: %d %+v", code, preview)
			}
			entry := preview.DeferredGraphs[0]
			if entry.GraphExists != interrupted || entry.DaemonMatches || len(entry.PossibleResources) == 0 {
				t.Fatalf("preview state: %+v", entry)
			}
			retry := `{"dryRun":false,"deferredGraphs":["env-demo"]}`
			if code := call(t, srv, "POST", cleanup, adminTok, "application/json", retry, nil); code != 409 {
				t.Fatalf("cleanup on wrong daemon or existing graph: %d", code)
			}
			if interrupted {
				for _, action := range []string{"/start", "/nodes/other/start", "/environment/build", "/environment/reset"} {
					if code := call(t, srv, "POST", graph+action, adminTok, "", "", nil); code != 409 {
						t.Fatalf("start during interrupted deletion %s: %d", action, code)
					}
				}
				if code := call(t, srv, "DELETE", graph, adminTok, "", "", nil); code != 409 {
					t.Fatalf("immediate delete on wrong daemon: %d", code)
				}
			}
			if name == "finish-interrupted" {
				if code := call(t, srv, "DELETE", graph+"?force=true", adminTok, "", "", nil); code != 204 {
					t.Fatalf("finish interrupted deletion: %d", code)
				}
				if code := call(t, srv, "GET", graph, adminTok, "", "", nil); code != 404 {
					t.Fatalf("graph still exists: %d", code)
				}
				if code := call(t, srv, "POST", create, adminTok, "application/yaml", envGraph, nil); code != 409 {
					t.Fatalf("ID lost its reservation: %d", code)
				}
				return
			}
			abandon := `{"dryRun":false,"abandonDeferred":{"graph":"env-demo","reason":"Verified the original daemon separately"}}`
			if code := call(t, srv, "POST", cleanup, operatorTok, "application/json", abandon, nil); code != 403 {
				t.Fatalf("non-admin abandon: %d", code)
			}
			if code := call(t, srv, "POST", cleanup, adminTok, "application/json", strings.Replace(abandon, "Verified the original daemon separately", " ", 1), nil); code != 400 {
				t.Fatalf("blank reason: %d", code)
			}
			if code := call(t, srv, "POST", cleanup, adminTok, "application/json", abandon, nil); code != 204 {
				t.Fatalf("release reservation: %d", code)
			}
			kind := domain.EventType("environment.deferred_cleanup_abandon_requested")
			if interrupted {
				kind = "environment.deferred_cleanup_cancel_requested"
				if code := call(t, srv, "PUT", graph, adminTok, "application/yaml", envGraph, nil); code != 200 {
					t.Fatalf("edit after cancellation: %d", code)
				}
				if _, err := app.Store.KVGet(t.Context(), "environment", "env-demo"); err != nil {
					t.Fatalf("cancel removed live environment: %v", err)
				}
			} else {
				if code := call(t, srv, "POST", create, adminTok, "application/yaml", envGraph, nil); code != 201 {
					t.Fatalf("recreate after abandonment: %d", code)
				}
				if _, err := app.Store.KVGet(t.Context(), "environment", "env-demo"); !errors.Is(err, storage.ErrNotFound) {
					t.Fatalf("abandon left stale environment: %v", err)
				}
			}
			events, err := app.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: "env-demo", Types: []domain.EventType{kind}})
			if err != nil || len(events) != 1 || events[0].Actor != "admin" || !strings.Contains(string(events[0].Data), "Verified the original daemon separately") ||
				!strings.Contains(string(events[0].Data), "buildx_buildkit_weft-builder-env-demo0_state") {
				t.Fatalf("audit: %+v, %v", events, err)
			}
		})
	}
}

func TestInvalidDeferredCleanupCanBeAbandonedThroughAPI(t *testing.T) {
	bin, _ := fakeDaemonCLI(t)
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	const cleanup = "/api/v1/environments/cleanup"
	ctx := t.Context()
	for id, data := range map[string]string{"invalid": "broken-private-bytes", "valid": `{"graph":"valid","daemon":"other-daemon"}`} {
		if err := app.Store.KVPut(ctx, domain.DeferredEnvironmentCleanup, id, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	var preview struct {
		DeferredGraphs []struct {
			Graph             string
			Invalid           bool
			PossibleResources []string
		}
	}
	if code := call(t, srv, "POST", cleanup, adminTok, "application/json", "{}", &preview); code != 200 || len(preview.DeferredGraphs) != 2 {
		t.Fatalf("preview: %d %+v", code, preview)
	}
	invalid, valid := preview.DeferredGraphs[0], preview.DeferredGraphs[1]
	if invalid.Graph != "invalid" || !invalid.Invalid || len(invalid.PossibleResources) == 0 || valid.Graph != "valid" || valid.Invalid {
		t.Fatalf("preview entries: %+v", preview)
	}
	for _, body := range []string{
		`{"dryRun":false,"abandonDeferred":{"graph":"","reason":"verified"}}`,
		`{"dryRun":false,"deferredGraphs":["valid"],"abandonDeferred":{"graph":"invalid","reason":"verified"}}`,
		`{"dryRun":false,"generations":["valid"],"abandonDeferred":{"graph":"invalid","reason":"verified"}}`,
	} {
		if code := call(t, srv, "POST", cleanup, adminTok, "application/json", body, nil); code != 400 {
			t.Fatalf("invalid selection: %d", code)
		}
	}
	retry := `{"dryRun":false,"deferredGraphs":["invalid"]}`
	if code := call(t, srv, "POST", cleanup, adminTok, "application/json", retry, nil); code != 409 {
		t.Fatalf("retry of corrupt record: %d", code)
	}
	abandon := `{"dryRun":false,"abandonDeferred":{"graph":"invalid","reason":"verified resources"}}`
	key := "graph-transition/invalid"
	if ok, err := app.Store.AcquireLease(ctx, key, "test-holder", time.Minute); err != nil || !ok {
		t.Fatalf("hold transition: %t %v", ok, err)
	}
	if code := call(t, srv, "POST", cleanup, adminTok, "application/json", abandon, nil); code != 409 {
		t.Fatalf("abandon with held lease: %d", code)
	}
	if err := app.Store.ReleaseLease(ctx, key, "test-holder"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{204, 404} {
		if code := call(t, srv, "POST", cleanup, adminTok, "application/json", abandon, nil); code != want {
			t.Fatalf("abandon: %d, want %d", code, want)
		}
	}
	events, err := app.Store.ListEvents(ctx, storage.EventFilter{GraphID: "invalid", Types: []domain.EventType{"environment.deferred_cleanup_abandon_requested"}})
	if err != nil || len(events) != 1 || events[0].Actor != "admin" || !strings.Contains(string(events[0].Data), `"invalid":true`) ||
		!strings.Contains(string(events[0].Data), "verified resources") || strings.Contains(string(events[0].Data), "broken-private-bytes") {
		t.Fatalf("audit: %+v, %v", events, err)
	}
}
