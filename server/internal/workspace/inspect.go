package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const InspectionLimit = 4 << 20

var ErrInspectionLimit = errors.New("Git inspection exceeds 4 MiB")

// Inspection is a live observation, never evidence of who authored a change.
type Inspection struct {
	Repository  bool   `json:"repository"`
	Directory   string `json:"directory"`
	Branch      string `json:"branch"`
	Content     string `json:"content"`
	Attribution string `json:"attribution"`
	GraphID     string `json:"graphId"`
	NodeID      string `json:"nodeId,omitempty"`
	Replica     *int   `json:"replica,omitempty"`
}

type boundedOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Len() int       { return b.buffer.Len() }
func (b *boundedOutput) String() string { return b.buffer.String() }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > InspectionLimit-b.Len() {
		b.overflow = true
		return 0, ErrInspectionLimit
	}
	return b.buffer.Write(p)
}

func InspectionArgs(view, path string) ([]string, error) {
	if strings.ContainsRune(path, 0) || filepath.IsAbs(path) {
		return nil, fmt.Errorf("invalid relative path")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." || strings.EqualFold(part, ".git") {
			return nil, fmt.Errorf("invalid relative path")
		}
	}
	var args []string
	switch view {
	case "status":
		args = []string{"status", "--short", "--untracked-files=all", "--ignore-submodules=all"}
	case "diff":
		args = []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--ignore-submodules=all", "HEAD"}
	case "history":
		args = []string{"log", "-50", "--format=%h %aI %s", "--no-decorate"}
	default:
		return nil, fmt.Errorf("invalid Git view")
	}
	args = append(args, "--")
	if path != "" {
		args = append(args, path)
	}
	return args, nil
}

// Inspect uses a bounded private copy of Git metadata so concurrent agent
// changes cannot replace validated configuration before Git reads it.
func (m *Manager) Inspect(ctx context.Context, dir, base, view, path string) (*Inspection, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := InspectionArgs(view, path); err != nil {
		return nil, err
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}
	work, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer work.Close()
	info, err := work.Lstat(".git")
	if errors.Is(err, os.ErrNotExist) {
		return &Inspection{Directory: dir, Attribution: "inconclusive"}, nil
	}
	if err != nil {
		return nil, err
	}
	relativeGit := "."
	if dir != base && info.Mode().IsRegular() {
		f, err := work.OpenFile(".git", os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 4097))
		f.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(data) > 4096 || !strings.HasPrefix(string(data), "gitdir: ") {
			return nil, fmt.Errorf("invalid worktree metadata")
		}
		target := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		rel, err := filepath.Rel(filepath.Join(base, ".git", "worktrees"), target)
		if err != nil || rel == "." || rel == ".." || strings.Contains(rel, string(filepath.Separator)) {
			return nil, fmt.Errorf("worktree metadata escapes repository")
		}
		relativeGit = filepath.Join("worktrees", rel)
	} else if dir != base || !info.IsDir() {
		return nil, fmt.Errorf("invalid Git directory")
	}
	source, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	metadata, err := source.OpenRoot(".git")
	if err != nil {
		return nil, err
	}
	defer metadata.Close()
	snapshot, err := snapshotGitMetadata(ctx, metadata)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(snapshot)
	return m.inspectSnapshot(ctx, dir, snapshot, relativeGit, view, path)
}

func (m *Manager) inspectSnapshot(ctx context.Context, dir, snapshot, relativeGit, view, path string) (*Inspection, error) {
	args, err := InspectionArgs(view, path)
	if err != nil {
		return nil, err
	}
	result := &Inspection{Directory: dir, Attribution: "inconclusive"}
	gitdir := filepath.Join(snapshot, relativeGit)
	if _, err := os.Lstat(filepath.Join(snapshot, "commondir")); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("common Git directory cannot redirect")
	}
	if relativeGit != "." {
		common, err := readMetadata(filepath.Join(gitdir, "commondir"))
		if err != nil || filepath.Clean(filepath.Join(gitdir, strings.TrimSpace(string(common)))) != snapshot {
			return nil, fmt.Errorf("invalid common Git directory")
		}
	}

	run := func(args ...string) (string, error) {
		child, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		prefix := []string{"--no-pager", "--git-dir=" + gitdir, "--work-tree=" + dir, "-c", "core.bare=false", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "color.ui=false", "-c", "log.showSignature=false"}
		workingDir := dir
		if len(args) > 0 && args[0] == "config" {
			prefix = []string{"--no-pager"}
			workingDir = filepath.Dir(snapshot)
		}
		cmd := exec.CommandContext(child, m.GitBin, append(prefix, args...)...)
		cmd.Dir = workingDir
		// Deliberately do not inherit GIT_DIR, alternate object stores or user config.
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
		var out, stderr boundedOutput
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if out.overflow || stderr.overflow {
				return "", ErrInspectionLimit
			}
			return "", fmt.Errorf("Git inspection failed: %w", err)
		}
		return out.String(), nil
	}
	// --no-includes prevents reading external include files. Only inert keys
	// written by init/clone/worktree are accepted; unknown configuration fails
	// closed instead of executing clean/process filters or third-party drivers.
	config, err := run("config", "--file", filepath.Join(snapshot, "config"), "--no-includes", "--name-only", "--list", "-z")
	if err != nil {
		return nil, err
	}
	for _, key := range strings.Split(strings.TrimSuffix(config, "\x00"), "\x00") {
		if !safeInspectionConfig(key) {
			return nil, fmt.Errorf("repository configuration is not supported for read-only inspection")
		}
	}
	branch, err := run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if _, headErr := run("rev-parse", "--verify", "HEAD"); headErr != nil {
			return nil, err
		}
		branch = "(detached)"
	}
	result.Repository = true
	result.Branch = strings.TrimSpace(branch)
	// Unborn repositories have status but no commit history or HEAD diff yet.
	if view != "status" {
		if _, err := run("rev-parse", "--verify", "HEAD"); err != nil {
			_, refErr := run("show-ref", "--verify", "--quiet", "refs/heads/"+result.Branch)
			var exit *exec.ExitError
			if !errors.As(refErr, &exit) || exit.ExitCode() != 1 {
				return nil, err
			}
			result.Content = "No commits yet. Untracked files are available in Files."
			return result, nil
		}
	}
	result.Content, err = run(args...)
	return result, err
}

