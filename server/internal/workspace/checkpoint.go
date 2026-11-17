package workspace

// Checkpoints (NAT-07) record a replica's working tree on hidden Git refs
// before the agent edits it, so a person (or the agent, with an explicit
// grant) can return to the state before a bad round.
//
// A checkpoint is a commit on refs/weft/checkpoints/<graph>/<node>/r<n>/<seq>
// whose tree is HEAD with the replica's directory replaced by its current
// content (tracked and untracked files; ignored files, nested repositories
// and the replica scratch folder .weft/ are excluded). Recording never
// touches the index, HEAD or the branch: the tree is built in a temporary
// index (GIT_INDEX_FILE) and the commit is made with commit-tree.
//
// The platform runs Git on content agents can influence, so only plumbing
// that runs no filters, hooks or external programs is used: file contents are
// read by Go through os.Root (never following a link out of the repository)
// and hashed here; Git only stores objects, builds trees and moves refs, with
// hooks, fsmonitor, signing, automatic maintenance and lazy fetches disabled
// and no system or global configuration. Restores write files through
// os.Root as well, so a planted symlink cannot redirect them.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

// CheckpointRefPrefix is the hidden ref namespace of checkpoints.
const CheckpointRefPrefix = "refs/weft/checkpoints/"

// DefaultMaxCheckpoints bounds the checkpoints kept per replica scope.
const DefaultMaxCheckpoints = 20

// Checkpoint reasons.
const (
	CheckpointBeforeEdit    = "before_edit"
	CheckpointBeforeRestore = "before_restore"
)

const (
	checkpointFileLimit         = 50000
	checkpointByteLimit   int64 = 512 << 20
	checkpointOutputLimit       = 64 << 20
	checkpointTimeout           = 2 * time.Minute
	scratchDir                  = ".weft"
)

var (
	// ErrNotRepository: the replica directory is not a Git repository (or a
	// subdirectory of the graph's repository), so it cannot be checkpointed.
	ErrNotRepository = errors.New("workspace is not a Git repository")
	// ErrCheckpointNotFound: no checkpoint matches the request.
	ErrCheckpointNotFound = errors.New("checkpoint not found")
	// ErrCheckpointTooLarge: the scope exceeds the file or byte limit.
	ErrCheckpointTooLarge = errors.New("workspace too large to checkpoint")
)

// CheckpointScope is one replica's working directory. The repository top
// level must be Dir itself (a worktree replica) or Base (the graph workspace
// that Dir is, or is a subdirectory of); an enclosing repository is never
// checkpointed.
type CheckpointScope struct {
	GraphID string
	NodeID  string
	Replica int
	Dir     string
	Base    string
}

// CheckpointMeta describes why a checkpoint is recorded.
type CheckpointMeta struct {
	ExecutionID string
	Reason      string
	Tool        string
}

