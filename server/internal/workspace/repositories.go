package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
)

// RepositoryState names the immutable Git head and the content visible to a
// check. State includes dirty and untracked files; a failed read is unknown.
type RepositoryState struct {
	Head    string `json:"head"`
	Base    string `json:"base"`
	Branch  string `json:"branch"`
	State   string `json:"state"`
	Dirty   bool   `json:"dirty"`
	Changed bool   `json:"changed"`
}

// WithRepositorySnapshot runs use with a private, inspected copy of Git
// metadata. Credentialed Git operations must never use agent-editable config.
func WithRepositorySnapshot(ctx context.Context, root string, r domain.TaskRepository, use func(RepositoryState, string) error) error {
	if !domain.ValidRepositoryAlias(r.Alias) {
		return errors.New("invalid repository alias")
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	dir := filepath.Join(real, r.Alias)
	parent, err := os.OpenRoot(real)
	if err != nil {
		return err
	}
	defer parent.Close()
	for _, path := range []string{r.Alias, filepath.Join(r.Alias, ".git")} {
		fi, err := parent.Lstat(path)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("repository %s: unexpected metadata or directory", r.Alias)
		}
	}
	meta, err := parent.OpenRoot(filepath.Join(r.Alias, ".git"))
	if err != nil {
		return err
	}
	defer meta.Close()
	snapshot, err := snapshotGitMetadata(ctx, meta)
	if err != nil {
		return err
	}
	defer os.RemoveAll(snapshot)
	m := Manager{GitBin: "git"}
	status, err := m.inspectSnapshot(ctx, dir, snapshot, ".", "status", "", false)
	if err != nil {
		return err
	}
	// inspectSnapshot validated the copied config. Retain only the object
	// format for subsequent commands; a remote or branch setting is never
	// needed to publish an explicitly supplied ref and URL.
	run := func(args ...string) (string, error) {
		argv := append([]string{"--no-replace-objects", "--git-dir=" + snapshot, "-c", "core.hooksPath=" + os.DevNull}, args...)
		cmd := exec.CommandContext(ctx, "git", argv...)
		cmd.Dir = snapshot
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0"}
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("could not read task Git state: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	for _, redirect := range []string{"info/grafts", "shallow", "commondir"} {
		if _, err := os.Lstat(filepath.Join(snapshot, redirect)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("unsupported Git metadata redirect")
		}
	}
	format, err := run("rev-parse", "--show-object-format")
	if err != nil {
		return err
	}
	config := "[core]\n bare = true\n repositoryformatversion = 0\n"
	switch strings.TrimSpace(format) {
	case "sha1":
	case "sha256":
		config = "[core]\n bare = true\n repositoryformatversion = 1\n[extensions]\n objectFormat = sha256\n"
	default:
		return errors.New("unsupported repository format")
	}
	if err = os.WriteFile(filepath.Join(snapshot, "config"), []byte(config), 0600); err != nil {
		return err
	}
	head, err := run("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return err
	}
	if _, err = run("merge-base", "--is-ancestor", r.BaseCommit, head); err != nil {
		return fmt.Errorf("repository %s: head does not descend from the reviewed base", r.Alias)
	}
	trees, err := run("rev-parse", r.BaseCommit+"^{tree}", head+"^{tree}")
	if err != nil {
		return err
	}
	pair := strings.Fields(trees)
	if len(pair) != 2 {
		return errors.New("could not identify reviewed trees")
	}
	// Anchor the final content walk beneath the original task root even if
	// the visible directory was replaced during Git inspection.
	child, err := parent.OpenRoot(r.Alias)
	if err != nil {
		return err
	}
	defer child.Close()
	digest, err := fingerprintRoot(ctx, child)
	if err != nil {
		return err
	}
	return use(RepositoryState{Head: head, Base: r.BaseCommit, Branch: status.Branch, State: digest, Dirty: status.Content != "", Changed: pair[0] != pair[1] || status.Content != ""}, snapshot)
}

func RepositoryStates(ctx context.Context, root string, repos []domain.TaskRepository) (map[string]RepositoryState, error) {
	states := make(map[string]RepositoryState, len(repos))
	for _, repo := range repos {
		err := WithRepositorySnapshot(ctx, root, repo, func(s RepositoryState, _ string) error { states[repo.Alias] = s; return nil })
		if err != nil {
			return nil, fmt.Errorf("%s: %w", repo.Alias, err)
		}
	}
	return states, nil
}

// Keep checkpoint refs and objects outside the sandbox. No checkpoint command
// reads the mutable checkout's Git config or writes through its refs.
func (m *Manager) memberCheckpointRepo(ctx context.Context, s CheckpointScope) (*checkpointRepo, error) {
	repo := s.Repository
	if !validRefPart(s.GraphID) || !domain.ValidRepositoryAlias(repo.Alias) || s.Dir != filepath.Join(s.Base, repo.Alias) || filepath.Clean(s.Base) != filepath.Join(m.Root, s.GraphID) {
		return nil, errors.New("invalid checkpoint repository binding")
	}
	vault := filepath.Join(m.Root, ".task-checkpoints", s.GraphID, repo.Alias+".git")
	var result *checkpointRepo
	err := WithRepositorySnapshot(ctx, s.Base, *repo, func(state RepositoryState, snapshot string) error {
		format := "sha1"
		if len(state.Head) == 64 {
			format = "sha256"
		}
		lock := m.lockCheckpoints(vault)
		defer lock()
		if err := os.MkdirAll(filepath.Dir(vault), 0700); err != nil {
			return err
		}
		safe := &checkpointGit{bin: m.GitBin, dir: filepath.Dir(vault)}
		if _, err := safe.run(ctx, nil, "init", "--quiet", "--template=", "--bare", "--object-format="+format, vault); err != nil {
			return err
		}
		vaultGit := &checkpointGit{bin: m.GitBin, dir: vault}
		if _, err := vaultGit.run(ctx, nil, "cat-file", "-e", state.Head+"^{commit}"); err != nil {
			if _, err = vaultGit.run(ctx, nil, "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--no-write-fetch-head", "--", snapshot, state.Head); err != nil {
				return err
			}
		}
		git := &checkpointGit{bin: m.GitBin, dir: s.Dir, env: []string{"GIT_DIR=" + vault, "GIT_WORK_TREE=" + s.Dir}}
		result = &checkpointRepo{git: git, top: s.Dir, format: format, reviewedHead: state.Head}
		return nil
	})
	return result, err
}

func (m *Manager) MemberCheckpoints(ctx context.Context, s CheckpointScope) ([]Checkpoint, error) {
	r, err := m.openCheckpointRepo(ctx, s)
	if err != nil {
		return nil, err
	}
	entries, err := r.list(ctx, CheckpointRefPrefix+s.GraphID+"/")
	newestFirst(entries)
	return entries, err
}
