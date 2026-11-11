package engine

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestTurnPrincipalCarriesTheGraphsDeclaredChecks(t *testing.T) {
	checks := []domain.Check{{Name: "test", Command: "go test ./..."}}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-run-x", Version: 2, Labels: map[string]string{domain.LabelTaskBase: "abc123"}}, Spec: domain.GraphSpec{Checks: checks}}
	n := &domain.Node{ID: "dev", Tools: []domain.ToolGrant{{Name: "shell.exec"}}}
	env := newTurnEnv(&replica{workdir: "/ws"}, &graphRun{g: g}, n, &domain.Execution{ID: "exe_1"})
	if len(env.principal.Checks) != 1 || env.principal.Checks[0] != checks[0] || env.principal.GraphID != "task-run-x" || env.principal.BaseCommit != "abc123" {
		t.Fatalf("principal %+v", env.principal)
	}
}
