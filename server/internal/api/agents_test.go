package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

type catalogModel struct{ prompts chan string }

func (catalogModel) Name() string                                    { return "catalog-fixture" }
func (catalogModel) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (m catalogModel) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	for _, msg := range r.Messages {
		if strings.HasPrefix(msg.Content, "Review repo ") {
			select {
			case m.prompts <- msg.Content:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "done", Name: "finish", Arguments: json.RawMessage(`{"summary":"reviewed"}`)}}}}, nil
}

func TestReusableAgentVersionAndWorkflowRuntime(t *testing.T) {
	srv, app := setupApp(t, nil)
	post := func(path, token, body string, want int, out any) {
		t.Helper()
		if got := call(t, srv, "POST", "/api/v1/"+path, token, "application/json", body, out); got != want {
			t.Fatalf("POST %s: got %d, want %d", path, got, want)
		}
	}
	fixture := catalogModel{prompts: make(chan string, 4)}
	app.Models.Add(llm.ProviderConfig{Name: "catalog-fixture", Type: "mock", DefaultModel: "fixture"}, fixture)
	definition := `{"id":"reviewer","definition":{"name":"Reviewer","runtime":{"kind":"native"},"model":{"provider":"catalog-fixture"},"prompt":"Review repo @repository.","parameters":[{"name":"repository","required":true}]}}`
	post("agents", viewerTok, definition, http.StatusForbidden, nil)
	post("agents", operatorTok, definition, http.StatusForbidden, nil)
	post("agents", adminTok, definition, http.StatusCreated, nil)
	post("agents", adminTok, definition, http.StatusConflict, nil)

	graphBody := `{"metadata":{"id":"code-review-fixture","name":"code-review"},"spec":{"defaults":{"tools":["shell.exec"]},"parameters":[{"name":"team","value":"platform"}],"nodes":[{"id":"reviewer","localInstructions":"Check @repository for @team.","agent":{"id":"reviewer","version":1,"parameters":{"repository":"alpha"}},"tools":["*"],"loop":{"mode":"event"}},{"id":"alternate","localInstructions":"Check only tests.","agent":{"id":"reviewer","version":1,"parameters":{"repository":"beta"}}}],"channels":[{"id":"requests","to":["reviewer"]},{"id":"alternate","to":["alternate"]}]}}`
	post("graphs", adminTok, strings.ReplaceAll(graphBody, `"version":1`, `"version":7`), http.StatusUnprocessableEntity, nil)
	post("graphs", adminTok, strings.Replace(graphBody, `"repository":"alpha"`, `"typo":"alpha"`, 1), http.StatusUnprocessableEntity, nil)
	post("graphs", adminTok, graphBody, http.StatusCreated, nil)
	published := strings.Replace(definition, "Review repo @repository.", "Updated @repository.", 1)
	if got := call(t, srv, "PUT", "/api/v1/agents/reviewer", adminTok, "application/json", published, nil); got != 400 {
		t.Fatalf("publish without If-Match: %d", got)
	}
	if got := callIfMatch(t, srv, "PUT", "/api/v1/agents/reviewer", adminTok, published, 1, nil); got != 200 {
		t.Fatalf("publish v2: %d", got)
	}
	if got := callIfMatch(t, srv, "PUT", "/api/v1/agents/reviewer", adminTok, published, 1, nil); got != 409 {
		t.Fatalf("publish with stale If-Match: %d", got)
	}
	var agent agentcatalog.Agent
	if got := call(t, srv, "GET", "/api/v1/agents/reviewer", viewerTok, "", "", &agent); got != 200 || len(agent.Versions) != 2 || agent.Versions[0].Definition.Prompt != "Review repo @repository." {
		t.Fatalf("catalog: %d %+v", got, agent)
	}
	saved, err := app.Store.GetGraph(t.Context(), "code-review-fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range saved.Spec.Nodes {
		if n.Agent.Version != 1 || len(n.Tools) != 0 || n.Prompt != "Review repo @repository." {
			t.Fatalf("saved agent: %+v", n)
		}
	}
	// Task preparation owns its own reviewed execution snapshot, including
	// source context appended after the workflow definition was resolved.
	taskCopy := *saved
	taskCopy.Metadata.ID = domain.TaskRunGraphPrefix + "catalog-fixture"
	taskCopy.Metadata.Labels = map[string]string{domain.LabelTask: "catalog-fixture"}
	reviewer := taskCopy.Node("reviewer")
	reviewer.Prompt += " Scoped source context."
	taskDoc, _, err := app.Engine.SaveTaskGraph(t.Context(), &taskCopy, "task-preparation")
	if err != nil {
		t.Fatal(err)
	}
	if n := taskDoc.Node("reviewer"); n.Agent.Version != 1 || n.Prompt != "Review repo @repository. Scoped source context." {
		t.Fatal("task preparation lost the pinned version or source context")
	}
	var catalog []agentcatalog.Summary
	if got := call(t, srv, "GET", "/api/v1/agents", viewerTok, "", "", &catalog); got != 200 || len(catalog) != 1 || catalog[0].Latest != 2 {
		t.Fatalf("catalog inventory: %d %+v", got, catalog)
	}
	post("graphs/code-review-fixture/start", operatorTok, "{}", 200, nil)
	for _, channel := range []string{"requests", "alternate"} {
		post("graphs/code-review-fixture/channels/"+channel+"/messages", operatorTok, `{"type":"review.requested","payload":{}}`, 201, nil)
	}
	want := map[string]bool{"Review repo alpha.\n\nWorkflow instructions:\nCheck alpha for platform.": true, "Review repo beta.\n\nWorkflow instructions:\nCheck only tests.": true}
	timeout := time.After(8 * time.Second)
	for len(want) > 0 {
		select {
		case got := <-fixture.prompts:
			if !want[got] {
				t.Fatalf("unexpected prompt %q", got)
			}
			delete(want, got)
		case <-timeout:
			t.Fatalf("missing prompts: %v", want)
		}
	}
}