func readMetadata(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid Git metadata")
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, err
	}
	if len(data) > 4096 {
		return nil, fmt.Errorf("Git metadata exceeds 4 KiB")
	}
	return data, nil
}

func safeInspectionConfig(key string) bool {
	key = strings.ToLower(key)
	switch key {
	case "", "core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.ignorecase", "core.precomposeunicode", "extensions.objectformat", "user.name", "user.email":
		return true
	}
	// These values are never used for network access by the inspection commands.
	return (strings.HasPrefix(key, "remote.") && (strings.HasSuffix(key, ".url") || strings.HasSuffix(key, ".fetch"))) || (strings.HasPrefix(key, "branch.") && (strings.HasSuffix(key, ".remote") || strings.HasSuffix(key, ".merge")))
}

// Snapshot metadata without symlinks/hardlinks, using rooted reads even if the
// source changes concurrently. Partial copies are removed on every failure.
const metadataByteLimit int64 = 64 << 20

func snapshotGitMetadata(ctx context.Context, source *os.Root) (snapshot string, err error) {
	snapshot, err = os.MkdirTemp("", "weft-git-inspect-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(snapshot)
		}
	}()
	pending := []string{"."}
	count := 0
	remaining := metadataByteLimit
	for len(pending) > 0 {
		if err = ctx.Err(); err != nil {
			return snapshot, err
		}
		path := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		dir, e := source.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e != nil {
			return snapshot, e
		}
		for {
			entries, readErr := dir.ReadDir(128)
			for _, entry := range entries {
				count++
				if count > 100000 {
					dir.Close()
					return snapshot, fmt.Errorf("Git metadata entry limit exceeded")
				}
				if entry.Type()&os.ModeSymlink != 0 || entry.Name() == "alternates" || entry.Name() == "http-alternates" {
					dir.Close()
					return snapshot, fmt.Errorf("indirect Git metadata is not supported")
				}
				rel := filepath.Join(path, entry.Name())
				dest := filepath.Join(snapshot, rel)
				if entry.IsDir() {
					if e = os.Mkdir(dest, 0700); e != nil {
						dir.Close()
						return snapshot, e
					}
					pending = append(pending, rel)
					continue
				}
				if !entry.Type().IsRegular() {
					dir.Close()
					return snapshot, fmt.Errorf("invalid Git metadata file")
				}
				n, e := copyMetadataFile(source, rel, dest, remaining)
				if e != nil {
					dir.Close()
					return snapshot, e
				}
				remaining -= n
			}
			if err = ctx.Err(); err != nil {
				dir.Close()
				return snapshot, err
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				dir.Close()
				return snapshot, readErr
			}
		}
		if e = dir.Close(); e != nil {
			return snapshot, e
		}
	}
	return snapshot, nil
}

func copyMetadataFile(source *os.Root, rel, dest string, remaining int64) (int64, error) {
	input, err := source.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("invalid Git metadata file")
	}
	if info.Size() > remaining {
		return 0, fmt.Errorf("Git metadata exceeds 64 MiB")
	}
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(output, io.LimitReader(input, remaining+1))
	closeErr := output.Close()
	if copyErr != nil {
		return n, copyErr
	}
	if closeErr != nil {
		return n, closeErr
	}
	if n > remaining {
		return n, fmt.Errorf("Git metadata exceeds 64 MiB")
	}
	return n, nil
}
