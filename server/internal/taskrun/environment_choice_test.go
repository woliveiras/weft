package taskrun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestEnvironmentChoicePreservesIsolationAndChecksOnlySandbox(t *testing.T) {
	g := &domain.Graph{Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Isolation: domain.IsolationSpec{Mode: domain.IsolationProcess}}}}}
	d := &taskconfig.Draft{Input: taskconfig.Input{ExecutionMode: "local", ProjectID: "prj_fixture"}}
	calls := 0
	m := Manager{NativeProfile: true, CheckSandbox: func(context.Context) error { calls++; return errors.New("offline") }}
	if b := m.checkEnvironment(t.Context(), d, g); len(b) != 0 || calls != 0 {
		t.Fatal(b, calls)
	}
	m.NativeProfile = false
	if b := m.checkEnvironment(t.Context(), d, g); len(b) == 0 {
		t.Fatal("server presented host execution")
	}
	m.NativeProfile = true
	g.Spec.Nodes[0].Isolation.Mode = domain.IsolationDocker
	if b := m.checkEnvironment(t.Context(), d, g); !strings.Contains(strings.Join(b, " "), "does not downgrade") {
		t.Fatal(b)
	}
	if g.Spec.Nodes[0].Isolation.Mode != domain.IsolationDocker {
		t.Fatal("workflow isolation changed")
	}
	d.ExecutionMode = "sandbox"
	if b := m.checkEnvironment(t.Context(), d, g); !strings.Contains(strings.Join(b, " "), "unavailable") {
		t.Fatal(b)
	}
	m.CheckSandbox = func(context.Context) error { return nil }
	if b := m.checkEnvironment(t.Context(), d, g); len(b) != 0 {
		t.Fatal(b)
	}
	g.Spec.Nodes[0].Isolation.Mode = domain.IsolationProcess
	if b := m.checkEnvironment(t.Context(), d, g); !strings.Contains(strings.Join(b, " "), "every enabled agent") {
		t.Fatal(b)
	}
	m.CheckSandbox = nil
	if b := m.checkEnvironment(t.Context(), d, g); !strings.Contains(strings.Join(b, " "), "cannot be verified") {
		t.Fatal(b)
	}
}