func TestAgentSourceReadsSavedWorkflowDefaults(t *testing.T) {
	srv := setup(t)
	body := `{"metadata":{"id":"legacy-review"},"spec":{"defaults":{"runtime":{"kind":"native"},"tools":["filesystem.read"]},"parameters":[{"name":"repository","value":"woliveiras/weft"},{"name":"other","value":"unused"}],"nodes":[{"id":"reviewer","prompt":"Review @repository","workspace":{"readOnly":true}}]}}`
	if got := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", body, nil); got != 201 {
		t.Fatalf("create inline workflow: %d", got)
	}
	var source domain.AgentDefinition
	if got := call(t, srv, "GET", "/api/v1/agents/source?workflow=legacy-review&node=reviewer&version=1", viewerTok, "", "", &source); got != 200 {
		t.Fatalf("load source: %d", got)
	}
	if source.Runtime.Kind != domain.RuntimeNative || !source.ReadOnly || len(source.Tools) != 1 || len(source.Parameters) != 1 || source.Parameters[0].Default != "woliveiras/weft" {
		t.Fatalf("promoted definition: %+v", source)
	}
	if got := call(t, srv, "GET", "/api/v1/agents/source?workflow=legacy-review&node=reviewer&version=7", viewerTok, "", "", nil); got != 404 {
		t.Fatalf("missing source version: %d", got)
	}
}

func TestWorkflowAgentsAppearAutomatically(t *testing.T) {
	srv, _ := setupApp(t, nil)
	graph := `{"metadata":{"id":"source-workflow"},"spec":{"parameters":[{"name":"repository","value":"original/project"}],"nodes":[{"id":"writer","description":"Writes summaries.","runtime":{"kind":"echo"},"prompt":"Summarize @repository","localInstructions":"Notes for the source only."}]}}`
	if got := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); got != 201 {
		t.Fatalf("create source workflow: %d", got)
	}
	var items []agentcatalog.Summary
	if got := call(t, srv, "GET", "/api/v1/agents", viewerTok, "", "", &items); got != 200 || len(items) != 1 {
		t.Fatalf("automatic catalog: %d %+v", got, items)
	}
	item := items[0]
	if item.Origin == nil || item.Origin.Workflow != "source-workflow" || item.Origin.Node != "writer" || item.Description != "Writes summaries." {
		t.Fatalf("unexpected summary: %+v", item)
	}
	var entry agentcatalog.Agent
	if got := call(t, srv, "GET", "/api/v1/agents/"+item.ID, viewerTok, "", "", &entry); got != 200 || len(entry.Versions) != 1 {
		t.Fatalf("get automatic catalog entry: %d %+v", got, entry)
	}
	def := entry.Versions[0].Definition
	if def.Prompt != "Summarize @repository" || len(def.Parameters) != 1 || !def.Parameters[0].Required || def.Parameters[0].Default != "" {
		t.Fatalf("local values leaked into the definition: %+v", def)
	}
	target := `{"metadata":{"id":"target-workflow"},"spec":{"nodes":[{"id":"summarizer","agent":{"id":"AGENT","version":1,"parameters":{"repository":"another/project"}},"localInstructions":"For the target only."}]}}`
	target = strings.Replace(target, "AGENT", item.ID, 1)
	if got := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", target, nil); got != 201 {
		t.Fatalf("reuse newly registered agent: %d", got)
	}
	var created struct{ Graph domain.Graph }
	if got := call(t, srv, "GET", "/api/v1/graphs/target-workflow", viewerTok, "", "", &created); got != 200 {
		t.Fatalf("read target workflow: %d", got)
	}
	n := created.Graph.Node("summarizer")
	if n == nil || n.Agent.ID != item.ID || n.Prompt != def.Prompt || n.LocalInstructions != "For the target only." {
		t.Fatalf("target node: %+v", n)
	}
}
