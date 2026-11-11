package taskauth

import (
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

type shellAlias struct{ sampleTool }

func (*shellAlias) Capability() string { return "shell.exec" }

func TestValidateModeUsesDeclaredToolCapability(t *testing.T) {
	registry := tools.Builtin(tools.BuiltinConfig{})
	registry.MustRegister(&shellAlias{})
	for _, tc := range []struct {
		mode      Mode
		isolation domain.IsolationMode
		grant     string
		disabled  bool
		want      error
	}{
		{Workspace, domain.IsolationProcess, "check.run", false, tools.ErrWorkspaceShellIsolation},
		{Workspace, domain.IsolationKubernetes, "ch?ck.*", false, tools.ErrWorkspaceShellIsolation},
		{Workspace, domain.IsolationProcess, "s?ell.e*", false, tools.ErrWorkspaceShellIsolation},
		{Workspace, domain.IsolationProcess, "*", false, tools.ErrWorkspaceShellIsolation},
		{Workspace, domain.IsolationProcess, "test.call", false, tools.ErrWorkspaceShellIsolation},
		{Workspace, domain.IsolationKubernetes, "test.*", false, tools.ErrWorkspaceShellIsolation},
		{Workspace, domain.IsolationDocker, "check.run", false, nil},
		{Workspace, domain.IsolationProcess, "filesystem.*", false, nil},
		{Workspace, domain.IsolationProcess, "shell.missing", false, nil},
		{Workspace, domain.IsolationProcess, "check.run", true, nil},
		{Ask, domain.IsolationProcess, "check.run", false, nil},
		{Full, domain.IsolationKubernetes, "check.run", false, nil},
		{"invalid", domain.IsolationDocker, "check.run", false, ErrInvalid},
	} {
		node := domain.Node{
			Isolation: domain.IsolationSpec{Mode: tc.isolation},
			Tools:     []domain.ToolGrant{{Name: tc.grant}},
			Disabled:  tc.disabled,
		}
		if err := ValidateMode(tc.mode, []domain.Node{node}, registry); !errors.Is(err, tc.want) {
			t.Errorf("%+v: got %v, want %v", tc, err, tc.want)
		}
	}
}
