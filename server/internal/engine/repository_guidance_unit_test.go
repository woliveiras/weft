package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/tools"
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
	for _, tc := range []struct {
		workdir, ref, want string
		subdir             bool
	}{
		{root, pkg + "/ref.md", pkg + "/ref.md", false},
		{workdir, pkg + "/ref.md", "ref.md", true},
		{alias, pkg + "/ref.md", "ref.md", true},
		{filepath.Dir(workdir), pkg + "/ref.md", "sample/ref.md", true},
		{workdir, ".agents/skills/sample-sibling/ref.md", "", true},
		{workdir, "ref.md", "", true},
		{workdir, pkg + "/../../../ref.md", "", true},
		{root, "../ref.md", "", false},
		{"", pkg + "/ref.md", "", true},
		{t.TempDir(), pkg + "/ref.md", "", true},
	} {
		if got, ok := guidanceGrantPath(root, tc.workdir, pkg, tc.ref, tc.subdir); got != tc.want || ok != (tc.want != "") {
			t.Errorf("path %q under %q: got %q, %t; want %q", tc.ref, tc.workdir, got, ok, tc.want)
		}
	}

	sibling := filepath.Join(root, "sibling")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := guidanceGrantPath(root, sibling, pkg, pkg+"/ref.md", true); !ok || got != pkg+"/ref.md" {
		t.Fatalf("expected checkout-relative grant subject, got %q, %t", got, ok)
	}
	if got, ok := guidanceGrantPath(root, sibling, pkg, pkg+"/ref.md", false); ok {
		t.Fatalf("out-of-workdir reference without subdir allowed: %q", got)
	}
}

func TestSubdirGrantMatchesCanonicalRootReference(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "src")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	const pkg = ".agents/skills/review"
	path, ok := guidanceGrantPath(root, workdir, pkg, pkg+"/reference.md", true)
	grant := domain.ToolGrant{Paths: []string{".agents/**"}}
	if !ok || tools.CheckPath(grant, path) != nil || tools.CheckPath(grant, ".agents/local.md") != nil {
		t.Fatalf("grant did not match its two scoped paths: %q", path)
	}
	if _, ok := guidanceGrantPath(root, workdir, pkg, ".agents/private.md", true); ok {
		t.Fatal("path outside the discovered skill was accepted")
	}
}
