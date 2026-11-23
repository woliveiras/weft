package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"gopkg.in/yaml.v3"
)

type securityDiscoveryModel struct {
	calls  atomic.Int32
	denied bool
}

func (*securityDiscoveryModel) Name() string                                    { return "security-fixture" }
func (*securityDiscoveryModel) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (m *securityDiscoveryModel) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if m.calls.Add(1) == 1 {
		return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "discovery", Name: "scm.security.alerts", Arguments: json.RawMessage(`{"connection":"weft-github","baseBranch":"main"}`)}}}}, nil
	}
	for _, message := range r.Messages {
		if message.Role == llm.RoleTool && message.Name == "scm.security.alerts" {
			if m.denied {
				if !strings.Contains(message.Content, "403") {
					return nil, fmt.Errorf("fixture did not observe explicit provider failure")
				}
				return nil, fmt.Errorf("security discovery blocked; no delivery attempted")
			}
			if !strings.Contains(message.Content, `"groups":[]`) {
				return nil, fmt.Errorf("fixture refuses an unconfirmed empty inventory")
			}
			return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "done", Name: "finish", Arguments: json.RawMessage(`{"summary":"No open dependency alerts observed. No edits, push or PR attempted."}`)}}}}, nil
		}
	}
	return nil, fmt.Errorf("discovery evidence missing")
}

func TestDependencySecurityExampleRequiresManualTaskDispatch(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint("denied=", denied), func(t *testing.T) {
			var reads, writes atomic.Int32
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes.Add(1)
					w.WriteHeader(500)
					return
				}
				reads.Add(1)
				if r.URL.Path != "/repos/woliveiras/weft/dependabot/alerts" {
					t.Error("unexpected remote path", r.URL.Path)
				}
				if denied {
					w.WriteHeader(403)
					return
				}
				fmt.Fprint(w, "[]")
			}))
			defer remote.Close()
			srv, a := setupApp(t, nil)
			credential := "fixture-security-credential"
			if _, err := a.SCM.Save(t.Context(), scm.Input{Name: "weft-github", Kind: "github", Repository: "woliveiras/weft", APIURL: remote.URL, Token: &credential}, "admin", false); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile("../../../examples/dependency-security/workflow.yaml")
			if err != nil {
				t.Fatal(err)
			}
			var g domain.Graph
			if err = yaml.Unmarshal(raw, &g); err != nil {
				t.Fatal(err)
			}
			if len(g.Spec.Nodes) != 1 || len(g.Spec.Channels) != 1 || g.Spec.Nodes[0].Loop.Mode != "once" || g.Spec.Nodes[0].Isolation.Mode != "docker" {
				t.Fatal("example lost manual sandbox configuration")
			}
			for _, grant := range g.Spec.Nodes[0].Tools {
				if strings.Contains(grant.Name, "merge") || strings.Contains(grant.Name, "dismiss") || strings.HasPrefix(grant.Name, "mcp.") {
					t.Fatal("unnecessary mutation escape", grant)
				}
			}
			// Only remote discovery is exercised here. Replace Docker/build/checkout
			// with a process fixture; no repository code or shell tool is invoked.
			g.Spec.Environment = nil
			g.Spec.Workspace = domain.WorkspaceSpec{}
			g.Spec.Nodes[0].Isolation.Mode = "process"
			g.Spec.Nodes[0].Model = domain.ModelRef{Provider: "security-fixture"}
			model := &securityDiscoveryModel{denied: denied}
			a.Models.Add(llm.ProviderConfig{Name: "security-fixture", Type: "mock", DefaultModel: "fixture"}, model)
			raw, _ = json.Marshal(g)
			if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(raw), nil); code != 201 {
				t.Fatal("save example", code)
			}
			var draft taskconfig.Draft
			body := `{"title":"Manual dependency security","request":"Check current alerts","outcome":"understand","graphId":"dependency-security","graphVersion":1,"nodeId":"security-fixer"}`
			if code := call(t, srv, "POST", "/api/v1/tasks", adminTok, "application/json", body, &draft); code != 201 {
				t.Fatal("draft", code)
			}
			xs, err := a.Store.ListExecutions(t.Context(), storage.ExecutionFilter{})
			if err != nil || len(xs) != 0 || reads.Load() != 0 || model.calls.Load() != 0 {
				t.Fatal("installation or drafting dispatched work", xs, reads.Load(), model.calls.Load(), err)
			}
			var review taskauth.Review
			endpoint := "/api/v1/tasks/" + draft.ID + "/authorization"
			if code := call(t, srv, "GET", endpoint, adminTok, "", "", &review); code != 200 {
				t.Fatal(code)
			}
			authorization, _ := json.Marshal(map[string]any{"mode": "full", "scope": review.Scope, "confirm": true})
			req, _ := http.NewRequest("PUT", srv.URL+endpoint, strings.NewReader(string(authorization)))
			req.Header.Set("Authorization", "Bearer "+adminTok)
			req.Header.Set("If-Match", "0")
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatal("consent", res.StatusCode)
			}
			if code := taskStart(t, srv.URL, draft.ID, adminTok, draft.Revision); code != 202 {
				t.Fatal("manual start", code)
			}
			waitFor(t, 5*time.Second, "security task outcome", func() bool {
				var v taskrun.View
				call(t, srv, "GET", "/api/v1/tasks/"+draft.ID+"/progress", adminTok, "", "", &v)
				if denied {
					return v.State == "failed"
				}
				return v.State == "succeeded" && v.Result != nil && strings.Contains(v.Result.Summary, "No open dependency alerts")
			})
			if reads.Load() != 1 || writes.Load() != 0 {
				t.Fatal("unexpected provider operations", reads.Load(), writes.Load())
			}
		})
	}
}
