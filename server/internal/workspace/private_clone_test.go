package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestPrivateCheckoutFailureAndLegacyRetention(t *testing.T) {
	m, err := NewManager(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "graph"}}
	n := domain.Node{ID: "worker", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}
	g.Spec.Nodes = []domain.Node{n}
	failed := errors.New("bundle interrupted")
	m.TransferReplicaBranch = func(ctx context.Context, r ReplicaBranchTransfer) error {
		writeT(t, filepath.Join(r.Destination, "checkout", "partial"), "partial")
		return failed
	}
	if _, _, err = m.Replica(t.Context(), g, &n, 0); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	dir, err := m.ReplicaDir(g, &n, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("partial checkout installed", err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil || len(entries) != 0 {
		t.Fatal("staging survived a failed seed", entries, err)
	}
	writeT(t, filepath.Join(dir, ".git"), "gitdir: old")
	writeT(t, filepath.Join(dir, "notes"), "pending")
	if _, _, err = m.Replica(t.Context(), g, &n, 0); err == nil || !strings.Contains(err.Error(), "migration") {
		t.Fatal("legacy checkout overwritten", err)
	}
	if readT(t, filepath.Join(dir, "notes")) != "pending" {
		t.Fatal("lost pending changes")
	}
}

func TestPublicationRequiresWritablePrivateScopeAndCommit(t *testing.T) {
	m, err := NewManager(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	m.TransferReplicaBranch = func(context.Context, ReplicaBranchTransfer) error {
		t.Fatal("invalid request reached transport")
		return nil
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "graph"}}
	g.Spec.Nodes = []domain.Node{{ID: "read", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree, ReadOnly: true}}, {ID: "shared"}, {ID: "worker", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}}
	head := strings.Repeat("a", 40)
	for _, q := range []struct {
		node, head, old string
		replica         int
	}{{"read", head, "", 0}, {"shared", head, "", 0}, {"missing", head, "", 0}, {"worker", head, "", -1}, {"worker", "HEAD", "", 0}, {"worker", head, "refs/heads/main", 0}, {"worker", head, "", 0}} {
		if _, err := m.PublishReplicaBranch(t.Context(), g, q.node, q.replica, q.head, q.old); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}

func TestPrivateReplicaPathsAndCheckpoints(t *testing.T) {
	m, err := NewManager(t.TempDir(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	m.TransferReplicaBranch = func(context.Context, ReplicaBranchTransfer) error { t.Fatal("unexpected transfer"); return nil }
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "graph"}}
	n := &domain.Node{ID: "developer", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}
	g.Spec.Nodes = []domain.Node{*n}
	base, err := m.Ensure(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := m.ReplicaDir(g, n, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "-q", "-b", "private")
	gitT(t, dir, "commit", "--allow-empty", "-qm", "initial")
	writeT(t, filepath.Join(dir, "draft"), "unpublished")
	scope := CheckpointScope{GraphID: "graph", NodeID: n.ID, Replica: 1, Dir: dir, Base: base}
	cp, _, err := m.RecordCheckpoint(t.Context(), scope, CheckpointMeta{Reason: "before-edit"})
	if err != nil {
		t.Fatal(err)
	}
	all, err := m.GraphCheckpoints(t.Context(), base, "graph")
	if err != nil || len(all) != 1 || all[0].Commit != cp.Commit {
		t.Fatal(all, err)
	}
	writeT(t, filepath.Join(dir, "draft"), "modified")
	if _, err := m.RestoreCheckpoint(t.Context(), scope, cp.ID, CheckpointMeta{}); err != nil {
		t.Fatal(err)
	}
	if readT(t, filepath.Join(dir, "draft")) != "unpublished" {
		t.Fatal("private checkpoint not restored")
	}
	if err := os.Symlink(filepath.Join(m.Root, ".worktrees"), filepath.Join(m.Root, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".worktrees", ".worktrees/graph", "alias/graph", filepath.Dir(m.Root), m.Root} {
		g.Spec.Workspace.Path = p
		if _, err := m.GraphDir(g); err == nil {
			t.Fatalf("accepted reserved or external graph workspace %q", p)
		}
	}
}
