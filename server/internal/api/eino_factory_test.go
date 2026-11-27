package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"gopkg.in/yaml.v3"
)

// An optimistic reviewer exposes whether objective findings and the real test
// exit status still control routing when the model approves every revision.
type factoryModel struct {
	catalogModel
	changes atomic.Int32
}

func (*factoryModel) Name() string { return "factory-fixture" }

func (m *factoryModel) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	var text string
	switch r.Metadata["node"] {
	case "planner":
		text = `{"tasks":[{"id":"one","title":"Verify the build"}],"questions":[{"id":"q","question":"Which package?"}]}`
	case "researcher":
		text = "Check the feature package."
	case "developer":
		script := "package feature\nimport \"testing\"\nfunc TestFeature(t *testing.T) {"
		switch m.changes.Add(1) {
		case 1:
			script += "// TODO implement\n"
		case 2:
			script += "t.Fatal(\"fixture regression\")"
		case 3:
		default:
			return nil, fmt.Errorf("unexpected extra development turn")
		}
		raw, err := json.Marshal(map[string]any{"files": []map[string]string{{"path": "src/feature_test.go", "content": script + "}\n"}}})
		if err != nil {
			return nil, err
		}
		text = string(raw)
	case "reviewer":
		text = `{"verdict":"approved","comments":[]}`
	default:
		return nil, fmt.Errorf("unexpected fixture node %s", r.Metadata["node"])
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: text}, StopReason: "stop"}, nil
}

func TestEinoFactoryReworkThroughController(t *testing.T) {
	srv, controller, binary := setupEinoController(t)
	exerciseEinoFactory(t, srv, controller, "", []string{binary})
}

func exerciseEinoFactory(t *testing.T, srv *httptest.Server, app *app.App, endpoint string, command []string) {
	t.Helper()
	ctx := t.Context()
	model := new(factoryModel)
	app.Models.Add(llm.ProviderConfig{Name: "factory-fixture", DefaultModel: "fixture"}, model)
	raw, err := os.ReadFile("../../../examples/graphs/software-factory.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var g domain.Graph
	if err := yaml.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	g.Spec.Defaults.Runtime.Endpoint = endpoint
	g.Spec.Defaults.Runtime.Command = command
	g.Spec.Defaults.Model.Provider = "factory-fixture"
	g.Node("integrator").Model = domain.ModelRef{Provider: "mock"}
	qa := g.Node("qa")
	qa.Config["test_command"] = "go test ./..."
	for i, grant := range qa.Tools {
		if grant.Name == "shell.exec" {
			qa.Tools[i].Commands = []string{"go test ./..."}
		}
	}
	gid := g.Metadata.ID
	body, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "/api/v1/graphs/" + gid
	post := func(path, payload string, want int, out any) {
		t.Helper()
		if got := call(t, srv, "POST", path, adminTok, "application/json", payload, out); got != want {
			t.Fatalf("%s: got %d, want %d", path, got, want)
		}
	}
	post("/api/v1/graphs", string(body), 201, nil)
	root, err := app.API.Workspaces.Ensure(ctx, &g)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/feature\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	post(prefix+"/start", "", 200, nil)
	post(prefix+"/channels/requests/messages", `{"type":"feature.request","payload":{"id":"case","title":"Check build"}}`, 201, nil)
	var held *domain.Approval
	waitFor(t, 120*time.Second, "release approval after research, review and QA rework", func() bool {
		a, err := app.Store.ListApprovals(ctx, storage.ApprovalFilter{GraphID: gid, Status: domain.ApprovalPending})
		if err != nil {
			t.Fatal(err)
		}
		if len(a) > 0 {
			held = a[0]
			return true
		}
		xs, err := app.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: gid, Status: domain.ExecFailed})
		if err != nil {
			t.Fatal(err)
		}
		if len(xs) > 0 {
			t.Fatalf("%s failed: %s", xs[0].NodeID, xs[0].Error)
		}
		return false
	})
	rows, err := app.Store.ListMessages(ctx, storage.MessageFilter{GraphID: gid, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"research.done": 1, "task.ready": 1, "code.ready": 3, "review.changes_requested": 1, "review.approved": 2, "qa.failed": 1, "qa.passed": 1}
	for _, row := range rows {
		m := row.Message
		if _, ok := want[m.Type]; !ok || row.Channel == "review-audit" || row.Group != "" {
			continue
		}
		want[m.Type]--
		var p struct {
			Request string `json:"request_id"`
			Task    string `json:"task_id"`
			Report  string
			Code    int `json:"exit_code"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.Request != "case" || (m.Type != "research.done" && p.Task != "case-one") {
			t.Fatalf("%s lost its request or task ID", m.Type)
		}
		if m.Type == "qa.failed" && (p.Code != 1 || !strings.Contains(p.Report, "fixture regression")) {
			t.Fatalf("incorrect failure report: %s", m.Payload)
		}
		if m.Type == "qa.passed" && (p.Code != 0 || !strings.Contains(p.Report, "ok")) {
			t.Fatalf("incorrect passing report: %s", m.Payload)
		}
	}
	for kind, left := range want {
		if left != 0 {
			t.Errorf("%s: observed count differs by %d", kind, left)
		}
	}
	filter := storage.ExecutionFilter{GraphID: gid, NodeID: "integrator"}
	xs, err := app.Store.ListExecutions(ctx, filter)
	if err != nil || len(xs) != 0 {
		t.Fatalf("release delivered before approval: %v, %v", xs, err)
	}
	post("/api/v1/approvals/"+held.ID+"/approve", "{}", 200, nil)
	waitFor(t, 10*time.Second, "acknowledged release", func() bool {
		rows, err := app.Store.ListMessages(ctx, storage.MessageFilter{GraphID: gid, Channel: "release", Status: domain.MsgAcked})
		if err != nil {
			t.Fatal(err)
		}
		return len(rows) == 1
	})
}
