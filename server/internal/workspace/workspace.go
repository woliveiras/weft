// Package workspace manages the directories agents work in.
//
// A graph has one workspace (usually a git repository). By default every
// replica of every node shares it - the simplest model, and the right one
// for pipelines whose stages touch different files. A node can instead ask
// for a git worktree per replica: each replica then has a full checkout on
// its own branch (weft/<graph>/<node>-<n>), so a cluster of eight
// developers is eight genuinely parallel checkouts rather than eight agents
// overwriting each other in one folder. Merging those branches is git's job,
// or an integrator agent's; the platform never merges.
//
// Worktrees live outside the graph workspace (<root>/.worktrees/<graph>/...)
// so they never appear as untracked files in the repository they belong to.
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
)

type Manager struct {
	Root          string // container/host path the platform uses
	HostRoot      string // the same directory as the Docker daemon sees it
	AllowAbsolute bool
	GitBin        string
	// MaxCheckpoints bounds the checkpoints kept per replica (default
	// DefaultMaxCheckpoints).
	MaxCheckpoints int

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewManager(root, hostRoot string, allowAbsolute bool) (*Manager, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	if hostRoot == "" {
		hostRoot = abs
	}
	return &Manager{Root: abs, HostRoot: hostRoot, AllowAbsolute: allowAbsolute, GitBin: "git", locks: map[string]*sync.Mutex{}}, nil
}

// GraphDir returns the workspace directory of a graph.
func (m *Manager) GraphDir(g *domain.Graph) (string, error) {
	p := g.Spec.Workspace.Path
	switch {
	case p == "":
		return filepath.Join(m.Root, g.Metadata.ID), nil
	case filepath.IsAbs(p):
		if !m.AllowAbsolute {
			return "", fmt.Errorf("absolute workspace paths are disabled (workspaces.allowAbsolute)")
		}
		return filepath.Clean(p), nil
	default:
		clean := filepath.Clean(filepath.Join(m.Root, p))
		if !strings.HasPrefix(clean, m.Root+string(filepath.Separator)) {
			return "", fmt.Errorf("workspace path escapes the workspace root")
		}
		return clean, nil
	}
}

// HostPath maps a platform path under Root to the daemon's view (for bind mounts).
func (m *Manager) HostPath(p string) string {
	rel, err := filepath.Rel(m.Root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return filepath.Join(m.HostRoot, rel)
}

// Ensure creates the graph workspace, initialising or cloning a repository
// when the graph asks for one.
func (m *Manager) Ensure(ctx context.Context, g *domain.Graph) (string, error) {
	dir, err := m.GraphDir(g)
	if err != nil {
		return "", err
	}
	lock := m.lock(dir)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	needGit := g.Spec.Workspace.Git || g.Spec.Workspace.Repo != ""
	for _, n := range g.Spec.Nodes {
		if n.Workspace.Mode == domain.WorkspaceWorktree {
			needGit = true
		}
	}
	if !needGit || isRepo(dir) {
		return dir, nil
	}
	if repo := g.Spec.Workspace.Repo; repo != "" {
		entries, _ := os.ReadDir(dir)
		if len(entries) == 0 {
			// Graphs saved before repository URLs were validated may still
			// hold a local path; never clone one (see graph.RepoKind).
			if graph.RepoKind(repo) == graph.RepoInvalid {
				return "", fmt.Errorf("clone %s: not a remote repository URL (use https://, ssh:// or user@host:path)", repo)
			}
			// Only network transports, also for submodules and redirects;
			// "--" keeps the URL from being read as an option.
			if _, err := m.git(ctx, filepath.Dir(dir),
				"-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.http.allow=always",
				"-c", "protocol.ssh.allow=always", "-c", "protocol.git.allow=always",
				"clone", "--", repo, dir); err != nil {
				return "", fmt.Errorf("clone %s: %w", repo, err)
			}
			return dir, nil
		}
	}
	if _, err := m.git(ctx, dir, "init", "-q", "-b", "main"); err != nil {
		return "", err
	}
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".weft/\n"), 0o644)
	if _, err := m.git(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := m.git(ctx, dir, "commit", "-q", "--allow-empty", "-m", "weft: initialise workspace"); err != nil {
		return "", err
	}
	return dir, nil
}

// Replica returns the working directory (and branch, for worktrees) of one
// replica, creating its worktree on first use. A restart reuses it.
func (m *Manager) Replica(ctx context.Context, g *domain.Graph, n *domain.Node, replica int) (dir, branch string, err error) {
	base, err := m.Ensure(ctx, g)
	if err != nil {
		return "", "", err
	}
	if n.Workspace.Mode != domain.WorkspaceWorktree {
		dir = base
		if n.Workspace.Subdir != "" {
			dir = filepath.Join(base, filepath.Clean("/" + n.Workspace.Subdir)[1:])
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", "", err
			}
		}
		return dir, "", nil
	}
	lock := m.lock(base)
	lock.Lock()
	defer lock.Unlock()
	dir = filepath.Join(m.Root, ".worktrees", g.Metadata.ID, fmt.Sprintf("%s-%d", n.ID, replica))
	branch = replicaBranch(g, n.ID, replica)
	if isRepo(dir) {
		return dir, branch, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", "", err
	}
	// Reuse the branch if it survived a previous release; otherwise create it.
	if _, err := m.git(ctx, base, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err = m.git(ctx, base, "worktree", "add", dir, branch)
		if err != nil {
			return "", "", err
		}
	} else if _, err := m.git(ctx, base, "worktree", "add", "-b", branch, dir); err != nil {
		return "", "", err
	}
	return dir, branch, nil
}

// ReleaseWorktree removes a replica's checkout without forcing: a worktree
// with uncommitted changes is kept (git refuses), and the branch is never
// deleted, so committed work always survives.
func (m *Manager) ReleaseWorktree(ctx context.Context, g *domain.Graph, nodeID string, replica int) error {
	base, err := m.GraphDir(g)
	if err != nil {
		return err
	}
	dir := filepath.Join(m.Root, ".worktrees", g.Metadata.ID, fmt.Sprintf("%s-%d", nodeID, replica))
	if !isRepo(dir) {
		return nil
	}
	_, err = m.git(ctx, base, "worktree", "remove", dir)
	return err
}

// Scratch is a replica's private folder inside the workspace for notes and
// intermediate files for one replica. It is git-ignored.
func Scratch(workdir, node string, replica int) string {
	return filepath.Join(workdir, ".weft", node, fmt.Sprintf("r%d", replica))
}

func (m *Manager) lock(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	return l
}

func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func (m *Manager) git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Platform git operations never run repository hooks (a worktree add
	// would otherwise run post-checkout from content agents can influence).
	args = append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, m.GitBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=weft", "GIT_AUTHOR_EMAIL=weft@localhost",
		"GIT_COMMITTER_NAME=weft", "GIT_COMMITTER_EMAIL=weft@localhost",
		"GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// Task roots already own weft/<graph>; Git cannot place a ref below that ref.
// Ordinary workflow branches retain their existing names.
func replicaBranch(g *domain.Graph, nodeID string, replica int) string {
	prefix := "weft"
	if g.Metadata.Labels["weft.task"] != "" {
		prefix = "weft-replicas"
	}
	return fmt.Sprintf("%s/%s/%s-%d", prefix, g.Metadata.ID, nodeID, replica)
}
