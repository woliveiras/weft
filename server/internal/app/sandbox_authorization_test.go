package app

import (
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestKubernetesSandboxFactoryUsesProcessShellPolicy(t *testing.T) {
	node := domain.Node{
		ID:        "worker",
		Isolation: domain.IsolationSpec{Mode: domain.IsolationKubernetes},
		Tools:     []domain.ToolGrant{{Name: "shell.exec"}},
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "kubernetes-fixture"}}
	factory := sandboxFactory(config.Default(), nil, nil, nil)
	sandbox, err := factory(t.Context(), engine.SandboxSpec{Graph: g, Node: &node, Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sandbox.Close(); err != nil {
			t.Error(err)
		}
	})
	if kind := sandbox.Kind(); kind != "process" {
		t.Fatalf("Kubernetes sandbox kind = %q, want process", kind)
	}
	registry := tools.Builtin(tools.BuiltinConfig{})
	for _, name := range []string{"shell.exec", tools.CheckToolName} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if _, err := tools.Authorize(&tools.Principal{Grants: node.Tools}, tool); err != nil {
			t.Fatalf("fixture needs a grant for %s: %v", name, err)
		}
		if approval, err := taskauth.Approval(taskauth.Workspace, tool.Capability(), sandbox.Kind(), false); approval || !errors.Is(err, taskauth.ErrDenied) {
			t.Errorf("Workspace approved %s: approval %t, error %v", name, approval, err)
		}
	}
}
