package trigger

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestAuthorizationProblemChecksInheritedGrantsAcrossWorkflow(t *testing.T) {
	for _, tc := range []struct {
		mode     taskauth.Mode
		defaults domain.IsolationMode
		disabled bool
		want     string
	}{
		{taskauth.Workspace, domain.IsolationKubernetes, false, tools.ErrWorkspaceShellIsolation.Error()},
		{taskauth.Workspace, domain.IsolationDocker, false, ""},
		{taskauth.Workspace, domain.IsolationProcess, true, ""},
		{taskauth.Ask, domain.IsolationProcess, false, ""},
		{"invalid", domain.IsolationDocker, false, "ask, workspace or full"},
	} {
		g := &domain.Graph{}
		g.Spec.Defaults.Isolation.Mode = tc.defaults
		g.Spec.Defaults.Tools = []domain.ToolGrant{{Name: "check.run"}}
		g.Spec.Nodes = []domain.Node{
			{ID: "entry", Tools: []domain.ToolGrant{}},
			{ID: "worker", Disabled: tc.disabled},
		}
		if got := (Manager{}).authorizationProblem(tc.mode, g); got != tc.want {
			t.Errorf("%+v: got %q", tc, got)
		}
	}
}
