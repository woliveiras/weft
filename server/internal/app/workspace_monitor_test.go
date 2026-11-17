package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestWorkspaceMonitorDiscoversStoppedGraphAndWorktrees(t *testing.T) {
	ctx := context.Background()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ws, err := workspace.NewManager(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "project"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}}}}
	if _, err := store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(ws.Root, ".worktrees", "project", "worker-0")
	if err := os.MkdirAll(worktree, 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.PutReplicaStatus(ctx, &domain.ReplicaStatus{GraphID: "project", NodeID: "worker", Replica: 0, Workdir: "/untrusted/status/path"}); err != nil {
		t.Fatal(err)
	}
	targets, err := workspaceTargets(ctx, store, ws)
	if err != nil || len(targets) != 2 {
		t.Fatalf("%+v %v", targets, err)
	}
	canonicalWorktree, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if targets[1].Dir != canonicalWorktree || targets[1].NodeID != "worker" {
		t.Fatalf("%+v", targets)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "project")); !os.IsNotExist(err) {
		t.Fatal("monitor created a workspace")
	}
	// No runtime status path is trusted, even while absolute graph paths are enabled.
	ws.AllowAbsolute = true
	if err := os.Symlink(t.TempDir(), filepath.Join(ws.Root, "project")); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceTargets(ctx, store, ws); err == nil {
		t.Fatal("relative workspace symlink escape accepted")
	}
}
