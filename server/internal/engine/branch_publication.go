package engine

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// The caller supplies a server-resolved graph. Agents use their turn snapshot.
func (e *Engine) PublishReplicaBranch(ctx context.Context, g *domain.Graph, node string, replica int, req workspace.BranchPublication, actor, execution string) (workspace.PublishedBranch, error) {
	ws := e.opt.Workspaces
	if ws == nil {
		return workspace.PublishedBranch{}, workspace.ErrBranchUnavailable
	}
	branch, err := ws.PublishReplicaBranch(ctx, g, node, replica, req.Commit, req.Previous)
	outcome := "published"
	switch {
	case errors.Is(err, workspace.ErrBranchConflict):
		outcome = "conflict"
	case errors.Is(err, workspace.ErrBranchUnavailable):
		outcome = "unavailable"
	case err != nil:
		outcome = "error"
	}
	ev := domain.NewEvent(domain.EvBranchPublication, g.Metadata.ID, map[string]any{"branch": branch, "commit": req.Commit, "previous": req.Previous, "outcome": outcome}).WithNode(node, replica).WithActor(actor).WithTrace(observability.TraceID(ctx))
	if execution != "" {
		ev.WithExecution(execution)
	}
	e.bus.Publish(context.WithoutCancel(ctx), ev)
	if err != nil {
		return workspace.PublishedBranch{}, err
	}
	return workspace.PublishedBranch{Branch: branch, Commit: req.Commit}, nil
}
