package api_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/eventtest"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

type proposalProvider struct {
	calls   int
	body    string
	failure error
}

func (p *proposalProvider) Name() string                                    { return "analysis" }
func (p *proposalProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *proposalProvider) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.calls++
	if p.failure != nil {
		return nil, p.failure
	}
	if p.body != "" {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: p.body}}, nil
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: `{"environment":{"image":"alpine:3.22"},"rationale":"A minimal shell for the fixture; no dependencies need installing.","tools":["sh"]}`}}, nil
}

func TestEnvironmentProposalApprovalBuildAndResume(t *testing.T) {
	for _, scenario := range []string{"resume", "stop revokes resume", "budget blocks automatic start", "budget reconciler holds graph lease"} {
		t.Run(scenario, func(t *testing.T) {
			stop := scenario == "stop revokes resume"
			blocked := scenario == "budget blocks automatic start"
			root := t.TempDir()
			bin, log := fakeDaemonCLI(t)
			srv, app := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
			provider := &proposalProvider{}
			app.Models.Add(llm.ProviderConfig{Name: "analysis", DefaultModel: "fixture"}, provider)
			const base = "/api/v1/graphs/proposal"
			doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: proposal}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
			if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
				t.Fatalf("create %d", code)
			}
			dir := filepath.Join(root, "proposal")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(dir, "mise.toml")
			if err := os.WriteFile(manifest, []byte("[tools]\n"), 0644); err != nil {
				t.Fatal(err)
			}
			request := `{"model":{"provider":"analysis"},"resume":true}`
			if code := call(t, srv, "POST", base+"/environment/proposal", operatorTok, "application/json", request, nil); code != 403 {
				t.Fatalf("operator request %d", code)
			}
			var proposal environment.Proposal
			if code := call(t, srv, "POST", base+"/environment/proposal", adminTok, "application/json", request, &proposal); code != 201 || proposal.State != "awaiting_approval" {
				t.Fatalf("propose %d %+v", code, proposal)
			}
			if provider.calls != 1 {
				t.Fatalf("model calls %d", provider.calls)
			}
			if data, err := os.ReadFile(log); err == nil && len(data) > 0 {
				t.Fatalf("Docker ran before approval: %s", data)
			}
			if code := call(t, srv, "POST", base+"/start", operatorTok, "application/json", `{}`, nil); code != 422 {
				t.Fatalf("start bypassed approval %d", code)
			}
			if stop {
				if code := call(t, srv, "POST", base+"/stop", operatorTok, "application/json", `{}`, nil); code != 200 {
					t.Fatalf("stop %d", code)
				}
			}
			path := "/api/v1/approvals/" + proposal.ID + "/approve"
			for _, role := range taskAPIRoles {
				if role.adminWrite == 200 {
					continue // The admin case exercises the full proposal flow below.
				}
				if code := call(t, srv, "POST", path, role.token, "application/json", `{}`, nil); code != role.adminWrite {
					t.Fatalf("%s environment approval: got %d, want %d", role.name, code, role.adminWrite)
				}
			}
			if err := os.WriteFile(manifest, []byte("[tools]\nnode='22'"), 0644); err != nil {
				t.Fatal(err)
			}
			if code := call(t, srv, "POST", path, adminTok, "application/json", `{}`, nil); code != 400 {
				t.Fatalf("stale approval %d", code)
			}
			if err := os.WriteFile(manifest, []byte("[tools]\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if code := call(t, srv, "POST", path, adminTok, "application/json", `{}`, nil); code != 200 {
				t.Fatalf("approval %d", code)
			}
			if blocked {
				rec, err := app.Store.GetGraphRecord(t.Context(), "proposal")
				if err != nil {
					t.Fatal(err)
				}
				rec.State, rec.DesiredState, rec.RunID = domain.GraphPaused, domain.GraphPaused, "run-budget"
				if err := app.Store.PutGraphRecord(t.Context(), rec); err != nil {
					t.Fatal(err)
				}
				seedLifecycleLedger(t, app.Store, rec.ID, rec.RunID, cost.TaskRun, rec.ID)
			}
			if scenario == "budget reconciler holds graph lease" {
				ctx := t.Context()
				waitFor(t, 5*time.Second, "proposal build started", func() bool {
					app.Proposals.Reconcile(ctx)
					v, err := app.Proposals.Get(ctx, "proposal")
					if err != nil {
						t.Fatal(err)
					}
					if v.State == "failed" {
						t.Fatalf("proposal failed before lease test: %s", v.Error)
					}
					return v.State == "building"
				})
				const key = "graph-transition/proposal"
				const holder = "reconciler-fixture"
				if ok, err := app.Store.AcquireLease(ctx, key, holder, time.Minute); err != nil || !ok {
					t.Fatalf("acquire lease: %t %v", ok, err)
				}
				waitFor(t, 5*time.Second, "built proposal waiting for the graph lease", func() bool {
					app.Proposals.Reconcile(ctx)
					v, err := app.Proposals.Get(ctx, "proposal")
					if err != nil {
						t.Fatal(err)
					}
					if v.State == "failed" {
						t.Fatalf("proposal failed while the graph lease was held: %s", v.Error)
					}
					return v.State == "ready" && v.Resume
				})
				if err := app.Store.ReleaseLease(ctx, key, holder); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				app.Proposals.Reconcile(context.Background())
				v, err := app.Proposals.Get(context.Background(), "proposal")
				if err != nil {
					t.Fatal(err)
				}
				if v.State == "failed" {
					t.Fatalf("workflow failed: %s", v.Error)
				}
				if v.State == "ready" && !v.Resume {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			v, err := app.Proposals.Get(context.Background(), "proposal")
			if err != nil || v.State != "ready" || v.Resume {
				t.Fatalf("not ready/resumed %+v %v", v, err)
			}
			g, err := app.Store.GetGraph(context.Background(), "proposal", 0)
			if err != nil {
				t.Fatal(err)
			}
			if g.Spec.Environment == nil || g.Spec.Environment.Origin.Source != "agent" {
				t.Fatal("approved definition not saved")
			}
			rec, err := app.Store.GetGraphRecord(context.Background(), "proposal")
			expected := domain.GraphRunning
			if stop {
				expected = domain.GraphStopped
			}
			if blocked {
				expected = domain.GraphPaused
				var view environment.Proposal
				if code := call(t, srv, "GET", base+"/environment/proposal", viewerTok, "", "", &view); code != 200 ||
					view.State != "ready" || view.Resume || view.Error != "" || view.StartBlocked == nil ||
					view.StartBlocked.Scope != cost.TaskRun || view.StartBlocked.ID != "proposal" {
					t.Fatalf("wrong outcome for blocked automatic start: %d %+v", code, view)
				}
				events, err := app.Store.ListEvents(t.Context(), storage.EventFilter{
					GraphID: "proposal", Types: []domain.EventType{"environment.proposal"},
				})
				if err != nil || len(events) == 0 || !strings.Contains(string(events[0].Data), `"startBlocked":{"scope":"run","id":"proposal"}`) {
					t.Fatalf("missing scoped proposal audit: %v %v", events, err)
				}
				eventtest.Capture(t, app.Store, domain.EvBudgetGraphTransition)
			}
			if err != nil || rec.DesiredState != expected {
				t.Fatalf("not resumed %+v %v", rec, err)
			}
			if data, err := os.ReadFile(log); err != nil || len(data) == 0 {
				t.Fatalf("approved build not run: %v", err)
			}
			eventtest.Capture(t, app.Store, "environment.proposal")
		})
	}
}

func TestEnvironmentProposalRejectionAndCancellationDoNotBuild(t *testing.T) {
	for _, action := range []string{"reject", "cancel", "expire"} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			bin, log := fakeDaemonCLI(t)
			srv, app := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
			app.Models.Add(llm.ProviderConfig{Name: "analysis", DefaultModel: "fixture"}, &proposalProvider{})
			doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: proposal}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
			if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
				t.Fatal(code)
			}
			if err := os.MkdirAll(filepath.Join(root, "proposal"), 0755); err != nil {
				t.Fatal(err)
			}
			var proposal environment.Proposal
			if code := call(t, srv, "POST", "/api/v1/graphs/proposal/environment/proposal", adminTok, "application/json", `{"model":{"provider":"analysis"}}`, &proposal); code != 201 {
				t.Fatal(code)
			}
			if action == "expire" {
				a, err := app.Store.GetApproval(context.Background(), proposal.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = app.Approvals.Expire(context.Background(), a); err != nil {
					t.Fatal(err)
				}
			} else {
				path := "/api/v1/approvals/" + proposal.ID + "/reject"
				if action == "cancel" {
					path = "/api/v1/graphs/proposal/environment/proposal/cancel"
				}
				if action == "reject" {
					for _, role := range taskAPIRoles {
						if role.adminWrite == 200 {
							continue // The admin case exercises the full proposal flow below.
						}
						if code := call(t, srv, "POST", path, role.token, "application/json", `{}`, nil); code != role.adminWrite {
							t.Fatalf("%s environment rejection: got %d, want %d", role.name, code, role.adminWrite)
						}
					}
				}
				if code := call(t, srv, "POST", path, adminTok, "application/json", `{}`, nil); code != 200 {
					t.Fatal(code)
				}
			}
			app.Proposals.Reconcile(context.Background())
			if code := call(t, srv, "POST", "/api/v1/graphs/proposal/start", operatorTok, "application/json", `{}`, nil); code != 422 {
				t.Fatalf("start after %s: %d", action, code)
			}
			if data, err := os.ReadFile(log); err == nil && len(data) > 0 {
				t.Fatalf("Docker after %s: %s", action, data)
			}
		})
	}
}

