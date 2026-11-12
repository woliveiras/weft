package engine

import (
	"errors"
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
