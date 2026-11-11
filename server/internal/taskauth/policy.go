// Package taskauth defines task consent without granting tools or bypassing policy.
package taskauth

import (
	"errors"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

type Mode string

const (
	Ask           Mode = "ask"
	Workspace     Mode = "workspace"
	Full          Mode = "full"
	PolicyVersion      = 1
)

var ErrInvalid = errors.New("invalid task authorization")
var ErrDenied = errors.New("task authorization denied")

func Valid(m Mode) bool { return m == Ask || m == Workspace || m == Full }

// Kubernetes currently uses process execution in the weftd pod.
func workspaceShellAllowed(isolation string) bool { return isolation == "docker" }

var builtinTools = tools.Builtin(tools.BuiltinConfig{})

// ValidateMode checks consent against the workflow's normalized agents.
func ValidateMode(mode Mode, agents []domain.Node, registry *tools.Registry) error {
	if !Valid(mode) {
		return ErrInvalid
	}
	if mode != Workspace {
		return nil
	}
	if registry == nil {
		registry = builtinTools
	}
	for _, n := range agents {
		if !n.Disabled && !workspaceShellAllowed(string(n.Isolation.Mode)) &&
			registry.DeclaresCapability(n.Tools, "shell.exec") {
			return tools.ErrWorkspaceShellIsolation
		}
	}
	return nil
}

// Approval is evaluated only after exact grants and installation restrictions.
// Mandatory grant approvals are never converted into automatic decisions.
func Approval(mode Mode, capability, isolation string, mandatory bool) (bool, error) {
	if !Valid(mode) {
		return false, ErrInvalid
	}
	if mode == Workspace && capability == "shell.exec" && !workspaceShellAllowed(isolation) {
		return false, ErrDenied // cwd/worktrees are not an OS boundary.
	}
	if mandatory {
		return true, nil
	}
	switch mode {
	case Ask:
		return true, nil
	case Workspace:
		return !(strings.HasPrefix(capability, "filesystem.") || capability == "git.read" || capability == "shell.exec"), nil
	default:
		return false, nil
	}
}

// Reduction reduces automatic execution; Ask requires fresh human decisions.
func Reduction(from, to Mode) bool {
	rank := map[Mode]int{Ask: 0, Workspace: 1, Full: 2}
	return Valid(from) && Valid(to) && rank[to] < rank[from]
}
