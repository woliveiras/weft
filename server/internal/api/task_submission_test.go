package api_test

import (
	"context"
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"testing"
)

type submissionModel struct{ raw string }

func (submissionModel) Name() string                                    { return "submission" }
func (submissionModel) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (model submissionModel) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	if model.raw != "" {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: model.raw}}, nil
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"outcome":"freeform","workflowId":"","repository":"","clarification":false}`}}, nil
}
func TestPromptSubmissionAdmissionReplayAndRepair(t *testing.T) {
	srv, a := setupApp(t, nil)
	graph := `{"metadata":{"id":"task-template"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"},"loop":{"mode":"once"},"tools":[]}]}}`
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", graph, nil); code != 201 {
		t.Fatal(code)
	}
	body := `{"request":"Explain the project","graphId":"task-template","key":"submission-fixture-key","confirm":true}`
	for _, token := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "POST", "/api/v1/tasks/submit", token, "application/json", body, nil); code != 403 {
			t.Fatal(code)
		}
	}
	if code := call(t, srv, "POST", "/api/v1/tasks/submit", adminTok, "application/json", `{"request":"x","key":"submission-fixture-key","confirm":false}`, nil); code != 400 {
		t.Fatal(code)
	}
	var result struct {
		Task   taskconfig.Draft `json:"task"`
		Review *taskrun.Review  `json:"review"`
	}
	if code := call(t, srv, "POST", "/api/v1/tasks/submit", adminTok, "application/json", body, &result); code != 200 || result.Task.Resolution.State != "needs_model" || result.Review.CanStart {
		t.Fatal(code, result)
	}
	id := result.Task.ID
	revision := result.Task.Revision
	if code := call(t, srv, "POST", "/api/v1/tasks/submit", adminTok, "application/json", body, &result); code != 200 || result.Task.ID != id || result.Task.Revision != revision {
		t.Fatal("replayed effects", code, result)
	}
	if err := a.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "submission", Type: "openai", DefaultModel: "fixture"}, Provider: submissionModel{}}}, "submission"); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"request": "Explain the project", "graphId": "task-template", "id": id, "revision": revision, "key": "submission-repair-key", "confirm": true})
	if code := call(t, srv, "POST", "/api/v1/tasks/submit", adminTok, "application/json", string(data), &result); code != 200 || result.Task.ExecutionGraphID == "" {
		t.Fatal(code, result)
	}
	execution := result.Task.ExecutionGraphID
	if code := call(t, srv, "POST", "/api/v1/tasks/submit", adminTok, "application/json", string(data), &result); code != 200 || result.Task.ExecutionGraphID != execution {
		t.Fatal("duplicate launch", code, result)
	}
	graphs, err := a.Store.ListGraphs(t.Context())
	if err != nil || len(graphs) != 2 {
		t.Fatal(graphs, err)
	}
	source, err := a.Store.GetGraph(t.Context(), "task-template", 0)
	if err != nil || source.Metadata.Version != 1 || len(source.Spec.Nodes[0].Tools) != 0 {
		t.Fatal("changed workflow", err)
	}
}
func TestPromptSubmissionStopsForUnresolvedRequirements(t *testing.T) {
	for _, tc := range []struct{ name, raw, state string }{
		{"clarification", `{"outcome":"freeform","clarification":true}`, "needs_input"},
		{"repository", `{"outcome":"freeform","repository":"https://example.com/source.git"}`, "needs_repository"},
		{"invalid", `{"outcome":"freeform","tools":["shell.exec"]}`, "invalid_proposal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, a := setupApp(t, nil)
			if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", `{"metadata":{"id":"submit-blocked"},"spec":{"nodes":[{"id":"reader","runtime":{"kind":"echo"}}]}}`, nil); c != 201 {
				t.Fatal(c)
			}
			if e := a.Models.ReplaceProviders([]llm.Entry{{Config: llm.ProviderConfig{Name: "submission", Type: "openai", DefaultModel: "fixture"}, Provider: submissionModel{raw: tc.raw}}}, "submission"); e != nil {
				t.Fatal(e)
			}
			var result struct {
				Task   taskconfig.Draft `json:"task"`
				Review taskrun.Review   `json:"review"`
			}
			if c := call(t, srv, "POST", "/api/v1/tasks/submit", adminTok, "application/json", `{"request":"Explain https://example.com/source.git","graphId":"submit-blocked","key":"blocked-submission-key","confirm":true}`, &result); c != 200 || result.Task.Status != "draft" || result.Review.CanStart || result.Task.ExecutionGraphID != "" || result.Task.Resolution.State != tc.state {
				t.Fatalf("%d state=%s", c, result.Task.Resolution.State)
			}
			graphs, e := a.Store.ListGraphs(t.Context())
			if e != nil || len(graphs) != 1 {
				t.Fatal("unexpected launch", e)
			}
		})
	}
}
