package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
	"gopkg.in/yaml.v3"
)

type summaryModel struct{ catalogModel }

func (summaryModel) Name() string { return "summary-fixture" }

func (summaryModel) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if req.Model != "summary-model" || !strings.Contains(llm.LastUserText(req.Messages), "green apples") {
		return nil, os.ErrInvalid
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: "The document describes green apples."}, StopReason: "stop", Usage: llm.Usage{InputTokens: 17, OutputTokens: 6}}, nil
}

func TestEinoSummaryThroughController(t *testing.T) {
	ctx := t.Context()
	base := filepath.Join("..", "..", "..")
	binary := filepath.Join(t.TempDir(), "weft-eino-agent")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/weft-eino-agent")
	build.Dir = filepath.Join(base, "agents", "eino")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build external service: %s: %v", out, err)
	}
	cb := httptest.NewUnstartedServer(nil)
	srv, app := setupApp(t, func(cfg *config.Config) {
		cfg.Server.InternalURL = "http://" + cb.Listener.Addr().String()
		cfg.Queue = config.Queue{Type: "fs", Dir: t.TempDir()}
	})
	cb.Config.Handler = app.API.Handler()
	cb.Start()
	t.Cleanup(cb.Close)
	app.Models.Add(llm.ProviderConfig{Name: "summary-fixture", DefaultModel: "summary-model"}, summaryModel{})
	raw, err := os.ReadFile(filepath.Join(base, "examples", "graphs", "eino-summary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var graph domain.Graph
	if err := yaml.Unmarshal(raw, &graph); err != nil {
		t.Fatal(err)
	}
	node := graph.Node("summarize")
	node.Runtime.Endpoint = ""
	node.Runtime.Command = []string{binary}
	node.Model = domain.ModelRef{Provider: "summary-fixture"}
	setup, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if got := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(setup), nil); got != 201 {
		t.Fatalf("save graph: %d", got)
	}
	workspace, err := app.API.Workspaces.Ensure(ctx, &graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "docs", "input.md"), []byte("green apples"), 0600); err != nil {
		t.Fatal(err)
	}
	baseURL := "/api/v1/graphs/eino-summary"
	if got := call(t, srv, "POST", baseURL+"/start", operatorTok, "", "", nil); got != 200 {
		t.Fatalf("start: %d", got)
	}
	for _, decision := range []string{"reject", "approve"} {
		var input domain.Message
		if got := call(t, srv, "POST", baseURL+"/channels/requests/messages", operatorTok, "application/json", `{"type":"summary.requested","payload":{"path":"docs/input.md"}}`, &input); got != 201 {
			t.Fatalf("inject: %d", got)
		}
		var approval *domain.Approval
		waitFor(t, 10*time.Second, "operator review", func() bool {
			list, err := app.Store.ListApprovals(ctx, storage.ApprovalFilter{GraphID: "eino-summary", Status: domain.ApprovalPending})
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 1 {
				xs, e := app.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: "eino-summary", NodeID: "summarize"})
				if e != nil {
					t.Fatal(e)
				}
				for _, x := range xs {
					if x.Status == domain.ExecFailed {
						t.Fatalf("summary execution failed: %s (tools %d, usage %+v)", x.Error, x.ToolCalls, x.Usage)
					}
				}
				return false
			}
			approval = list[0]
			return true
		})
		var held struct{ Message domain.Message }
		if err := json.Unmarshal(approval.Detail, &held); err != nil {
			t.Fatal(err)
		}
		var payload struct{ SourceMessageID, Summary string }
		if err := json.Unmarshal(held.Message.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if input.ID == "" || payload.SourceMessageID != input.ID || payload.Summary != "The document describes green apples." {
			t.Fatal("incorrect summary or source identity", payload)
		}
		waitFor(t, 5*time.Second, "one charged model and tool", func() bool {
			x, err := app.Store.GetExecution(ctx, approval.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			return x.Status == domain.ExecSucceeded && x.ToolCalls == 1 && x.Usage.InputTokens == 17 && x.Usage.OutputTokens == 6
		})
		before, err := app.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: "eino-summary", NodeID: "accepted"})
		if err != nil || len(before) != 0 {
			t.Fatalf("consumer ran before approval: %v %v", before, err)
		}
		if got := call(t, srv, "POST", "/api/v1/approvals/"+approval.ID+"/"+decision, operatorTok, "application/json", "{}", nil); got != 200 {
			t.Fatalf("%s: %d", decision, got)
		}
		want := domain.MsgRejected
		if decision == "approve" {
			want = domain.MsgAcked
		}
		waitFor(t, 5*time.Second, "settled message", func() bool {
			rows, err := app.Store.ListMessages(ctx, storage.MessageFilter{GraphID: "eino-summary", Channel: "summaries", Status: want})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if r.Message.ID == held.Message.ID {
					return true
				}
			}
			return false
		})
	}
	// The denied document exists. Its absence from model usage is evidence
	// of the path grant, independently of any filesystem lookup failure.
	if err := os.WriteFile(filepath.Join(workspace, "outside.md"), []byte("private fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	var denied domain.Message
	if got := call(t, srv, "POST", baseURL+"/channels/requests/messages", operatorTok, "application/json", `{"type":"summary.requested","payload":{"path":"outside.md"}}`, &denied); got != 201 {
		t.Fatalf("inject denied path: %d", got)
	}
	waitFor(t, 5*time.Second, "denied read fails before inference", func() bool {
		xs, err := app.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: "eino-summary", NodeID: "summarize", Status: domain.ExecFailed})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range xs {
			if len(x.InputIDs) == 1 && x.InputIDs[0] == denied.ID {
				if len(x.OutputIDs) != 0 || x.Usage.InputTokens != 0 || x.Usage.OutputTokens != 0 {
					t.Fatalf("denied document produced output: %+v", x)
				}
				return true
			}
		}
		return false
	})

}
