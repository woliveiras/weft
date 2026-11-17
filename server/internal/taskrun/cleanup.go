package taskrun

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (m Manager) cleanupTarget(ctx context.Context, id, node string, replica int) (*projectconfig.Project, string, error) {
	v, err := m.View(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if v.State != "cancelled" || v.Task.ProjectID == "" || m.Workspaces == nil {
		return nil, "", fmt.Errorf("%w: stop a registered-project task and wait for cancellation before cleanup", storage.ErrConflict)
	}
	g, record, err := m.Engine.GetGraph(ctx, v.Task.ExecutionGraphID, 0)
	if err != nil {
		return nil, "", err
	}
	if record.State != domain.GraphStopped {
		return nil, "", storage.ErrConflict
	}
	root, err := filepath.EvalSymlinks(m.Workspaces.Root)
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(root, g.Metadata.ID)
	if node != "" {
		found := false
		for _, n := range g.Spec.Nodes {
			if n.ID == node && n.Workspace.Mode == domain.WorkspaceWorktree {
				found = true
			}
		}
		if !found || replica < 0 {
			return nil, "", fmt.Errorf("%w: unknown agent worktree", storage.ErrNotFound)
		}
		dir = filepath.Join(root, ".worktrees", g.Metadata.ID, fmt.Sprintf("%s-%d", node, replica))
	} else if replica != 0 {
		return nil, "", storage.ErrNotFound
	}
	p, err := (projectconfig.Manager{Store: m.Store}).Get(ctx, v.Task.ProjectID)
	return p, dir, err
}

func (m Manager) PreviewCleanup(ctx context.Context, id, node string, replica int) (*projectconfig.CleanupPreview, error) {
	p, dir, err := m.cleanupTarget(ctx, id, node, replica)
	if err != nil {
		return nil, err
	}
	return projectconfig.PreviewCleanup(ctx, p, dir)
}

func (m Manager) Cleanup(ctx context.Context, id, node string, replica int, fingerprint, actor string) error {
	p, dir, err := m.cleanupTarget(ctx, id, node, replica)
	if err != nil {
		return err
	}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.workspace_cleanup_requested", ExecutionID(id), map[string]any{"taskId": id, "nodeId": node, "replica": replica}).WithActor(actor)); err != nil {
		return err
	}
	if err = projectconfig.RemoveReviewedWorktree(ctx, p, dir, fingerprint); err != nil {
		return err
	}
	return m.Store.AppendEvent(ctx, domain.NewEvent("task.workspace_cleanup_completed", ExecutionID(id), map[string]any{"taskId": id, "nodeId": node, "replica": replica}).WithActor(actor))
}
