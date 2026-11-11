package taskrun

import (
	"context"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// Selection validates the saved workflow; it never downgrades isolation or
// changes its tools. A different environment requires a reviewed workflow edit.
func environmentChoice(d *taskconfig.Draft, g *domain.Graph, native bool) ([]string, bool) {
	blockers := []string{}
	sandbox := false
	if d.ExecutionMode == "local" {
		if !native {
			blockers = append(blockers, "Local worktree execution requires the native Weft installation on this computer.")
		}
		if d.ProjectID == "" {
			blockers = append(blockers, "Select a registered project for local worktree execution.")
		}
	}
	for _, n := range g.Spec.Nodes {
		if n.Disabled {
			continue
		}
		if n.Isolation.Mode == domain.IsolationDocker {
			sandbox = true
		}
		if d.ExecutionMode == "local" && n.Isolation.Mode != domain.IsolationProcess {
			blockers = append(blockers, "Local selection does not downgrade workflow isolation. Review the workflow's agent environments.")
			break
		}
		if d.ExecutionMode == "sandbox" && n.Isolation.Mode != domain.IsolationDocker {
			blockers = append(blockers, "Sandbox selection requires Docker isolation for every enabled agent. Review the workflow's agent environments.")
			break
		}
	}
	return blockers, sandbox || d.ExecutionMode == "sandbox"
}

func (m Manager) checkEnvironment(ctx context.Context, d *taskconfig.Draft, g *domain.Graph) []string {
	blockers, sandbox := environmentChoice(d, g, m.NativeProfile)
	if sandbox {
		if m.CheckSandbox == nil {
			blockers = append(blockers, "Sandbox availability cannot be verified on this installation.")
		} else if err := m.CheckSandbox(ctx); err != nil {
			blockers = append(blockers, "The configured sandbox daemon is unavailable. Start the configured Docker/Colima runtime and review again; Weft will not fall back to local execution.")
		}
	}
	return blockers
}
