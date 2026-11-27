//go:build kubernetes_e2e

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/woliveiras/weft/server/internal/workspace"
)

type publicationModel struct {
	catalogModel
	request json.RawMessage
}

func (publicationModel) Name() string { return "publish-fixture" }
func (m publicationModel) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	for _, msg := range r.Messages {
		if msg.Role == llm.RoleTool && msg.ToolCallID == "publish" {
			return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "done", Name: "finish", Arguments: json.RawMessage(`{"summary":"publication processed"}`)}}}}, nil
		}
	}
	for _, tool := range r.Tools {
		if tool.Name == "git.publish" {
			return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "publish", Name: tool.Name, Arguments: m.request}}}}, nil
		}
	}
	return nil, fmt.Errorf("git.publish missing from turn tool schemas")
}

func TestKubernetesAuthorizedBranchPublication(t *testing.T) {
	root := os.Getenv("WEFT_FIXTURE_WORKSPACES")
	if root == "" {
		t.Fatal("set WEFT_FIXTURE_WORKSPACES to the mounted claim")
	}
	dir, err := os.MkdirTemp(root, "publish-")
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(dir)
	srv, a := setupApp(t, func(c *config.Config) {
		c.Workspaces.Root = root
		c.Isolation.Kubernetes = &config.KubernetesIsolation{Image: "weft/sandbox-go:dev"}
	})
	model := &publicationModel{}
	a.Models.Add(llm.ProviderConfig{Name: "publish-fixture", Type: "mock", DefaultModel: "fixture"}, model)
	ctx := t.Context()
	graph := `apiVersion: weft.io/v1
kind: Graph
metadata: {name: ` + id + `}
spec:
  workspace: {git: true}
  nodes:
    - id: dev
      runtime: {kind: native}
      prompt: Publish the reviewed private branch.
      model: {provider: publish-fixture}
      workspace: {mode: worktree}
      tools: [{name: git.publish, requiresApproval: true}]
  channels: [{id: in, to: [dev]}]
`
	var created json.RawMessage
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", graph, &created); code != 201 {
		t.Fatalf("create: %d %s", code, created)
	}
	g, _, err := a.Engine.GetGraph(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	clone, branch, err := a.API.Workspaces.Replica(ctx, g, g.Node("dev"), 0)
	if err != nil {
		t.Fatal(err)
	}
	base, err := a.API.Workspaces.GraphDir(g)
	if err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	firstBase := git(base, "rev-parse", "HEAD")
	commit := func(label string) string {
		git(clone, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", label)
		return git(clone, "rev-parse", "HEAD")
	}
	first := commit("first")
	prefix := "/api/v1/graphs/" + id
	endpoint := prefix + "/nodes/dev/replicas/0/git/publish"
	body := func(commit, prev string) string { return fmt.Sprintf(`{"commit":%q,"previous":%q}`, commit, prev) }
	post := func(path, token, payload string, want int, out any) {
		t.Helper()
		if got := call(t, srv, "POST", path, token, "application/json", payload, out); got != want {
			t.Fatalf("POST %s returned %d, want %d", path, got, want)
		}
	}
	post(endpoint, viewerTok, body(first, ""), 403, nil)
	post(endpoint, "", body(first, ""), 401, nil)
	var receipt workspace.PublishedBranch
	for i := 0; i < 2; i++ {
		post(endpoint, operatorTok, body(first, ""), 200, &receipt)
		if receipt.Branch != branch || receipt.Commit != first {
			t.Fatalf("receipt %+v", receipt)
		}
	}
	ref := "refs/heads/" + branch
	second := commit("second")
	var inspection workspace.Inspection
	if got := call(t, srv, "GET", prefix+"/git/history?node=dev&replica=0", viewerTok, "", "", &inspection); got != 200 || !strings.HasPrefix(inspection.Content, second+" ") {
		t.Fatalf("private branch history: %d %s", got, inspection.Content)
	}
	post(endpoint, operatorTok, body(second, firstBase), 409, nil)
	if got := git(base, "rev-parse", ref); got != first {
		t.Fatalf("conflicting update changed tip to %s", got)
	}
	model.request = json.RawMessage(body(second, first))
	post(prefix+"/start", operatorTok, "{}", 200, nil)
	post(prefix+"/channels/in/messages", operatorTok, `{"type":"job","payload":{}}`, 201, nil)
	var pending *domain.Approval
	waitFor(t, 45*time.Second, "agent publication approval", func() bool {
		approvals, err := a.Store.ListApprovals(ctx, storage.ApprovalFilter{GraphID: id, Status: domain.ApprovalPending})
		if err != nil {
			t.Fatal(err)
		}
		if len(approvals) > 0 {
			pending = approvals[0]
			return true
		}
		failed, err := a.Store.ListExecutions(ctx, storage.ExecutionFilter{GraphID: id, Status: domain.ExecFailed})
		if err != nil {
			t.Fatal(err)
		}
		if len(failed) > 0 {
			t.Fatal(failed[0].Error)
		}
		return false
	})
	if got := git(base, "rev-parse", ref); got != first {
		t.Fatalf("unapproved update changed tip to %s", got)
	}
	post("/api/v1/approvals/"+pending.ID+"/approve", operatorTok, "{}", 200, nil)
	waitFor(t, 60*time.Second, "agent publication audit", func() bool {
		events, err := a.Store.ListEvents(ctx, storage.EventFilter{GraphID: id, Types: []domain.EventType{domain.EvBranchPublication}})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if e.Actor == "agent:dev#0" {
				var d struct{ Outcome, Commit string }
				if err := json.Unmarshal(e.Data, &d); err != nil {
					t.Fatal(err)
				}
				if d.Outcome != "published" || d.Commit != second || e.ExecutionID != pending.ExecutionID {
					t.Fatalf("unexpected agent publication: %+v %s", e, e.Data)
				}
				return true
			}
		}
		return false
	})
	if git(base, "rev-parse", ref) != second || git(base, "rev-parse", "HEAD") != firstBase {
		t.Fatal("published branch or graph HEAD is incorrect")
	}
}
