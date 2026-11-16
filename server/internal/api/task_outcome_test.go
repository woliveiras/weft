package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/eventtest"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type outcomeModel struct{ signal string }

func (outcomeModel) Name() string                                    { return "outcome" }
func (outcomeModel) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p outcomeModel) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if len(r.Tools) == 0 {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"missing":[]}`}}, nil
	}
	args, _ := json.Marshal(map[string]string{"signal": p.signal, "summary": "Dependency registry unavailable. token=fixture-sensitive-value " + strings.Repeat("detail ", 100)})
	calls := []llm.ToolCall{{ID: "finish", Name: "finish", Arguments: args}}
	if p.signal == "blocked" {
		calls = append([]llm.ToolCall{{ID: "emit", Name: "emit", Arguments: json.RawMessage(`{"type":"work.done","payload":{"partial":true}}`)}}, calls...)
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: calls}}, nil
}

func TestTriggerUndeliveredTaskSettlesAndCanBeExplicitlyRetried(t *testing.T) {
	for _, signal := range []string{"blocked", "no_further_work"} {
		t.Run(signal, func(t *testing.T) {
			srv, a := setupTriggerApp(t, func(c *config.Config) { c.Server.PublicURL = "https://weft.example" })
			a.Models.Add(llm.ProviderConfig{Name: "outcome", Type: "mock", DefaultModel: "fixture"}, outcomeModel{signal})
			graph := strings.Replace(taskGraph, `"kind":"echo"`, `"kind":"native"`, 1)
			graph = strings.Replace(graph, `"tools":[]`, `"prompt":"Complete the issue or declare the blocker.","model":{"provider":"outcome","name":"fixture"},"tools":[]`, 1)
			if c, _, b := rawCall(t, "POST", srv.URL+"/api/v1/graphs", adminTok, graph, nil); c != 201 {
				t.Fatal(c, b)
			}
			searches := 0
			var comments, labels []string
			a.Tools.MustRegister(fixtureTool{name: "mcp.gh.search_issues", calls: &searches, content: `{"items":[` + apiFixtureIssue + `]}`})
			event := "[]"
			a.TrackerAPI.Client = &http.Client{Transport: trackerTransport(func(req *http.Request) (*http.Response, error) {
				body := "{}"
				switch {
				case req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/issues/7"):
					body = apiFixtureIssue
				case req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/events"):
					body = event
				case req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/permission"):
					body = `{"role_name":"maintain"}`
				case req.Method == "POST":
					data, err := io.ReadAll(req.Body)
					if err != nil {
						return nil, err
					}
					if strings.HasSuffix(req.URL.Path, "/comments") {
						comments = append(comments, string(data))
					} else {
						labels = append(labels, string(data))
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var tr trigger.Trigger
			body := strings.Replace(triggerBody, `"ask"`, `"full"`, 1)
			if c := call(t, srv, "POST", "/api/v1/triggers", adminTok, "application/json", body, &tr); c != 201 {
				t.Fatal(c)
			}
			tick := func() {
				t.Helper()
				_, err := a.Triggers.Triggers.Update(t.Context(), tr.ID, "fixture", func(t *trigger.Trigger) error { t.NextRunAt = nil; return nil })
				if err != nil {
					t.Fatal(err)
				}
				a.Triggers.Tick(t.Context())
				got, err := a.Triggers.Triggers.Get(t.Context(), tr.ID)
				if err != nil {
					t.Fatal(err)
				}
				tr = *got
			}
			tick()
			id := tr.CurrentTask
			if id == "" {
				t.Fatalf("not started: %+v", tr)
			}
			gid := taskrun.ExecutionID(id)
			var v taskrun.View
			waitFor(t, 10*time.Second, "blocked task and stopped execution copy", func() bool {
				call(t, srv, "GET", "/api/v1/tasks/"+id+"/progress", viewerTok, "", "", &v)
				rec, err := a.Store.GetGraphRecord(t.Context(), gid)
				return err == nil && v.State == "blocked" && v.Task.Status == "blocked" && rec.State == domain.GraphStopped && rec.DesiredState == domain.GraphStopped
			})
			if v.Result == nil || !strings.HasPrefix(v.Result.Summary, "Dependency registry unavailable.") || strings.Contains(v.Result.Summary, "fixture-sensitive") || len([]rune(v.Result.Summary)) > 201 {
				t.Fatalf("unsafe summary: %+v", v.Result)
			}
			if signal == "blocked" && (v.Result.Status != domain.ExecFailed || len(v.Result.OutputIDs) != 0) {
				t.Fatal(v.Result.Status)
			}
			if signal == "blocked" {
				eventtest.Capture(t, a.Store, "execution.failed")
			}
			tick()
			if tr.State != trigger.Paused || tr.CurrentTask != "" || tr.LastFailedTask != id {
				t.Fatalf("trigger outcome: %+v", tr)
			}
			for _, s := range []string{comments[len(comments)-1], tr.PausedReason} {
				if !strings.Contains(s, "Dependency registry unavailable.") || !strings.Contains(s, "https://weft.example/#/tasks/"+id) || strings.Contains(s, "fixture-sensitive") {
					t.Fatal(s)
				}
			}
			if !strings.Contains(labels[len(labels)-1], "weft:needs-attention") {
				t.Fatal(labels)
			}
			// Graph mutation APIs already fence all task execution copies.
			for _, action := range []string{"start", "stop", "pause", "resume", "kill"} {
				if c := call(t, srv, "POST", "/api/v1/graphs/"+gid+"/"+action, adminTok, "application/json", `{}`, nil); c != 409 {
					t.Fatal(action, c)
				}
			}
			if c := call(t, srv, "DELETE", "/api/v1/graphs/"+gid, adminTok, "", "", nil); c != 409 {
				t.Fatal(c)
			}
			oldTask, _ := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), id)
			oldGraph, _ := a.Store.GetGraph(t.Context(), gid, 0)
			oldExec, _ := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: gid})
			path := "/api/v1/triggers/" + tr.ID + "/retry-failed"
			retryBody := `{"key":"acme/app#7","taskId":"` + id + `","confirm":true}`
			for _, token := range []string{viewerTok, operatorTok} {
				if c := call(t, srv, "POST", path, token, "application/json", retryBody, nil); c != 403 {
					t.Fatal(c)
				}
			}
			if c := call(t, srv, "POST", path, adminTok, "application/json", strings.Replace(retryBody, "true", "false", 1), nil); c != 400 {
				t.Fatal(c)
			}
			if c := call(t, srv, "POST", path, adminTok, "application/json", retryBody, &tr); c != 200 {
				t.Fatal(c)
			}
			if c := call(t, srv, "POST", path, adminTok, "application/json", retryBody, nil); c != 409 {
				t.Fatal(c)
			}
			if c := call(t, srv, "POST", "/api/v1/triggers/"+tr.ID+"/resume", adminTok, "", "", &tr); c != 200 {
				t.Fatal(c)
			}
			tick()
			if tr.CurrentTask != "" {
				t.Fatal("retry accepted without a fresh maintainer label")
			}
			event = `[{"id":1,"event":"labeled","label":{"name":"weft:todo"},"actor":{"login":"maintainer","type":"User"},"created_at":"` + time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano) + `"}]`
			tick()
			if tr.CurrentTask == "" || tr.CurrentTask == id {
				t.Fatalf("new admission missing: %+v", tr)
			}
			retained, _ := (taskconfig.Manager{Store: a.Store}).Get(t.Context(), id)
			graphAfter, _ := a.Store.GetGraph(t.Context(), gid, 0)
			execAfter, _ := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: gid})
			if !reflect.DeepEqual(oldTask, retained) || !reflect.DeepEqual(oldGraph, graphAfter) || !reflect.DeepEqual(oldExec, execAfter) {
				t.Fatal("retry mutated historical evidence")
			}
		})
	}
}
