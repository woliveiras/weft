package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/eventtest"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func prepareTaskEnvironment(t *testing.T, url string, d taskconfig.Draft, token string) int {
	t.Helper()
	req, err := http.NewRequest("POST", url+"/api/v1/tasks/"+d.ID+"/prepare-environment", strings.NewReader(`{"confirm":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", fmt.Sprint(d.Revision))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}
func TestTaskEnvironmentBuildRequiresSeparateStartAndRetainsSource(t *testing.T) {
	bin, _ := fakeDaemonCLI(t)
	srv, _ := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	var g domain.Graph
	if err := json.Unmarshal([]byte(taskGraph), &g); err != nil {
		t.Fatal(err)
	}
	g.Spec.Environment = &domain.EnvironmentSpec{Image: "alpine:3.22"}
	g.Spec.Nodes[0].Isolation.Mode = domain.IsolationDocker
	b, _ := json.Marshal(g)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(b), nil); c != 201 {
		t.Fatal(c)
	}
	var input taskconfig.Input
	if err := json.Unmarshal([]byte(taskBody), &input); err != nil {
		t.Fatal(err)
	}
	input.ResultNodeID = input.NodeID
	input.ExecutionMode = "sandbox"
	b, _ = json.Marshal(input)
	var d taskconfig.Draft
	if c := call(t, srv, "POST", "/api/v1/tasks", adminTok, "application/json", string(b), &d); c != 201 {
		t.Fatal(c)
	}
	var review taskrun.Review
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", viewerTok, "", "", &review); c != 200 || review.CanStart || !review.CanPrepareEnvironment {
		t.Fatal(c, review)
	}
	if c := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); c == 202 {
		t.Fatal("started before build")
	}
	for _, tok := range []string{viewerTok, operatorTok} {
		if c := prepareTaskEnvironment(t, srv.URL, d, tok); c != 403 {
			t.Fatal(c)
		}
	}
	if c := prepareTaskEnvironment(t, srv.URL, d, adminTok); c != 202 {
		t.Fatal(c)
	}
	var progress taskrun.View
	waitFor(t, 5*time.Second, "task environment ready", func() bool {
		call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress)
		return progress.Environment != nil && progress.Environment.UpToDate
	})
	var executions []domain.Execution
	if c := call(t, srv, "GET", "/api/v1/graphs/"+progress.Task.ExecutionGraphID+"/executions", viewerTok, "", "", &executions); c != 200 || len(executions) != 0 {
		t.Fatal("build executed agents", c, executions)
	}
	if progress.Task.Status != "environment_pending" || len(progress.Executions) != 0 {
		t.Fatal(progress)
	}
	if c := taskStart(t, srv.URL, d.ID, adminTok, d.Revision); c != 409 {
		t.Fatal("stale start", c)
	}
	var consent taskauth.Review
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/authorization", viewerTok, "", "", &consent); c != 200 || consent.Record != nil {
		t.Fatal("stale start created consent", c, consent)
	}
	if c := call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/review", viewerTok, "", "", &review); c != 200 || !review.CanStart {
		t.Fatal(c, review)
	}
	if c := taskStart(t, srv.URL, d.ID, adminTok, review.Task.Revision); c != 202 {
		t.Fatal(c)
	}
	waitFor(t, 5*time.Second, "prepared task result", func() bool {
		call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress)
		return progress.State == "succeeded" && len(progress.Executions) == 1
	})
	if c := taskStart(t, srv.URL, d.ID, adminTok, review.Task.Revision); c != 202 {
		t.Fatal("idempotent completed start", c)
	}
	var source envView
	if c := call(t, srv, "GET", "/api/v1/graphs/task-fixture/environment", viewerTok, "", "", &source); c != 200 || source.State != "absent" {
		t.Fatal("source environment changed", c, source)
	}
}

func TestTaskEnvironmentFailedBuildCanRetryAndCancelWithoutDispatch(t *testing.T) {
	bin, _ := fakeDaemonCLI(t)
	marker := filepath.Join(filepath.Dir(bin), "fail")
	script, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1\" = buildx ] && [ \"$2\" = build ] && [ -f \""+marker+"\" ]; then cat >/dev/null; exit 1; fi\n", 1))
	if err = os.WriteFile(bin, script, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(marker, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.Bin = bin })
	var g domain.Graph
	if err = json.Unmarshal([]byte(taskGraph), &g); err != nil {
		t.Fatal(err)
	}
	g.Spec.Environment = &domain.EnvironmentSpec{Image: "alpine:3.22"}
	g.Spec.Nodes[0].Isolation.Mode = domain.IsolationDocker
	b, _ := json.Marshal(g)
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(b), nil); code != 201 {
		t.Fatal(code)
	}
	var input taskconfig.Input
	if err = json.Unmarshal([]byte(taskBody), &input); err != nil {
		t.Fatal(err)
	}
	input.ResultNodeID = input.NodeID
	b, _ = json.Marshal(input)
	var d taskconfig.Draft
	call(t, srv, "POST", "/api/v1/tasks", adminTok, "application/json", string(b), &d)
	if code := prepareTaskEnvironment(t, srv.URL, d, adminTok); code != 202 {
		t.Fatal(code)
	}
	var progress taskrun.View
	waitFor(t, 5*time.Second, "failed task build", func() bool {
		call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress)
		return progress.Environment != nil && string(progress.Environment.State) == "failed"
	})
	if code := taskStart(t, srv.URL, d.ID, adminTok, progress.Task.Revision); code == 202 {
		t.Fatal("failed environment started")
	}
	if err = os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if code := prepareTaskEnvironment(t, srv.URL, *progress.Task, adminTok); code != 202 {
		t.Fatal(code)
	}
	waitFor(t, 5*time.Second, "repaired task build", func() bool {
		call(t, srv, "GET", "/api/v1/tasks/"+d.ID+"/progress", viewerTok, "", "", &progress)
		return progress.Environment != nil && progress.Environment.UpToDate
	})
	if code := call(t, srv, "POST", "/api/v1/tasks/"+d.ID+"/cancel", adminTok, "application/json", `{"confirm":true}`, nil); code != 202 {
		t.Fatal(code)
	}
	var environment envView
	if code := call(t, srv, "GET", "/api/v1/graphs/"+progress.Task.ExecutionGraphID+"/environment", viewerTok, "", "", &environment); code != 200 || !environment.UpToDate {
		t.Fatal("cancel reset environment", code, environment)
	}
	var executions []domain.Execution
	if code := call(t, srv, "GET", "/api/v1/graphs/"+progress.Task.ExecutionGraphID+"/executions", viewerTok, "", "", &executions); code != 200 || len(executions) != 0 {
		t.Fatal(code, executions)
	}
	eventtest.Capture(t, app.Store, domain.EvEnvFailed, domain.EvEnvReady)
}
