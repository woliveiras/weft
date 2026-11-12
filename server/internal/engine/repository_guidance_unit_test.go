package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestGuidanceCheckoutRootRedactsLookupFailure(t *testing.T) {
	root := t.TempDir()
	g := &domain.Graph{}
	g.Spec.Workspace.Path = filepath.Join(root, "restricted")
	n := &domain.Node{}
	n.Workspace.Subdir = "src"
	checkout, err := guidanceCheckoutRoot(&workspace.Manager{Root: root}, g, n, filepath.Join(root, "src"))
	if checkout != "" || !errors.Is(err, instructions.ErrWorkspaceUnavailable) || strings.Contains(err.Error(), root) {
		t.Fatalf("unexpected checkout resolution %q, %v", checkout, err)
	}
}

func TestToolCallBudgetRefusalCanBeDistinguishedFromYield(t *testing.T) {
	n := &domain.Node{}
	n.Limits.MaxToolCalls = 1
	env := &turnEnv{n: n}
	if err := env.countTool(); err != nil {
		t.Fatal(err)
	}
	var limit toolCallBudgetError
	if err := env.countTool(); !errors.As(err, &limit) || limit != 1 {
		t.Fatalf("second call should exhaust the budget: %v", err)
	}
	env.yielded = true
	if err := env.countTool(); err == nil || errors.As(err, &limit) {
		t.Fatalf("yield should be distinguishable from budget exhaustion: %v", err)
	}
}

func TestGuidanceGrantPathPreservesPhysicalWorkdir(t *testing.T) {
	root := t.TempDir()
	const pkg = ".agents/skills/sample"
	workdir := filepath.Join(root, pkg)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "src")
	if err := os.Symlink(pkg, alias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ workdir, ref, want string }{
		{root, pkg + "/ref.md", pkg + "/ref.md"},
		{workdir, pkg + "/ref.md", "ref.md"},
		{alias, pkg + "/ref.md", "ref.md"},
		{workdir, ".agents/skills/sample-sibling/ref.md", ""},
		{root, "../ref.md", ""},
		{"", pkg + "/ref.md", ""},
	} {
		if got, ok := guidanceGrantPath(root, tc.workdir, tc.ref); got != tc.want || ok != (tc.want != "") {
			t.Errorf("path %q under %q: got %q, %t; want %q", tc.ref, tc.workdir, got, ok, tc.want)
		}
	}
}
