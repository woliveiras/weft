package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func workspaceTargets(ctx context.Context, store storage.Store, ws *workspace.Manager) ([]tools.WorkspaceTarget, error) {
	graphs, err := store.ListGraphs(ctx)
	if err != nil {
		return nil, err
	}
	targets := []tools.WorkspaceTarget{}
	add := func(target tools.WorkspaceTarget, absolute bool) error {
		// Resolve symlinks within the configured root before inspecting. Absolute
		// graph workspaces have already been explicitly enabled by the installation.
		jail := ws.Root
		if ws.AllowAbsolute && absolute {
			jail = target.Dir
		}
		resolved, _, err := tools.ResolvePath(jail, target.Dir)
		if err != nil {
			return err
		}
		info, err := os.Stat(resolved)
		if os.IsNotExist(err) {
			target.Dir = resolved
			targets = append(targets, target)
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("workspace is not a directory")
		}
		target.Dir = resolved
		targets = append(targets, target)
		return nil
	}
	for _, g := range graphs {
		dir, err := ws.GraphDir(g)
		if err != nil {
			return nil, err
		}
		if err := add(tools.WorkspaceTarget{GraphID: g.Metadata.ID, Dir: dir}, filepath.IsAbs(g.Spec.Workspace.Path)); err != nil {
			return nil, err
		}
		replicas, err := store.ListReplicaStatus(ctx, g.Metadata.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range replicas {
			for _, n := range g.Spec.Nodes {
				if n.ID != r.NodeID || n.Workspace.Mode != domain.WorkspaceWorktree {
					continue
				}
				dir := filepath.Join(ws.Root, ".worktrees", g.Metadata.ID, fmt.Sprintf("%s-%d", n.ID, r.Replica))
				if err := add(tools.WorkspaceTarget{GraphID: g.Metadata.ID, NodeID: n.ID, Replica: r.Replica, Dir: dir}, false); err != nil {
					return nil, err
				}
			}
		}
	}
	return targets, nil
}