func TestProposalAnalysisFailureDoesNotCreateApproval(t *testing.T) {
	for _, provider := range []*proposalProvider{{failure: errors.New("provider offline")}, {body: `{"environment":{"image":"alpine","network":{"run":"bridge"}},"rationale":"unsafe"}`}, {failure: context.DeadlineExceeded}} {
		root := t.TempDir()
		bin, log := fakeDaemonCLI(t)
		srv, app := setupApp(t, func(c *config.Config) { c.Workspaces.Root = root; c.Isolation.Docker.Bin = bin })
		app.Models.Add(llm.ProviderConfig{Name: "analysis"}, provider)
		if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: proposal}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}}]\n", nil); code != 201 {
			t.Fatal(code)
		}
		if err := os.MkdirAll(filepath.Join(root, "proposal"), 0755); err != nil {
			t.Fatal(err)
		}
		if code := call(t, srv, "POST", "/api/v1/graphs/proposal/environment/proposal", adminTok, "application/json", `{"model":{"provider":"analysis"}}`, nil); code != 400 {
			t.Fatalf("analysis failure: %d", code)
		}
		v, err := app.Proposals.Get(context.Background(), "proposal")
		if err != nil || v.State != "failed" || v.Error == "" {
			t.Fatalf("failure missing %+v %v", v, err)
		}
		if _, err := app.Store.GetApproval(context.Background(), v.ID); err == nil {
			t.Fatal("created approval for invalid analysis")
		}
		if data, err := os.ReadFile(log); err == nil && len(data) > 0 {
			t.Fatalf("Docker after failure %s", data)
		}
	}
}
