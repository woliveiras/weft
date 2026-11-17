package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

type environmentGate struct{ err error }

func (g *environmentGate) Check(context.Context, *domain.Graph) error { return g.err }

type replacementSandbox struct {
	tools.ProcessSandbox
	closed bool
	fail   bool
}

func (s *replacementSandbox) Close() error {
	if s.fail {
		return errors.New("cannot remove old sandbox")
	}
	s.closed = true
	return nil
}
func TestApprovedEnvironmentSandboxReplacementIntegration(t *testing.T) {
	ctx := context.Background()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ws, err := workspace.NewManager(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "project"}, Spec: domain.GraphSpec{Environment: &domain.EnvironmentSpec{Image: "approved:fixture"}}}
	if _, err := store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	gate := &environmentGate{err: errors.New("awaiting approval")}
	old := &replacementSandbox{}
	fresh := &replacementSandbox{}
	calls := 0
	failCreate := false
	e := &Engine{store: store, opt: Options{EnvironmentProposals: gate, Environments: gate, Workspaces: ws, Sandboxes: func(_ context.Context, s SandboxSpec) (tools.Sandbox, error) {
		calls++
		if !old.closed || s.Graph.Spec.Environment.Image != "approved:fixture" || s.Workdir != "workspace" {
			t.Fatal("incorrect replacement", s)
		}
		if failCreate {
			return nil, errors.New("capacity unavailable")
		}
		return fresh, nil
	}}}
	r := &replica{sandbox: old}
	turn := &turnEnv{r: r, gr: &graphRun{e: e}, n: &domain.Node{Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}, principal: &tools.Principal{GraphID: "project", Workdir: "workspace", Sandbox: old}}
	if err := turn.ensureEnvironment(ctx); err == nil || old.closed || calls != 0 {
		t.Fatal("approval bypass", err)
	}
	gate.err = nil
	old.fail = true
	if err := turn.ensureEnvironment(ctx); err == nil || calls != 0 {
		t.Fatal("cleanup failure ignored", err)
	}
	old.fail = false
	failCreate = true
	if err := turn.ensureEnvironment(ctx); err == nil {
		t.Fatal("capacity failure ignored")
	}
	failCreate = false
	if err := turn.ensureEnvironment(ctx); err != nil {
		t.Fatal(err)
	}
	if turn.principal.Sandbox != fresh || r.sandbox != fresh {
		t.Fatal("old sandbox still exposed")
	}
	before := calls
	if err := turn.ensureEnvironment(ctx); err != nil || calls != before {
		t.Fatal("redundant sandbox replacement", err)
	}
}
