package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestTriggerEnvironmentLaunchLifecycle(t *testing.T) {
	for _, scenario := range []string{"reuse-ask", "repository-reuse", "new-build", "preparation-failure", "build-failure", "reject", "expired", "stale-approval", "start-failure"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			prepareRepository := func(id string) {
				t.Helper()
				dir := filepath.Join(root, id)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n"), 0644); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"init", "-q"}, {"add", "go.mod"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture"}} {
					cmd := exec.Command("git", args...)
					cmd.Dir = dir
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatal(err, string(out))
					}
				}
			}
			bin, dockerLog := fakeDaemonCLI(t)
			gate := bin + ".continue"
			if scenario == "new-build" {
				script, err := os.ReadFile(bin)
				if err != nil {
					t.Fatal(err)
				}
				script = []byte(strings.Replace(string(script), "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1\" = buildx ] && [ \"$2\" = build ]; then n=0; while [ ! -f '"+gate+"' ] && [ \"$n\" -lt 250 ]; do sleep 0.02; n=$((n+1)); done; fi\n", 1))
				if err := os.WriteFile(bin, script, 0755); err != nil {
					t.Fatal(err)
				}
			}

			if scenario == "build-failure" {
				script, err := os.ReadFile(bin)
				if err != nil {
					t.Fatal(err)
				}
				script = []byte(strings.Replace(string(script), "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1\" = buildx ] && [ \"$2\" = build ]; then cat >/dev/null; echo 'token=fixture-secret /private/workspace' >&2; exit 1; fi\n", 1))
				if err := os.WriteFile(bin, script, 0755); err != nil {
					t.Fatal(err)
				}
			}
			srv, a := setupTriggerApp(t, func(c *config.Config) {
				c.Workspaces.Root = root
				c.Isolation.Docker.Bin = bin
				c.Server.PublicURL = "https://weft.example"
			})
			var log bytes.Buffer
			a.Triggers.Log = slog.New(slog.NewJSONHandler(&log, nil))
			model := &readinessModel{verdict: `{"missing":[]}`}
			a.Models.Add(llm.ProviderConfig{Name: model.Name(), Type: "mock", DefaultModel: "fixture"}, model)
			var g domain.Graph
			if err := json.Unmarshal([]byte(taskGraph), &g); err != nil {
				t.Fatal(err)
			}
			g.Spec.Environment = &domain.EnvironmentSpec{Image: "alpine:3.22", Caches: []string{"/home/sandbox/.cache/pip"}}
			g.Spec.Nodes[0].Isolation.Mode = domain.IsolationDocker
			g.Spec.Nodes[0].Model = domain.ModelRef{Provider: model.Name(), Name: "fixture"}
			if scenario == "repository-reuse" {
				g.Spec.Workspace.Repo = "https://fixture.invalid/acme/app.git"
				prepareRepository("task-fixture")
			}
			if scenario == "preparation-failure" || scenario == "start-failure" {
				g.Spec.Nodes[0].Loop.Mode = domain.LoopContinuous
			}
			if scenario == "start-failure" {
				g.Spec.Environment = nil
				g.Spec.Nodes[0].Isolation.Mode = domain.IsolationProcess
			}
			raw, _ := json.Marshal(g)
			if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(raw), nil); code != 201 {
				t.Fatal(code)
			}
			var source environment.View
			if scenario == "reuse-ask" || scenario == "repository-reuse" {
				if code := call(t, srv, "POST", "/api/v1/graphs/task-fixture/environment/build", adminTok, "application/json", `{}`, nil); code != 202 {
					t.Fatal(code)
				}
				waitFor(t, 5*time.Second, "source ready", func() bool {
					call(t, srv, "GET", "/api/v1/graphs/task-fixture/environment", viewerTok, "", "", &source)
					return source.UpToDate
				})
			}
			searches := 0
			a.Tools.MustRegister(fixtureTool{name: "mcp.gh.search_issues", calls: &searches, content: `{"items":[` + apiFixtureIssue + `]}`})
			var labels, comments []string
			a.TrackerAPI.Client = &http.Client{Transport: trackerTransport(func(req *http.Request) (*http.Response, error) {
				body := "{}"
				if req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/issues/7") {
					body = apiFixtureIssue
				}
				if req.Method == "POST" {
					b, err := io.ReadAll(req.Body)
					if err != nil {
						return nil, err
					}
					if strings.HasSuffix(req.URL.Path, "/comments") {
						comments = append(comments, string(b))
					} else {
						labels = append(labels, string(b))
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			body := triggerBody
			ask := scenario == "reuse-ask" || scenario == "repository-reuse" || scenario == "reject" || scenario == "expired" || scenario == "stale-approval"
			if !ask {
				body = strings.Replace(body, `"ask"`, `"full"`, 1)
			}
			var tr trigger.Trigger
			if code := call(t, srv, "POST", "/api/v1/triggers", adminTok, "application/json", body, &tr); code != 201 {
				t.Fatal(code)
			}
			tick := func() {
				t.Helper()
				if _, err := a.Triggers.Triggers.Update(t.Context(), tr.ID, "fixture", func(tr *trigger.Trigger) error { tr.NextRunAt = nil; return nil }); err != nil {
					t.Fatal(err)
				}
				a.Triggers.Tick(t.Context())
				var err error
				current, getErr := a.Triggers.Triggers.Get(t.Context(), tr.ID)
				err = getErr
				if current != nil {
					tr = *current
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			tick()
			page, err := (taskconfig.Manager{Store: a.Store}).List(t.Context(), "", 10)
			if err != nil || len(page.Items) != 1 || model.observed() == nil {
				t.Fatalf("admission: %+v %v", page, err)
			}
			d := page.Items[0]
			if scenario == "repository-reuse" {
				prepareRepository(taskrun.ExecutionID(d.ID))
			}
			if d.ResultNodeID != d.NodeID || len(labels) == 0 || !strings.Contains(labels[0], "weft:wip") {
				t.Fatalf("claim/default: %+v %v", d, labels)
			}
			if ask {
				approval, err := a.Store.GetApproval(t.Context(), d.EnvironmentApprovalID)
				if err != nil || approval.Kind != domain.ApprovalTaskEnvironment || approval.Status != domain.ApprovalPending {
					t.Fatalf("approval: %+v %v", approval, err)
				}
				tick()
				if tr.State != trigger.Active || len(comments) != 1 || len(labels) != 1 {
					t.Fatalf("pending approval changed issue: %+v %v %v", tr, comments, labels)
				}
				if _, err := a.Store.GetGraph(t.Context(), taskrun.ExecutionID(d.ID), 0); err != storage.ErrNotFound {
					t.Fatal("prepared before approval", err)
				}
				if code := prepareTaskEnvironment(t, srv.URL, *d, adminTok); code == 202 {
					t.Fatal("direct prepare bypassed pending approval")
				}
				if code := call(t, srv, "POST", "/api/v1/approvals/"+approval.ID+"/approve", operatorTok, "application/json", `{}`, nil); code != 403 {
					t.Fatal("operator approved environment", code)
				}
				decision := "approve"
				if scenario == "reject" {
					decision = "reject"
				}
				if scenario == "stale-approval" {
					in := d.Input
					in.Request = "Edited task"
					if _, err := (taskconfig.Manager{Store: a.Store}).Save(t.Context(), d.ID, in, d.Revision, "admin"); err != nil {
						t.Fatal(err)
					}
					if code := call(t, srv, "POST", "/api/v1/approvals/"+approval.ID+"/approve", adminTok, "application/json", `{}`, nil); code != 400 {
						t.Fatal("stale approval accepted", code)
					}
					return
				}
				if scenario == "expired" {
					past := time.Now().Add(-time.Second)
					approval.ExpiresAt = &past
					if err := a.Store.PutApproval(t.Context(), approval); err != nil {
						t.Fatal(err)
					}
				} else if code := call(t, srv, "POST", "/api/v1/approvals/"+approval.ID+"/"+decision, adminTok, "application/json", `{}`, nil); code != 200 {
					t.Fatal(code)
				}
				tick()
			}
			if scenario == "preparation-failure" || scenario == "start-failure" || scenario == "reject" || scenario == "expired" || scenario == "build-failure" {
				if scenario == "build-failure" {
					waitFor(t, 5*time.Second, "build failure", func() bool {
						v, err := a.API.TaskRunner().View(t.Context(), d.ID)
						return err == nil && v.Environment != nil && v.Environment.State == environment.StateFailed
					})
					tick()
				}
				d, err = (taskconfig.Manager{Store: a.Store}).Get(t.Context(), d.ID)
				if err != nil || tr.State != trigger.Paused || d.LaunchError == "" || !strings.Contains(tr.PausedReason, d.LaunchError) {
					t.Fatalf("failure: %+v %+v %v", tr, d, err)
				}
				comment := comments[len(comments)-1]
				if !strings.Contains(comment, d.LaunchError) || !strings.Contains(comment, "https://weft.example/#/tasks/"+d.ID) || !strings.Contains(labels[len(labels)-1], "weft:needs-attention") {
					t.Fatalf("writeback: %v %v", comments, labels)
				}
				tick()
				if strings.Count(log.String(), "trigger task launch failed") != 1 || strings.Contains(log.String(), "fixture-secret") || strings.Contains(comment, "/private") || strings.Contains(d.LaunchError, "fixture-secret") {
					t.Fatalf("unsafe/repeated diagnostic: %s %s", log.String(), comment)
				}
				if scenario == "start-failure" {
					path := "/api/v1/triggers/" + tr.ID + "/retry-failed"
					body := `{"key":"acme/app#7","taskId":"` + d.ID + `","confirm":true}`
					for _, tok := range []string{viewerTok, operatorTok} {
						if code := call(t, srv, "POST", path, tok, "application/json", body, nil); code != 403 {
							t.Fatal("retry authority", code)
						}
					}
					if code := call(t, srv, "POST", path, adminTok, "application/json", strings.Replace(body, `true`, `false`, 1), nil); code != 400 {
						t.Fatal("retry confirmation", code)
					}
					if code := call(t, srv, "POST", path, adminTok, "application/json", body, &tr); code != 200 || tr.State != trigger.Paused {
						t.Fatal("retry", code, tr)
					}
					retained, err := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), d.ID)
					if err != nil || retained.Status != "cancelled" || retained.LaunchError != d.LaunchError {
						t.Fatal("retained draft", retained, err)
					}
					if code := taskStart(t, srv.URL, d.ID, adminTok, retained.Revision); code != 202 {
						t.Fatal(code)
					}
					if executions, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: taskrun.ExecutionID(d.ID)}); err != nil || len(executions) != 0 {
						t.Fatal("retired draft dispatched", executions, err)
					}
				}
				return
			}
			if scenario == "new-build" {
				pending, err := a.API.TaskRunner().View(t.Context(), d.ID)
				if err != nil || pending.Environment == nil || pending.Environment.State != environment.StateBuilding {
					t.Fatalf("poll waited for build: %+v %v", pending, err)
				}
				tick()
				if tr.State != trigger.Active || len(labels) != 1 {
					t.Fatal("building changed issue", tr, labels)
				}
				if err := os.WriteFile(gate, []byte("continue"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var v *taskrun.View
			waitFor(t, 5*time.Second, "task environment ready", func() bool {
				v, err = a.API.TaskRunner().View(t.Context(), d.ID)
				return err == nil && v.Environment != nil && v.Environment.UpToDate
			})
			if v.Task.Status != "environment_pending" || len(v.Executions) != 0 {
				t.Fatalf("build dispatched task: %+v", v)
			}
			if scenario == "reuse-ask" {
				data, err := os.ReadFile(dockerLog)
				if err != nil {
					t.Fatal(err)
				}
				if !v.Environment.Reused || v.Environment.Hash != source.Hash || v.Environment.Image == source.Image || v.Environment.Caches[0].Volume == source.Caches[0].Volume || strings.Count(string(data), "buildx build ") != 1 {
					t.Fatalf("reuse: %+v %+v %s", v.Environment, source, data)
				}
			}
			if scenario == "repository-reuse" {
				data, err := os.ReadFile(dockerLog)
				if err != nil {
					t.Fatal(err)
				}
				if !v.Environment.Reused || v.Environment.Image != source.Image || v.Environment.ReuseKey != source.ReuseKey || v.Environment.Caches[0].Volume == source.Caches[0].Volume || strings.Count(string(data), "buildx build ") != 1 {
					t.Fatalf("repository task reuse: %+v %+v %s", source, v.Environment, data)
				}
			}
			tick()
			d, err = (taskconfig.Manager{Store: a.Store}).Get(t.Context(), d.ID)
			if err != nil || d.Status != "running" || tr.State != trigger.Active || len(labels) != 1 || len(comments) != 1 {
				t.Fatalf("start: %+v %+v %v %v %v", d, tr, labels, comments, err)
			}
			messages, err := a.Store.ListOperatorMessages(t.Context(), d.ExecutionGraphID, d.NodeID, 10)
			if err != nil || len(messages) != 1 {
				t.Fatalf("input: %v %v", messages, err)
			}
			waitFor(t, 5*time.Second, "trigger task execution", func() bool {
				view, err := a.API.TaskRunner().View(t.Context(), d.ID)
				return err == nil && view.State == "succeeded" && len(view.Executions) == 1
			})
		})
	}
}
