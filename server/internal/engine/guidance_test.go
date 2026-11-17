package engine

import (
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestSmallModelGuidanceShowsOnlyGrantedToolShapes(t *testing.T) {
	g := smallModelGuidance([]llm.ToolSchema{{Name: "filesystem.read"}, {Name: "filesystem.edit"}})
	for _, want := range []string{"## How to call tools", "exactly one tool per reply", "never guess a file name", `filesystem.read {"path":"<file>"`, `filesystem.edit {"path":"<file>"`, "plan.update", "finish {"} {
		if !strings.Contains(g, want) {
			t.Fatalf("missing %q in\n%s", want, g)
		}
	}
	if strings.Contains(g, "shell.exec") || strings.Contains(g, "apply_patch") {
		t.Fatalf("shapes of tools that were not granted:\n%s", g)
	}
	// Examples must not name real-looking files: models copy them verbatim.
	if strings.Contains(g, ".go") {
		t.Fatalf("examples name a concrete file:\n%s", g)
	}
}