// Checkpoint is one recorded state of a replica scope.
type Checkpoint struct {
	ID          string    `json:"id"`
	Ref         string    `json:"ref"`
	Commit      string    `json:"commit"`
	Tree        string    `json:"tree"`
	NodeID      string    `json:"nodeId"`
	Replica     int       `json:"replica"`
	Sequence    int       `json:"sequence"`
	ExecutionID string    `json:"executionId,omitempty"`
	Reason      string    `json:"reason"`
	Tool        string    `json:"tool,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RestoreResult reports a restore. Saved holds the state just before the
// restore, so a restore can itself be undone.
type RestoreResult struct {
	Restored Checkpoint `json:"restored"`
	Saved    Checkpoint `json:"saved"`
	Written  int        `json:"written"`
	Removed  int        `json:"removed"`
}

// CheckpointID is the identifier of a checkpoint inside its graph.
func CheckpointID(node string, replica, seq int) string {
	return fmt.Sprintf("%s/r%d/%d", node, replica, seq)
}

// ParseCheckpointID splits an identifier made by CheckpointID.
func ParseCheckpointID(id string) (node string, replica, seq int, err error) {
	bad := fmt.Errorf("invalid checkpoint id %q", id)
	parts := strings.Split(id, "/")
	if len(parts) != 3 || !validRefPart(parts[0]) || !strings.HasPrefix(parts[1], "r") {
		return "", 0, 0, bad
	}
	replica, err = strconv.Atoi(parts[1][1:])
	if err != nil || replica < 0 || strconv.Itoa(replica) != parts[1][1:] {
		return "", 0, 0, bad
	}
	seq, err = strconv.Atoi(parts[2])
	if err != nil || seq <= 0 || strconv.Itoa(seq) != parts[2] {
		return "", 0, 0, bad
	}
	return parts[0], replica, seq, nil
}

// validRefPart accepts graph and node identifiers (graph.idRe's alphabet).
func validRefPart(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func scopeRefPrefix(s CheckpointScope) string {
	return fmt.Sprintf("%s%s/%s/r%d/", CheckpointRefPrefix, s.GraphID, s.NodeID, s.Replica)
}

func (m *Manager) maxCheckpoints() int {
	if m.MaxCheckpoints > 0 {
		return m.MaxCheckpoints
	}
	return DefaultMaxCheckpoints
}

// ReplicaDir returns a replica's working directory without creating it
// (Replica creates worktrees on first use).
func (m *Manager) ReplicaDir(g *domain.Graph, n *domain.Node, replica int) (string, error) {
	base, err := m.GraphDir(g)
	if err != nil {
		return "", err
	}
	switch {
	case n.Workspace.Mode == domain.WorkspaceWorktree:
		return filepath.Join(m.Root, ".worktrees", g.Metadata.ID, fmt.Sprintf("%s-%d", n.ID, replica)), nil
	case n.Workspace.Subdir != "":
		return filepath.Join(base, filepath.Clean("/" + n.Workspace.Subdir)[1:]), nil
	default:
		return base, nil
	}
}

// RecordCheckpoint records the scope's current state unless it equals the
// newest checkpoint of the scope. created reports whether a new checkpoint
// was written; otherwise the newest one is returned.
func (m *Manager) RecordCheckpoint(ctx context.Context, s CheckpointScope, meta CheckpointMeta) (cp *Checkpoint, created bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, checkpointTimeout)
	defer cancel()
	r, err := m.openCheckpointRepo(ctx, s)
	if err != nil {
		return nil, false, err
	}
	unlock := m.lockCheckpoints(r.top)
	defer unlock()
	cp, created, _, err = r.record(ctx, s, meta, m.maxCheckpoints())
	return cp, created, err
}

// Checkpoints lists the checkpoints of one scope, newest first.
func (m *Manager) Checkpoints(ctx context.Context, s CheckpointScope) ([]Checkpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, checkpointTimeout)
	defer cancel()
	r, err := m.openCheckpointRepo(ctx, s)
	if err != nil {
		return nil, err
	}
	list, err := r.list(ctx, scopeRefPrefix(s))
	if err != nil {
		return nil, err
	}
	newestFirst(list)
	return list, nil
}

// GraphCheckpoints lists every checkpoint of a graph recorded in the
// repository at base (worktree replicas share its refs), newest first. A
// base that is not a repository has none.
func (m *Manager) GraphCheckpoints(ctx context.Context, base, graphID string) ([]Checkpoint, error) {
	if !validRefPart(graphID) {
		return nil, fmt.Errorf("invalid graph id")
	}
	ctx, cancel := context.WithTimeout(ctx, checkpointTimeout)
	defer cancel()
	dir, err := filepath.EvalSymlinks(base)
	if errors.Is(err, fs.ErrNotExist) {
		return []Checkpoint{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !hasGitEntry(dir) {
		return []Checkpoint{}, nil
	}
	r := &checkpointRepo{git: &checkpointGit{bin: m.GitBin, dir: dir}, top: dir}
	list, err := r.list(ctx, CheckpointRefPrefix+graphID+"/")
	if err != nil {
		return nil, err
	}
	newestFirst(list)
	return list, nil
}

// RestoreCheckpoint makes the scope's files equal to a checkpoint: id, or
// the newest checkpoint recorded before an edit when id is empty. The state
// before the restore is recorded first. Ignored files, nested repositories,
// .weft/, the index, HEAD and the branch are left untouched.
func (m *Manager) RestoreCheckpoint(ctx context.Context, s CheckpointScope, id string, meta CheckpointMeta) (*RestoreResult, error) {
	ctx, cancel := context.WithTimeout(ctx, checkpointTimeout)
	defer cancel()
	r, err := m.openCheckpointRepo(ctx, s)
	if err != nil {
		return nil, err
	}
	unlock := m.lockCheckpoints(r.top)
	defer unlock()
	list, err := r.list(ctx, scopeRefPrefix(s))
	if err != nil {
		return nil, err
	}
	var target *Checkpoint
	for i := range list {
		c := &list[i]
		if (id == "" && c.Reason == CheckpointBeforeEdit) || (id != "" && c.ID == id) {
			target = c // list is oldest first: keep the newest match
		}
	}
	if target == nil {
		if id == "" {
			return nil, fmt.Errorf("%w: no checkpoint recorded before an edit for %s replica %d", ErrCheckpointNotFound, s.NodeID, s.Replica)
		}
		return nil, fmt.Errorf("%w: %s", ErrCheckpointNotFound, id)
	}
	restored := *target
	meta.Reason = CheckpointBeforeRestore
	saved, _, current, err := r.record(ctx, s, meta, m.maxCheckpoints())
	if err != nil {
		return nil, fmt.Errorf("record state before restore: %w", err)
	}
	want, err := r.lsTree(ctx, restored.Tree)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.top)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	res := &RestoreResult{Restored: restored, Saved: *saved}
	parents := map[string]bool{}
	for _, p := range sortedKeys(current) {
		if _, keep := want[p]; keep || current[p].mode == gitlinkMode {
			continue
		}
		if err := root.Remove(filepath.FromSlash(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return res, fmt.Errorf("restore: remove %s: %w", p, err)
		}
		res.Removed++
		for dir := path.Dir(p); dir != "." && dir != r.prefix; dir = path.Dir(dir) {
			parents[dir] = true
		}
	}
	if err := pruneEmptyDirs(root, parents); err != nil {
		return res, err
	}
	var writes []string
	for _, p := range sortedKeys(want) {
		if e := want[p]; e.mode != gitlinkMode && current[p] != e {
			writes = append(writes, p)
		}
	}
	n, err := r.writeEntries(ctx, root, want, writes)
	res.Written = n
	if err != nil {
		return res, err
	}
	return res, nil
}

func (m *Manager) lockCheckpoints(top string) func() {
	l := m.lock("checkpoint:" + top)
	l.Lock()
	return l.Unlock
}

func newestFirst(list []Checkpoint) {
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.After(list[j].CreatedAt)
		}
		if list[i].NodeID != list[j].NodeID || list[i].Replica != list[j].Replica {
			return list[i].ID < list[j].ID
		}
		return list[i].Sequence > list[j].Sequence
	})
}

// --- repository access -----------------------------------------------------

const gitlinkMode = "160000"

type treeEntry struct{ mode, oid string }

type checkpointGit struct {
	bin string
	dir string
	env []string
}

func (g *checkpointGit) with(env ...string) *checkpointGit {
	return &checkpointGit{bin: g.bin, dir: g.dir, env: append(append([]string{}, g.env...), env...)}
}

// command builds a Git command that runs no hooks, filters, pagers, signing
// programs, maintenance or lazy fetches and reads no system/global config.
func (g *checkpointGit) command(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"--no-pager",
		"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false",
		"-c", "core.splitIndex=false", "-c", "commit.gpgSign=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false",
	}, args...)
	cmd := exec.CommandContext(ctx, g.bin, full...)
	cmd.Dir = g.dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_LITERAL_PATHSPECS=1", "LC_ALL=C",
		"GIT_AUTHOR_NAME=weft", "GIT_AUTHOR_EMAIL=weft@localhost", "GIT_COMMITTER_NAME=weft", "GIT_COMMITTER_EMAIL=weft@localhost",
	}, g.env...)
	return cmd
}

var errOutputLimit = errors.New("Git output exceeds the checkpoint limit")

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errOutputLimit
	}
	return b.Buffer.Write(p)
}

func (g *checkpointGit) run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := g.command(ctx, args...)
	cmd.Stdin = stdin
	out := &limitedBuffer{limit: checkpointOutputLimit}
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 512 {
			msg = msg[:512] + "…"
		}
		return out.Bytes(), &gitError{op: args[0], err: err, stderr: msg}
	}
	return out.Bytes(), nil
}

type gitError struct {
	op     string
	err    error
	stderr string
}

func (e *gitError) Error() string {
	if e.stderr == "" {
		return fmt.Sprintf("git %s: %v", e.op, e.err)
	}
	return fmt.Sprintf("git %s: %v: %s", e.op, e.err, e.stderr)
}
func (e *gitError) Unwrap() error { return e.err }

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

type checkpointRepo struct {
	git    *checkpointGit
	top    string // repository top level (symlinks resolved)
	prefix string // scope directory relative to top, slash-separated; "" = top
	format string // object format: sha1 or sha256
}

func hasGitEntry(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func (m *Manager) openCheckpointRepo(ctx context.Context, s CheckpointScope) (*checkpointRepo, error) {
	if !validRefPart(s.GraphID) || !validRefPart(s.NodeID) || s.Replica < 0 {
		return nil, fmt.Errorf("invalid checkpoint scope")
	}
	dir, err := filepath.EvalSymlinks(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("replica workspace: %w", err)
	}
	base := dir
	if s.Base != "" {
		if base, err = filepath.EvalSymlinks(s.Base); err != nil {
			return nil, fmt.Errorf("graph workspace: %w", err)
		}
	}
	if !hasGitEntry(dir) && !hasGitEntry(base) {
		return nil, ErrNotRepository
	}
	g := &checkpointGit{bin: m.GitBin, dir: dir}
	out, err := g.run(ctx, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, err
	}
	if top != dir && top != base {
		return nil, fmt.Errorf("%w: the repository top level is outside the replica workspace", ErrNotRepository)
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("replica workspace is outside its repository")
	}
	if rel == "." {
		rel = ""
	}
	g.dir = top
	out, err = g.run(ctx, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, err
	}
	format := strings.TrimSpace(string(out))
	if format != "sha1" && format != "sha256" {
		return nil, fmt.Errorf("unsupported Git object format %q", format)
	}
	return &checkpointRepo{git: g, top: top, prefix: filepath.ToSlash(rel), format: format}, nil
}

func (r *checkpointRepo) pathspec() []string {
	if r.prefix == "" {
		return []string{"--"}
	}
	return []string{"--", r.prefix}
}

// inScope reports whether a repository path belongs to the checkpoint scope.
func (r *checkpointRepo) inScope(p string) bool {
	rel := p
	if r.prefix != "" {
		if !strings.HasPrefix(p, r.prefix+"/") {
			return false
		}
		rel = p[len(r.prefix)+1:]
	}
	return rel != "" && rel != scratchDir && !strings.HasPrefix(rel, scratchDir+"/")
}

func (r *checkpointRepo) zeroOID() string {
	if r.format == "sha256" {
		return strings.Repeat("0", 64)
	}
	return strings.Repeat("0", 40)
}

func (r *checkpointRepo) blobID(data []byte) string {
	var h hash.Hash = sha1.New()
	if r.format == "sha256" {
		h = sha256.New()
	}
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// record writes a checkpoint of the scope unless its tree equals the newest
// one. It also returns the scope's current entries (for restores).
func (r *checkpointRepo) record(ctx context.Context, s CheckpointScope, meta CheckpointMeta, keep int) (*Checkpoint, bool, map[string]treeEntry, error) {
	tmp, err := os.MkdirTemp("", "weft-checkpoint-")
	if err != nil {
		return nil, false, nil, err
	}
	defer os.RemoveAll(tmp)
	ig := r.git.with("GIT_INDEX_FILE=" + filepath.Join(tmp, "index"))

	head := ""
	out, err := ig.run(ctx, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	switch {
	case err == nil:
		head = strings.TrimSpace(string(out))
		if _, err := ig.run(ctx, nil, "read-tree", head); err != nil {
			return nil, false, nil, err
		}
	case exitCode(err) != 1: // 1 = unborn branch
		return nil, false, nil, err
	}
	cached, err := r.cachedEntries(ctx, ig)
	if err != nil {
		return nil, false, nil, err
	}
	current, contents, err := r.scan(ctx, ig, cached)
	if err != nil {
		return nil, false, nil, err
	}
	if err := r.storeMissing(ctx, ig, contents); err != nil {
		return nil, false, nil, err
	}
	var info bytes.Buffer
	for _, p := range sortedKeys(cached) {
		if _, ok := current[p]; !ok {
			fmt.Fprintf(&info, "0 %s\t%s\x00", r.zeroOID(), p)
		}
	}
	for _, p := range sortedKeys(current) {
		if e := current[p]; cached[p] != e {
			fmt.Fprintf(&info, "%s %s\t%s\x00", e.mode, e.oid, p)
		}
	}
	if info.Len() > 0 {
		if _, err := ig.run(ctx, &info, "update-index", "-z", "--index-info"); err != nil {
			return nil, false, nil, err
		}
	}
	out, err = ig.run(ctx, nil, "write-tree")
	if err != nil {
		return nil, false, nil, err
	}
	tree := strings.TrimSpace(string(out))

	list, err := r.list(ctx, scopeRefPrefix(s))
	if err != nil {
		return nil, false, nil, err
	}
	if n := len(list); n > 0 && list[n-1].Tree == tree {
		newest := list[n-1]
		return &newest, false, current, nil
	}
	seq := 1
	if n := len(list); n > 0 {
		seq = list[n-1].Sequence + 1
	}
	args := []string{"commit-tree", "--no-gpg-sign", tree, "-F", "-"}
	if head != "" {
		args = append(args, "-p", head)
	}
	msg := fmt.Sprintf("weft checkpoint %s\n\nWeft-Execution: %s\nWeft-Reason: %s\nWeft-Tool: %s\n",
		trailerValue(meta.Reason), trailerValue(meta.ExecutionID), trailerValue(meta.Reason), trailerValue(meta.Tool))
	out, err = r.git.run(ctx, strings.NewReader(msg), args...)
	if err != nil {
		return nil, false, nil, err
	}
	commit := strings.TrimSpace(string(out))
	ref := fmt.Sprintf("%s%08d", scopeRefPrefix(s), seq)
	if _, err := r.git.run(ctx, nil, "update-ref", ref, commit, r.zeroOID()); err != nil {
		return nil, false, nil, err
	}
	cp := Checkpoint{ID: CheckpointID(s.NodeID, s.Replica, seq), Ref: ref, Commit: commit, Tree: tree, NodeID: s.NodeID, Replica: s.Replica,
		Sequence: seq, ExecutionID: trailerValue(meta.ExecutionID), Reason: trailerValue(meta.Reason), Tool: trailerValue(meta.Tool), CreatedAt: time.Now().UTC().Truncate(time.Second)}
	list = append(list, cp)
	for _, old := range list[:max(0, len(list)-keep)] {
		if _, err := r.git.run(ctx, nil, "update-ref", "-d", old.Ref, old.Commit); err != nil {
			return &cp, true, current, fmt.Errorf("prune checkpoint %s: %w", old.ID, err)
		}
	}
	return &cp, true, current, nil
}

func trailerValue(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}

// cachedEntries lists the temporary index (HEAD) entries inside the scope.
func (r *checkpointRepo) cachedEntries(ctx context.Context, ig *checkpointGit) (map[string]treeEntry, error) {
	out, err := ig.run(ctx, nil, append([]string{"ls-files", "-z", "-s"}, r.pathspec()...)...)
	if err != nil {
		return nil, err
	}
	cached := map[string]treeEntry{}
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("unexpected ls-files output")
		}
		if r.inScope(p) {
			cached[p] = treeEntry{fields[0], fields[1]}
		}
	}
	return cached, nil
}

// scan reads the scope's files through os.Root and hashes them. contents
// holds the data of blobs that are not in HEAD (they may need storing).
func (r *checkpointRepo) scan(ctx context.Context, ig *checkpointGit, cached map[string]treeEntry) (map[string]treeEntry, map[string][]byte, error) {
	out, err := ig.run(ctx, nil, append([]string{"ls-files", "-z", "--others", "--exclude-standard"}, r.pathspec()...)...)
	if err != nil {
		return nil, nil, err
	}
	paths := make(map[string]bool, len(cached))
	for p := range cached {
		paths[p] = true
	}
	for _, p := range strings.Split(string(out), "\x00") {
		// "dir/" is a nested repository: not part of this checkpoint.
		if p != "" && !strings.HasSuffix(p, "/") && r.inScope(p) {
			paths[p] = true
		}
	}
	if len(paths) > checkpointFileLimit {
		return nil, nil, fmt.Errorf("%w: more than %d files", ErrCheckpointTooLarge, checkpointFileLimit)
	}
	root, err := os.OpenRoot(r.top)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	current := map[string]treeEntry{}
	contents := map[string][]byte{}
	var total int64
	for _, p := range sortedKeys(paths) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if c, ok := cached[p]; ok && c.mode == gitlinkMode {
			current[p] = c
			continue
		}
		name := filepath.FromSlash(p)
		info, err := root.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		var data []byte
		mode := "100644"
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := root.Readlink(name)
			if err != nil {
				return nil, nil, err
			}
			data, mode = []byte(target), "120000"
		case info.Mode().IsRegular():
			if total += info.Size(); total > checkpointByteLimit {
				return nil, nil, fmt.Errorf("%w: more than %d MiB", ErrCheckpointTooLarge, checkpointByteLimit>>20)
			}
			if data, err = readRooted(root, name, checkpointByteLimit-total+info.Size()); err != nil {
				return nil, nil, err
			}
			if info.Mode()&0o111 != 0 {
				mode = "100755"
			}
		default:
			// A directory where HEAD had a file (its files are listed on
			// their own), or a socket/device Git cannot store.
			continue
		}
		e := treeEntry{mode, r.blobID(data)}
		current[p] = e
		if cached[p].oid != e.oid {
			contents[e.oid] = data
		}
	}
	return current, contents, nil
}

func readRooted(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s changed while recording the checkpoint", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: more than %d MiB", ErrCheckpointTooLarge, checkpointByteLimit>>20)
	}
	return data, nil
}

// storeMissing writes the blobs the object database lacks in one fast-import
// stream (no filters run; contents come from Go).
func (r *checkpointRepo) storeMissing(ctx context.Context, ig *checkpointGit, contents map[string][]byte) error {
	if len(contents) == 0 {
		return nil
	}
	var query bytes.Buffer
	oids := sortedKeys(contents)
	for _, oid := range oids {
		query.WriteString(oid + "\n")
	}
	out, err := ig.run(ctx, &query, "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return err
	}
	var missing []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if oid, state, ok := strings.Cut(line, " "); ok && state == "missing" {
			missing = append(missing, oid)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	pr, pw := io.Pipe()
	go func() {
		w := bufio.NewWriter(pw)
		for _, oid := range missing {
			data := contents[oid]
			fmt.Fprintf(w, "blob\ndata %d\n", len(data))
			w.Write(data)
			w.WriteString("\n")
		}
		pw.CloseWithError(w.Flush())
	}()
	_, err = ig.run(ctx, pr, "fast-import", "--quiet")
	pr.Close()
	return err
}

// list returns the checkpoints under a ref prefix, oldest first per scope.
func (r *checkpointRepo) list(ctx context.Context, prefix string) ([]Checkpoint, error) {
	out, err := r.git.run(ctx, nil, "for-each-ref",
		"--format=%(refname)%00%(objectname)%00%(tree)%00%(creatordate:unix)%00%(contents:body)%1e", prefix)
	if err != nil {
		return nil, err
	}
	list := []Checkpoint{}
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x00", 5)
		if len(f) != 5 {
			return nil, fmt.Errorf("unexpected checkpoint ref listing")
		}
		parts := strings.Split(strings.TrimPrefix(f[0], CheckpointRefPrefix), "/")
		if len(parts) != 4 || !strings.HasPrefix(parts[2], "r") {
			continue // not a ref this package wrote
		}
		replica, rerr := strconv.Atoi(parts[2][1:])
		seq, serr := strconv.Atoi(parts[3])
		unix, terr := strconv.ParseInt(f[3], 10, 64)
		if rerr != nil || serr != nil || terr != nil || seq <= 0 || !validRefPart(parts[1]) {
			continue
		}
		c := Checkpoint{ID: CheckpointID(parts[1], replica, seq), Ref: f[0], Commit: f[1], Tree: f[2], NodeID: parts[1], Replica: replica,
			Sequence: seq, CreatedAt: time.Unix(unix, 0).UTC()}
		for _, line := range strings.Split(f[4], "\n") {
			key, value, _ := strings.Cut(line, ": ")
			switch key {
			case "Weft-Execution":
				c.ExecutionID = value
			case "Weft-Reason":
				c.Reason = value
			case "Weft-Tool":
				c.Tool = value
			}
		}
		list = append(list, c)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].NodeID != list[j].NodeID || list[i].Replica != list[j].Replica {
			return list[i].ID < list[j].ID
		}
		return list[i].Sequence < list[j].Sequence
	})
	return list, nil
}

// lsTree lists a checkpoint tree's entries inside the scope.
func (r *checkpointRepo) lsTree(ctx context.Context, tree string) (map[string]treeEntry, error) {
	out, err := r.git.run(ctx, nil, append([]string{"ls-tree", "-r", "-z", tree}, r.pathspec()...)...)
	if err != nil {
		return nil, err
	}
	want := map[string]treeEntry{}
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("unexpected ls-tree output")
		}
		if r.inScope(p) {
			want[p] = treeEntry{fields[0], fields[2]}
		}
	}
	return want, nil
}

// writeEntries writes the given checkpoint entries, reading blobs through one
// cat-file --batch process (no filters) and writing through os.Root.
func (r *checkpointRepo) writeEntries(ctx context.Context, root *os.Root, want map[string]treeEntry, paths []string) (int, error) {
	if len(paths) == 0 {
		return 0, nil
	}
	cmd := r.git.command(ctx, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() {
		w := bufio.NewWriter(stdin)
		for _, p := range paths {
			w.WriteString(want[p].oid + "\n")
		}
		w.Flush()
		stdin.Close()
	}()
	written := 0
	reader := bufio.NewReader(stdout)
	var werr error
	for _, p := range paths {
		var data []byte
		if data, werr = readBatchObject(reader, want[p].oid); werr != nil {
			break
		}
		if werr = writeEntry(root, p, want[p].mode, data); werr != nil {
			werr = fmt.Errorf("restore: write %s: %w", p, werr)
			break
		}
		written++
	}
	if werr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return written, werr
	}
	if err := cmd.Wait(); err != nil {
		return written, &gitError{op: "cat-file", err: err, stderr: strings.TrimSpace(stderr.String())}
	}
	return written, nil
}

func readBatchObject(r *bufio.Reader, oid string) ([]byte, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read checkpoint object %s: %w", oid, err)
	}
	fields := strings.Fields(header)
	if len(fields) != 3 || fields[0] != oid || fields[1] != "blob" {
		return nil, fmt.Errorf("checkpoint object %s unavailable: %s", oid, strings.TrimSpace(header))
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 || size > checkpointByteLimit {
		return nil, fmt.Errorf("checkpoint object %s has an invalid size", oid)
	}
	data := make([]byte, size+1)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read checkpoint object %s: %w", oid, err)
	}
	return data[:size], nil
}

func writeEntry(root *os.Root, p, mode string, data []byte) error {
	name := filepath.FromSlash(p)
	if info, err := root.Lstat(name); err == nil {
		// A directory still here holds ignored files: refuse to delete them.
		if err := root.Remove(name); err != nil {
			if info.IsDir() {
				return fmt.Errorf("a directory with files the checkpoint does not track is in the way: %w", err)
			}
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if dir := path.Dir(p); dir != "." {
		if err := root.MkdirAll(filepath.FromSlash(dir), 0o755); err != nil {
			return err
		}
	}
	if mode == "120000" {
		return root.Symlink(string(data), name)
	}
	perm := os.FileMode(0o644)
	if mode == "100755" {
		perm = 0o755
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// pruneEmptyDirs removes directories left empty by a restore, deepest first.
func pruneEmptyDirs(root *os.Root, dirs map[string]bool) error {
	list := sortedKeys(dirs)
	sort.SliceStable(list, func(i, j int) bool { return strings.Count(list[i], "/") > strings.Count(list[j], "/") })
	for _, d := range list {
		info, err := root.Lstat(filepath.FromSlash(d))
		if err != nil || !info.IsDir() {
			continue // gone, or replaced by a file or link the restore must not touch
		}
		err = root.Remove(filepath.FromSlash(d))
		if err == nil || errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
			continue
		}
		return fmt.Errorf("restore: remove empty directory %s: %w", d, err)
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
