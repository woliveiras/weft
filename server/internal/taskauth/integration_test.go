package taskauth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

type sampleTool struct{ calls int }

func (*sampleTool) Name() string            { return "test.call" }
func (*sampleTool) Capability() string      { return "test.call" }
func (*sampleTool) Description() string     { return "test" }
func (*sampleTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (s *sampleTool) Invoke(_ context.Context, _ *tools.Invocation) (*tools.Result, error) {
	s.calls++
	return &tools.Result{Content: "ok"}, nil
}

type approver struct {
	calls  int
	onWait func()
}

func (a *approver) RequestApproval(_ context.Context, r *domain.Approval) (*domain.Approval, error) {
	a.calls++
	if a.onWait != nil {
		a.onWait()
	}
	r.Status = domain.ApprovalApproved
	return r, nil
}
func setup(t *testing.T) (Manager, *taskconfig.Draft, *tools.Principal) {
	return setupAt(t, t.TempDir())
}
func setupAt(t *testing.T, root string) (Manager, *taskconfig.Draft, *tools.Principal) {
	t.Helper()
	s, err := fsstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "workflow"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Tools: []domain.ToolGrant{{Name: "test.call"}}}}}}
	if _, err = s.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	d, err := (taskconfig.Manager{Store: s}).Save(t.Context(), "", taskconfig.Input{Title: "Task", Request: "test", Outcome: "freeform", GraphID: "workflow", GraphVersion: 1, NodeID: "worker"}, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return Manager{Store: s}, d, &tools.Principal{GraphID: "task-run-" + strings.TrimPrefix(d.ID, "tsk_"), GraphVersion: 1, NodeID: "worker", Grants: []domain.ToolGrant{{Name: "test.call"}}}
}
func launch(t *testing.T, m Manager, d *taskconfig.Draft, p *tools.Principal) {
	t.Helper()
	d.ExecutionGraphID = p.GraphID
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: p.GraphID, Labels: map[string]string{"weft.task": d.ID}}}
	if _, err := m.Store.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Bind(t.Context(), d.ID, g); err != nil {
		t.Fatal(err)
	}
	if _, err := (taskconfig.Manager{Store: m.Store}).Transition(t.Context(), d, "running", "admin"); err != nil {
		t.Fatal(err)
	}
}
func TestGatewayConsentAndMandatoryGrants(t *testing.T) {
	for _, mode := range []Mode{Ask, Workspace, Full} {
		t.Run(string(mode), func(t *testing.T) {
			m, d, p := setup(t)
			r, err := m.Review(t.Context(), d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.Save(t.Context(), d.ID, mode, r.Scope, 0, "admin", true); err != nil {
				t.Fatal(err)
			}
			launch(t, m, d, p)
			tool := &sampleTool{}
			reg := tools.NewRegistry()
			reg.Register(tool)
			ap := &approver{}
			gw := tools.NewGateway(reg, ap, nil)
			gw.TaskAuthorization = m.Check
			if _, err = gw.Invoke(t.Context(), p, tool.Name(), nil); err != nil {
				t.Fatal(err)
			}
			expected := 1
			if mode == Full {
				expected = 0
			}
			if ap.calls != expected || tool.calls != 1 {
				t.Fatal(ap.calls, tool.calls)
			}
			p.Grants[0].RequiresApproval = true
			if _, err = gw.Invoke(t.Context(), p, tool.Name(), nil); err != nil || ap.calls != expected+1 {
				t.Fatal(err, ap.calls)
			}
			p.NodeID = "temporary-child"
			p.Grants = nil
			if _, err = gw.Invoke(t.Context(), p, tool.Name(), nil); !errors.Is(err, tools.ErrDenied) {
				t.Fatal(err)
			}
		})
	}
}

func TestRunningLegacyTaskWithoutConsent(t *testing.T) {
	ctx := t.Context()
	m, draft, principal := setup(t)
	// Seed a running execution copy in the pre-consent shape. Current task
	// admission creates and binds consent, so it cannot build this fixture.
	draft.ExecutionGraphID = principal.GraphID
	execution := &domain.Graph{
		Metadata: domain.GraphMetadata{ID: principal.GraphID, Labels: map[string]string{"weft.task": draft.ID}},
		Spec: domain.GraphSpec{Nodes: []domain.Node{
			{ID: draft.NodeID, Tools: principal.Grants},
			{ID: "reviewed", Tools: []domain.ToolGrant{{Name: "test.call", RequiresApproval: true}}},
			{ID: "ungranted"},
		}},
	}
	if _, err := m.Store.SaveGraph(ctx, execution, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := (taskconfig.Manager{Store: m.Store}).Transition(ctx, draft, "running", "admin"); err != nil {
		t.Fatal(err)
	}
	review, err := m.Review(ctx, draft.ID)
	if err != nil || review.Record != nil {
		t.Fatalf("expected no consent for running legacy task: %+v, %v", review, err)
	}
	saved, err := m.Store.GetGraph(ctx, principal.GraphID, 0)
	if err != nil {
		t.Fatal(err)
	}

	tool := &sampleTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	ap := &approver{onWait: func() {
		if tool.calls != 1 {
			t.Fatalf("tool ran before approval: %d calls", tool.calls)
		}
	}}
	gw := tools.NewGateway(reg, ap, nil)
	gw.TaskAuthorization = m.Check

	principalFor := func(id string) *tools.Principal {
		t.Helper()
		node := saved.Node(id)
		if node == nil {
			t.Fatalf("legacy execution copy missing node %q", id)
		}
		return &tools.Principal{GraphID: saved.Metadata.ID, GraphVersion: saved.Metadata.Version, NodeID: node.ID, Grants: node.Tools}
	}
	if result, err := gw.Invoke(ctx, principalFor(draft.NodeID), tool.Name(), nil); err != nil ||
		result == nil || result.Content != "ok" || tool.calls != 1 || ap.calls != 0 {
		t.Fatalf("legacy grant without approval: result=%+v err=%v calls=%d approvals=%d", result, err, tool.calls, ap.calls)
	}
	gated := principalFor("reviewed")
	if _, err := gw.Invoke(tools.WithoutApproval(ctx), gated, tool.Name(), nil); !errors.Is(err, tools.ErrDenied) ||
		tool.calls != 1 || ap.calls != 0 {
		t.Fatalf("mandatory approval was not enforced: err=%v calls=%d approvals=%d", err, tool.calls, ap.calls)
	}
	if result, err := gw.Invoke(ctx, gated, tool.Name(), nil); err != nil ||
		result == nil || result.Content != "ok" || tool.calls != 2 || ap.calls != 1 {
		t.Fatalf("legacy grant with approval: result=%+v err=%v calls=%d approvals=%d", result, err, tool.calls, ap.calls)
	}
	if _, err := gw.Invoke(ctx, principalFor("ungranted"), tool.Name(), nil); !errors.Is(err, tools.ErrDenied) ||
		tool.calls != 2 || ap.calls != 1 {
		t.Fatalf("ungranted legacy call: err=%v calls=%d approvals=%d", err, tool.calls, ap.calls)
	}

	for _, mode := range []Mode{Ask, Workspace, Full} {
		if _, err := m.Save(ctx, draft.ID, mode, review.Scope, 0, "admin", true); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("running legacy task adopted %s: %v", mode, err)
		}
	}
	review, err = m.Review(ctx, draft.ID)
	if err != nil || review.Record != nil {
		t.Fatalf("legacy task acquired consent: %+v, %v", review, err)
	}
}

func TestConsentInvalidationAndPersistence(t *testing.T) {
	m, d, p := setup(t)
	r, _ := m.Review(t.Context(), d.ID)
	if _, err := m.Save(t.Context(), d.ID, Full, r.Scope, 0, "admin", false); err == nil {
		t.Fatal("missing confirmation")
	}
	if _, err := m.Save(t.Context(), d.ID, Full, "stale", 0, "admin", true); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := m.Save(t.Context(), d.ID, Full, r.Scope, 0, "admin", true); err != nil {
		t.Fatal(err)
	}
	loaded, err := (Manager{Store: m.Store}).Review(t.Context(), d.ID)
	if err != nil || loaded.Record.Actor != "admin" || loaded.Record.Mode != Full {
		t.Fatal(loaded, err)
	}
	launch(t, m, d, p)
	g, _ := m.Store.GetGraph(t.Context(), "workflow", 0)
	g.Metadata.Name = "changed"
	m.Store.SaveGraph(t.Context(), g, 1)
	if _, _, err = m.Check(t.Context(), p, &sampleTool{}, false); err == nil {
		t.Fatal("stale workflow accepted")
	}
}

func TestPendingApprovalRevalidatesExecutionScope(t *testing.T) {
	m, d, p := setup(t)
	if err := m.Ensure(t.Context(), d.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	launch(t, m, d, p)
	tool := &sampleTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	ap := &approver{onWait: func() {
		g, err := m.Store.GetGraph(t.Context(), p.GraphID, 0)
		if err != nil {
			t.Fatal(err)
		}
		g.Spec.Nodes = append(g.Spec.Nodes, domain.Node{ID: "unreviewed"})
		if _, err = m.Store.SaveGraph(t.Context(), g, 1); err != nil {
			t.Fatal(err)
		}
	}}
	gw := tools.NewGateway(reg, ap, nil)
	gw.TaskAuthorization = m.Check
	if _, err := gw.Invoke(t.Context(), p, tool.Name(), nil); !errors.Is(err, tools.ErrDenied) || tool.calls != 0 {
		t.Fatal(err, tool.calls)
	}
}

type unavailable struct{ storage.Store }

func (unavailable) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, errors.New("storage unavailable")
}
func TestDependencyFailureDenies(t *testing.T) {
	m, d, p := setup(t)
	_ = d
	m.Store = unavailable{m.Store}
	if _, _, err := m.Check(t.Context(), p, &sampleTool{}, false); err == nil {
		t.Fatal("ignored dependency failure")
	}
}

func TestReductionDuringApprovalAndCancellation(t *testing.T) {
	m, d, p := setup(t)
	review, _ := m.Review(t.Context(), d.ID)
	if _, err := m.Save(t.Context(), d.ID, Full, review.Scope, 0, "admin", true); err != nil {
		t.Fatal(err)
	}
	launch(t, m, d, p)
	tool := &sampleTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	p.Grants[0].RequiresApproval = true
	ap := &approver{onWait: func() {
		r, err := m.Review(t.Context(), d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = m.Save(t.Context(), d.ID, Ask, r.Scope, r.Record.Revision, "admin", true); err != nil {
			t.Fatal(err)
		}
	}}
	gw := tools.NewGateway(reg, ap, nil)
	gw.TaskAuthorization = m.Check
	if _, err := gw.Invoke(t.Context(), p, tool.Name(), nil); !errors.Is(err, tools.ErrDenied) || tool.calls != 0 {
		t.Fatal(err, tool.calls)
	}
	ap.onWait = nil
	if _, err := gw.Invoke(t.Context(), p, tool.Name(), nil); err != nil || tool.calls != 1 {
		t.Fatal(err, tool.calls)
	}
	r, _ := m.Review(t.Context(), d.ID)
	if _, err := m.Save(t.Context(), d.ID, Full, r.Scope, r.Record.Revision, "admin", true); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("expanded live mode", err)
	}
	current, _ := (taskconfig.Manager{Store: m.Store}).Get(t.Context(), d.ID)
	if _, err := (taskconfig.Manager{Store: m.Store}).Transition(t.Context(), current, "cancel_requested", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Invoke(t.Context(), p, tool.Name(), nil); !errors.Is(err, tools.ErrDenied) || tool.calls != 1 {
		t.Fatal(err, tool.calls)
	}
}
func TestPreferenceIsOnlySuggestion(t *testing.T) {
	m, d, _ := setup(t)
	in := d.Input
	in.ProjectID = "project"
	in.BaseCommit = strings.Repeat("a", 40)
	d, err := (taskconfig.Manager{Store: m.Store}).Save(t.Context(), d.ID, in, d.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	review, _ := m.Review(t.Context(), d.ID)
	if _, err = m.Save(t.Context(), d.ID, Full, review.Scope, 0, "admin", true); err != nil {
		t.Fatal(err)
	}
	if err = m.Remember(t.Context(), d.ID, Full, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	if err = m.Remember(t.Context(), d.ID, Full, 0, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	next, err := (taskconfig.Manager{Store: m.Store}).Save(t.Context(), "", in, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	review, err = m.Review(t.Context(), next.ID)
	if err != nil || review.Record != nil || review.Preference == nil || review.Preference.Mode != Full {
		t.Fatal(review, err)
	}
	if err = m.Ensure(t.Context(), next.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	review, _ = m.Review(t.Context(), next.ID)
	if review.Record.Mode != Ask {
		t.Fatal("suggestion silently expanded scope")
	}
}

func TestConsentSurvivesStoreRestart(t *testing.T) {
	root := t.TempDir()
	m, d, p := setupAt(t, root)
	r, err := m.Review(t.Context(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Save(t.Context(), d.ID, Full, r.Scope, 0, "admin", true); err != nil {
		t.Fatal(err)
	}
	launch(t, m, d, p)
	if err = m.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := fsstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	m.Store = reopened
	required, _, err := m.Check(t.Context(), p, &sampleTool{}, false)
	if err != nil || required {
		t.Fatal(required, err)
	}
	r, err = m.Review(t.Context(), d.ID)
	if err != nil || r.Record.ExecutionScope == "" || r.Record.Actor != "admin" {
		t.Fatal(r, err)
	}
}

func TestProcessShellRequiresAskOrExplicitFull(t *testing.T) {
	for _, tc := range []struct {
		isolation domain.IsolationMode
		mode      Mode
	}{
		{domain.IsolationProcess, Ask},
		{domain.IsolationProcess, Full},
		{domain.IsolationKubernetes, Ask},
		{domain.IsolationKubernetes, Full},
	} {
		t.Run(string(tc.isolation)+"/"+string(tc.mode), func(t *testing.T) {
			m, d, p := setup(t)
			g, err := m.Store.GetGraph(t.Context(), d.GraphID, 0)
			if err != nil {
				t.Fatal(err)
			}
			g.Spec.Nodes[0].Isolation.Mode = tc.isolation
			g.Spec.Nodes[0].Tools = []domain.ToolGrant{{Name: "shell.exec"}}
			if _, err = m.Store.SaveGraph(t.Context(), g, 1); err != nil {
				t.Fatal(err)
			}
			in := d.Input
			in.GraphVersion = 2
			d, err = (taskconfig.Manager{Store: m.Store}).Save(t.Context(), d.ID, in, d.Revision, "admin")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.Review(t.Context(), d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.Save(t.Context(), d.ID, Workspace, r.Scope, 0, "admin", true); !errors.Is(err, ErrInvalid) {
				t.Fatal("process shell admitted under Workspace access", err)
			}
			if _, err = m.Save(t.Context(), d.ID, tc.mode, r.Scope, 0, "admin", true); err != nil {
				t.Fatal(err)
			}
			launch(t, m, d, p)
			p.Grants = g.Spec.Nodes[0].Tools
			p.Workdir = t.TempDir()
			p.Sandbox = &tools.ProcessSandbox{Root: p.Workdir}
			ap := &approver{}
			gw := tools.NewGateway(tools.Builtin(tools.BuiltinConfig{}), ap, nil)
			gw.TaskAuthorization = m.Check
			res, err := gw.Invoke(t.Context(), p, "shell.exec", json.RawMessage(`{"command":"printf fixture"}`))
			if err != nil || res.IsError || !strings.Contains(res.Content, "fixture") {
				t.Fatal(res, err)
			}
			want := 0
			if tc.mode == Ask {
				want = 1
			}
			if ap.calls != want {
				t.Fatal(ap.calls)
			}
		})
	}
}
