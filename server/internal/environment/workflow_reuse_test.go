package environment

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestWorkflowImageReuseRejectsWorkspaceDerivedImages(t *testing.T) {
	for _, change := range []func(*domain.Graph){
		func(g *domain.Graph) { g.Spec.Workspace.Repo = "https://example.test/repo.git" },
		func(g *domain.Graph) { g.Spec.Workspace.Path = "/private/work" },
		func(g *domain.Graph) { g.Spec.Environment.Context = "." },
		func(g *domain.Graph) { g.Spec.Environment.Setup = []string{"touch output"} },
		func(g *domain.Graph) { g.Spec.Environment.Origin = &domain.EnvironmentOrigin{Source: "agent"} },
		func(g *domain.Graph) { g.Spec.Environment = nil },
	} {
		g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22"})
		if !workflowImageReusable(g) {
			t.Fatal("plain image rejected")
		}
		change(g)
		if workflowImageReusable(g) {
			t.Fatalf("unsafe reuse: %+v", g.Spec)
		}
	}
}
