package workspace

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
)

// ReplicaBranchTransfer copies one branch between independent repositories.
// An empty Commit seeds a new checkout in Destination/checkout. Publication
// supplies the exact source and previous destination (empty for creation).
type ReplicaBranchTransfer struct {
	Graph, Node                 string
	Replica                     int
	Source, Destination, Branch string
	Commit, Previous            string
}

func (m *Manager) privateClone(ctx context.Context, g *domain.Graph, n *domain.Node, replica int, base, dir, branch string) (string, string, error) {
	if !validRefPart(g.Metadata.ID) || !validRefPart(n.ID) || replica < 0 {
		return "", "", errors.New("invalid private replica identity")
	}
	volume, err := os.OpenRoot(m.Root)
	if err != nil {
		return "", "", err
	}
	defer volume.Close()
	parentPath := filepath.Join(".worktrees", g.Metadata.ID)
	for _, part := range []string{".worktrees", parentPath} {
		if err := volume.Mkdir(part, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return "", "", err
		}
		info, err := volume.Lstat(part)
		if err != nil || !info.IsDir() {
			return "", "", errors.New("invalid private checkout parent")
		}
	}
	parent, err := volume.OpenRoot(parentPath)
	if err != nil {
		return "", "", err
	}
	defer parent.Close()
	leaf := filepath.Base(dir)
	exists := func() (bool, error) {
		info, err := parent.Lstat(leaf)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			return false, errors.New("unexpected private checkout path")
		}
		git, err := parent.Lstat(filepath.Join(leaf, ".git"))
		if err != nil || !git.IsDir() {
			return false, errors.New("existing checkout requires migration to a private clone; its files were retained")
		}
		return true, nil
	}
	ok, err := exists()
	if err != nil {
		return "", "", err
	}
	if ok {
		return dir, branch, nil
	}
	stage := ".seed-" + rand.Text()
	if err := parent.Mkdir(stage, 0700); err != nil {
		return "", "", err
	}
	defer parent.RemoveAll(stage)
	transfer := ReplicaBranchTransfer{Graph: g.Metadata.ID, Node: n.ID, Replica: replica, Source: base, Destination: filepath.Join(filepath.Dir(dir), stage), Branch: branch}
	if err := m.TransferReplicaBranch(ctx, transfer); err != nil {
		return "", "", fmt.Errorf("seed private replica: %w", err)
	}
	if err := parent.Rename(filepath.Join(stage, "checkout"), leaf); err != nil {
		// Another controller may have completed the same replica concurrently.
		if ok, check := exists(); check != nil || !ok {
			return "", "", errors.Join(err, check)
		}
	}
	return dir, branch, nil
}

var ErrBranchUnavailable = errors.New("replica branch unavailable")

var ErrBranchConflict = errors.New("replica branch publication conflict")

// PublishReplicaBranch accepts commit IDs only. Destination paths, repository
// names and refs are derived exclusively from the stored graph and replica.
func (m *Manager) PublishReplicaBranch(ctx context.Context, g *domain.Graph, node string, replica int, commit, previous string) (string, error) {
	n := g.Node(node)
	if m.TransferReplicaBranch == nil || n == nil || n.Workspace.Mode != domain.WorkspaceWorktree || n.Workspace.ReadOnly || replica < 0 || !validRefPart(node) || !validRefPart(g.Metadata.ID) {
		return "", fmt.Errorf("%w: private writable replica required", ErrBranchUnavailable)
	}
	if !domain.ValidFullCommit(commit) || previous != "" && (!domain.ValidFullCommit(previous) || len(previous) != len(commit)) {
		return "", errors.New("expected full commit IDs")
	}
	base, err := m.GraphDir(g)
	if err != nil {
		return "", err
	}
	dir, err := m.ReplicaDir(g, n, replica)
	if err != nil {
		return "", err
	}
	meta, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect private replica: %w", err)
	}
	if err != nil || !meta.IsDir() {
		return "", fmt.Errorf("%w: private clone unavailable", ErrBranchUnavailable)
	}
	branch := replicaBranch(g, node, replica)
	req := ReplicaBranchTransfer{Graph: g.Metadata.ID, Node: node, Replica: replica, Source: dir, Destination: base, Branch: branch, Commit: commit, Previous: previous}
	return branch, m.TransferReplicaBranch(ctx, req)
}

// Private repositories keep their own checkpoint refs. Enumerate retained
// replicas as well as running ones, confining metadata access to the claim.
func (m *Manager) privateCheckpoints(ctx context.Context, graph string) ([]Checkpoint, error) {
	volume, err := os.OpenRoot(m.Root)
	if err != nil {
		return nil, err
	}
	defer volume.Close()
	parent, err := volume.OpenRoot(filepath.Join(".worktrees", graph))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	entries, err := fs.ReadDir(parent.FS(), ".")
	if err != nil {
		return nil, err
	}
	var list []Checkpoint
	for _, entry := range entries {
		name := entry.Name()
		i := strings.LastIndexByte(name, '-')
		if !entry.IsDir() || i <= 0 || !validRefPart(name[:i]) {
			continue
		}
		index, err := strconv.Atoi(name[i+1:])
		if err != nil || index < 0 || strconv.Itoa(index) != name[i+1:] {
			continue
		}
		path := filepath.Join(name, ".git")
		info, err := parent.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			continue
		} // Linked worktrees store refs in the graph repository.
		items, err := m.privateCheckpointRefs(ctx, parent, path, CheckpointScope{GraphID: graph, NodeID: name[:i], Replica: index})
		if err != nil {
			return nil, err
		}
		list = append(list, items...)
	}
	return list, nil
}

func (m *Manager) privateCheckpointRefs(ctx context.Context, parent *os.Root, path string, scope CheckpointScope) ([]Checkpoint, error) {
	git, err := parent.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer git.Close()
	snapshot, err := snapshotGitMetadata(ctx, git)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(snapshot)
	if _, err := os.Lstat(filepath.Join(snapshot, "commondir")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("unexpected common Git directory")
	}
	if err := m.validateSnapshotConfig(ctx, snapshot); err != nil {
		return nil, err
	}
	repo := checkpointRepo{git: &checkpointGit{bin: m.GitBin, dir: snapshot, env: []string{"GIT_DIR=" + snapshot, "GIT_NO_REPLACE_OBJECTS=1"}}}
	return repo.list(ctx, scopeRefPrefix(scope))
}
