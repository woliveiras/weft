package taskrun

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (m Manager) cleanupTarget(ctx context.Context, id, node string, replica int, member ...string) (*projectconfig.Project, string, error) {
	v, err := m.View(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if v.State != "cancelled" || (v.Task.ProjectID == "" && len(v.Task.Repositories) == 0) || m.Workspaces == nil {
		return nil, "", fmt.Errorf("%w: stop a registered-project task and wait for cancellation before cleanup", storage.ErrConflict)
	}
	g, record, err := m.Engine.GetGraph(ctx, v.Task.ExecutionGraphID, 0)
	partial := len(v.Task.Repositories) > 0 && v.Task.Status == "cancelled" && v.Task.ExecutionGraphID == ExecutionID(v.Task.ID) && (errors.Is(err, storage.ErrNotFound) || errors.Is(err, engine.ErrNotFound))
	if err != nil && !partial {
		return nil, "", err
	}
	if !partial && record.State != domain.GraphStopped {
		return nil, "", storage.ErrConflict
	}
	root, err := filepath.EvalSymlinks(m.Workspaces.Root)
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(root, v.Task.ExecutionGraphID)
	projectID := v.Task.ProjectID
	if len(v.Task.Repositories) > 0 {
		if len(member) != 1 || node != "" || replica != 0 {
			return nil, "", fmt.Errorf("%w: select a task repository for cleanup", storage.ErrConflict)
		}
		for _, repo := range v.Task.Repositories {
			if member[0] == repo.Alias {
				projectID = repo.ProjectID
				dir = filepath.Join(dir, repo.Alias)
				break
			}
		}
		if projectID == "" {
			return nil, "", storage.ErrNotFound
		}
	} else if len(member) > 0 {
		return nil, "", storage.ErrNotFound
	}
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
	p, err := (projectconfig.Manager{Store: m.Store}).Get(ctx, projectID)
	return p, dir, err
}

func (m Manager) PreviewCleanup(ctx context.Context, id, node string, replica int, member ...string) (*projectconfig.CleanupPreview, error) {
	p, dir, err := m.cleanupTarget(ctx, id, node, replica, member...)
	if err != nil {
		return nil, err
	}
	return (projectconfig.Manager{Store: m.Store}).PreviewTaskCleanup(ctx, p, ExecutionID(id), dir, member...)
}

func (m Manager) Cleanup(ctx context.Context, id, node string, replica int, fingerprint, actor string, member ...string) error {
	p, dir, err := m.cleanupTarget(ctx, id, node, replica, member...)
	if err != nil {
		return err
	}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.workspace_cleanup_requested", ExecutionID(id), map[string]any{"taskId": id, "repositories": member, "nodeId": node, "replica": replica}).WithActor(actor)); err != nil {
		return err
	}
	if err = (projectconfig.Manager{Store: m.Store}).RemoveReviewedTaskCheckout(ctx, p, ExecutionID(id), dir, fingerprint, member...); err != nil {
		return err
	}
	return m.Store.AppendEvent(ctx, domain.NewEvent("task.workspace_cleanup_completed", ExecutionID(id), map[string]any{"taskId": id, "repositories": member, "nodeId": node, "replica": replica}).WithActor(actor))
}
