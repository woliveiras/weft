package agentcatalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestResolveKeepsLocalSettingsAndUsesOnlySelectedDefinition(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := Manager{Store: store}
	d := domain.AgentDefinition{Name: "Reviewer", Prompt: "Review @repository on @branch", ReadOnly: true,
		Parameters: []domain.AgentParameter{{Name: "repository", Required: true}, {Name: "branch", Default: "main"}}}
	if _, err := m.Publish(t.Context(), "reviewer", 0, d, "admin"); err != nil {
		t.Fatal(err)
	}
	d.Prompt = "New instructions"
	if _, err := m.Publish(t.Context(), "reviewer", 1, d, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Publish(t.Context(), "reviewer", 1, d, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale publish: %v", err)
	}
	g := &domain.Graph{Spec: domain.GraphSpec{
		Defaults: domain.NodeDefaults{Tools: []domain.ToolGrant{{Name: "shell.exec"}}, Model: domain.ModelRef{Provider: "unreviewed"}},
		Nodes: []domain.Node{{ID: "review", Tools: []domain.ToolGrant{{Name: "*"}}, Loop: domain.LoopSpec{Mode: domain.LoopEvent},
			Agent: &domain.AgentBinding{ID: "reviewer", Version: 1, Parameters: map[string]string{"repository": "example/repo"}}}},
	}}
	issues, err := m.Resolve(t.Context(), g)
	if err != nil || len(issues) > 0 {
		t.Fatalf("resolve: %v, %v", issues, err)
	}
	graph.Normalize(g)
	n := g.Node("review")
	if n == nil || !n.Workspace.ReadOnly || n.Loop.Mode != domain.LoopEvent || len(n.Tools) != 0 || n.Model.Provider != "" {
		t.Fatalf("effective node: %+v", n)
	}
	if got := graph.Substitute(n.Prompt, n.Agent.Parameters); got != "Review example/repo on main" {
		t.Fatalf("resolved prompt: %q", got)
	}
}

func TestAgentParameterErrors(t *testing.T) {
	p := []domain.AgentParameter{{Name: "repository", Required: true}}
	for _, tc := range []struct {
		input     map[string]string
		errorText string
	}{
		{nil, "required"},
		{map[string]string{"repository": " "}, "required"},
		{map[string]string{"repository": "one", "unknown": "value"}, "not declared"},
		{map[string]string{"repository": strings.Repeat("x", 17<<10)}, "exceeds"},
	} {
		if _, err := parameterValues(p, tc.input); err == nil || !strings.Contains(err.Error(), tc.errorText) {
			t.Fatalf("expected %q, got %v", tc.errorText, err)
		}
	}
}

type failedStore struct{ storage.Store }

func (failedStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, errors.New("catalog storage unavailable")
}

func TestResolvePropagatesDependencyFailure(t *testing.T) {
	m := Manager{Store: failedStore{}}
	g := &domain.Graph{Spec: domain.GraphSpec{Nodes: []domain.Node{{Agent: &domain.AgentBinding{ID: "reviewer", Version: 1}}}}}
	issues, err := m.Resolve(t.Context(), g)
	if err == nil || err.Error() != "catalog storage unavailable" || len(issues) != 0 {
		t.Fatalf("resolve: %v, %v", issues, err)
	}
}

func TestRegisterExistingWorkflowTakesStableSnapshot(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := Manager{Store: store}
	g := &domain.Graph{
		Metadata: domain.GraphMetadata{ID: "source-workflow"},
		Spec: domain.GraphSpec{
			Defaults: domain.NodeDefaults{Tools: []domain.ToolGrant{{Name: "filesystem.read"}}},
			Nodes:    []domain.Node{{ID: "source", Prompt: "First", LocalInstructions: "Only here"}},
		},
	}
	if _, err := store.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	taskCopy := *g
	taskCopy.Metadata.ID = domain.TaskRunGraphPrefix + "hidden"
	if _, err := store.SaveGraph(t.Context(), &taskCopy, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterExistingWorkflows(t.Context()); err != nil {
		t.Fatal(err)
	}
	g.Spec.Nodes[0].Prompt = "Later workflow change"
	if err := m.RegisterWorkflow(t.Context(), g, "author"); err != nil {
		t.Fatal(err)
	}
	items, err := m.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Origin == nil || items[0].Origin.Node != "source" {
		t.Fatalf("catalog contains unexpected entries: %+v, %v", items, err)
	}
	entry, err := m.Get(t.Context(), items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	d := entry.Versions[0].Definition
	if len(entry.Versions) != 1 || d.Prompt != "First" || len(d.Tools) != 1 || d.Tools[0].Name != "filesystem.read" {
		t.Fatalf("initial definition was lost: %+v", entry)
	}
	d.Prompt = "New shared version"
	published, err := m.Publish(t.Context(), entry.ID, 1, d, "author")
	if err != nil || published.Origin == nil || published.Origin.Workflow != "source-workflow" || published.Versions[1].Version != 2 {
		t.Fatalf("publish from automatic entry: %+v, %v", published, err)
	}
}
