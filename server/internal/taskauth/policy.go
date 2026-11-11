// Package taskauth defines task consent without granting tools or bypassing policy.
package taskauth

import (
	"errors"
	"strings"
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

// Approval is evaluated only after exact grants and installation restrictions.
// Mandatory grant approvals are never converted into automatic decisions.
func Approval(mode Mode, capability, isolation string, mandatory bool) (bool, error) {
	if !Valid(mode) {
		return false, ErrInvalid
	}
	if mode == Workspace && capability == "shell.exec" && isolation != "docker" && isolation != "kubernetes" {
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
