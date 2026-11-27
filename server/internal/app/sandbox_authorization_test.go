package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestUnconfiguredKubernetesSandboxPreservesWorkspaceShellDenial(t *testing.T) {
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
	if kind := sandbox.Kind(); kind != "kubernetes" {
		t.Fatalf("Kubernetes sandbox kind = %q, want kubernetes", kind)
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

func TestUnconfiguredKubernetesShellCannotRunOnController(t *testing.T) {
	factory := sandboxFactory(config.Default(), nil, nil, nil)
	gateway := tools.NewGateway(tools.Builtin(tools.BuiltinConfig{}), nil, nil)
	for _, mode := range []domain.IsolationMode{domain.IsolationKubernetes, domain.IsolationProcess} {
		t.Run(string(mode), func(t *testing.T) {
			root := t.TempDir()
			node := &domain.Node{ID: "worker", Isolation: domain.IsolationSpec{Mode: mode}}
			sb, err := factory(t.Context(), engine.SandboxSpec{Graph: &domain.Graph{}, Node: node, Workdir: root})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := sb.Close(); err != nil {
					t.Error(err)
				}
			}()
			grant := domain.ToolGrant{Name: "shell.exec", Commands: []string{"touch marker"}}
			res, err := gateway.Invoke(t.Context(), &tools.Principal{Sandbox: sb, Workdir: root, Grants: []domain.ToolGrant{grant}}, "shell.exec", []byte(`{"argv":["touch","marker"]}`))
			_, statErr := os.Stat(filepath.Join(root, "marker"))
			if mode == domain.IsolationKubernetes {
				if err != nil || res == nil || !res.IsError || !strings.Contains(res.Content, "Kubernetes replica sandbox") || !os.IsNotExist(statErr) {
					t.Fatalf("unconfigured Kubernetes command: result %v error %v marker %v", res, err, statErr)
				}
			} else if err != nil || res == nil || res.IsError || statErr != nil {
				t.Fatalf("explicit process command: result %v error %v marker %v", res, err, statErr)
			}
		})
	}
}

func TestKubernetesInstallationNeverUsesProcessSandbox(t *testing.T) {
	cfg := config.Default()
	cfg.Isolation.Kubernetes = &config.KubernetesIsolation{}
	factory := sandboxFactory(cfg, nil, nil, nil)
	for _, mode := range []domain.IsolationMode{"", domain.IsolationProcess, domain.IsolationKubernetes} {
		root := t.TempDir()
		sb, err := factory(t.Context(), engine.SandboxSpec{Graph: &domain.Graph{}, Node: &domain.Node{Isolation: domain.IsolationSpec{Mode: mode}}, Workdir: root})
		if err != nil {
			t.Fatal(err)
		}
		_, err = sb.Exec(t.Context(), tools.ExecRequest{Argv: []string{"touch", "marker"}})
		if _, stat := os.Stat(filepath.Join(root, "marker")); err == nil || !os.IsNotExist(stat) {
			t.Fatalf("%s: command escaped disabled sandbox", mode)
		}
	}
}
