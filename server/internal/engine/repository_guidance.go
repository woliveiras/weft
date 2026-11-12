package engine

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func guidanceCheckoutRoot(workspaces *workspace.Manager, g *domain.Graph, n *domain.Node, workdir string) (string, error) {
	if n.Workspace.Mode == domain.WorkspaceWorktree || n.Workspace.Subdir == "" {
		return workdir, nil
	}
	root, err := workspaces.GraphDir(g)
	if err != nil {
		return "", instructions.ErrWorkspaceUnavailable
	}
	return root, nil
}

type turnGuidance struct {
	*instructions.Session
	env *turnEnv
}

func (g *turnGuidance) Load(ctx context.Context, skill, ref string) (string, error) {
	g.env.toolMu.Lock()
	defer g.env.toolMu.Unlock()
	ctx, done, err := g.env.controlled(ctx)
	defer done()
	if err != nil {
		return "", err
	}
	if err := g.env.countTool(); err != nil {
		return err.Error(), nil
	}
	return g.Session.Load(ctx, skill, ref)
}

func (t *turnEnv) readGuidanceReference(ctx context.Context, path string, read func() (string, string)) (string, string, error) {
	gateway, ok := t.gr.e.opt.Tools.(interface {
		ReadGuidanceReference(context.Context, *tools.Principal, string, func() string) (*tools.Result, error)
	})
	if !ok {
		return "", "permission_denied", nil
	}
	if t.gr.e.opt.Tools.NeedsApproval(t.principal, "filesystem.read") {
		t.r.setState(domain.ReplicaAwaitingApproval, "waiting for approval: filesystem.read")
		defer t.r.setState(domain.ReplicaRunning, "")
	}
	// Session supplies the validated checkout-relative path. Ordinary tools
	// still resolve their paths from the principal's working directory.
	var content, reason string
	result, err := gateway.ReadGuidanceReference(ctx, t.principal, path, func() string {
		content, reason = read()
		return reason
	})
	switch {
	case errors.Is(err, tools.ErrDenied), errors.Is(err, tools.ErrUnknownTool):
		return "", "permission_denied", nil
	case errors.Is(err, tools.ErrRejected):
		return "", "approval_rejected", nil
	case errors.Is(err, tools.ErrRateLimited):
		return "", "rate_limited", nil
	case err != nil:
		return "", "", err
	case result.IsError && reason == "":
		return "", "unreadable", nil
	default:
		return content, reason, nil
	}
}
