package projectconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// PreviewTaskCleanup selects the new representation exclusively through the
// persisted binding. Old tasks retain linked-worktree cleanup and its fences.
func (m Manager) PreviewTaskCleanup(ctx context.Context, p *Project, graphID, dir string) (*CleanupPreview, error) {
	b, err := m.GetTaskCheckout(ctx, graphID)
	if errors.Is(err, storage.ErrNotFound) {
		return PreviewCleanup(ctx, p, dir)
	}
	if err != nil {
		return nil, err
	}
	if !b.valid(p, graphID, b.Path) {
		return nil, fmt.Errorf("%w: task checkout binding changed", ErrInvalid)
	}
	if err = localTaskMetadata(b.Path); err != nil {
		return nil, err
	}
	if dir != b.Path {
		return previewTaskReplicaCleanup(ctx, b, dir)
	}
	if _, err = os.Lstat(b.ArchivePath); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: retained task repository already exists or is unavailable", storage.ErrConflict)
	}
	// Explicit --git-dir/--work-tree makes repository configuration incapable
	// of redirecting which checkout is reviewed. Replacement refs are disabled.
	git := func(args ...string) (string, error) {
		prefix := []string{"--no-replace-objects", "--git-dir=" + filepath.Join(dir, ".git"), "--work-tree=" + dir}
		return checkoutGit(ctx, dir, append(prefix, args...)...)
	}
	metadata, err := taskMetadataFingerprint(ctx, filepath.Join(dir, ".git"), false)
	if err != nil {
		return nil, err
	}
	if _, err = git("merge-base", "--is-ancestor", b.Base, "HEAD"); err != nil {
		return nil, fmt.Errorf("%w: task checkout no longer contains its reviewed base", ErrInvalid)
	}
	head, err := git("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, err
	}
	branch, err := git("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%w: detached checkout cannot be removed here", ErrInvalid)
	}
	inspection, err := (&workspace.Manager{GitBin: "git"}).InspectCleanup(ctx, dir, dir)
	if err != nil {
		return nil, err
	}
	v := &CleanupPreview{Path: dir, Branch: branch, Head: head, Status: inspection.Content, Clean: strings.TrimSpace(inspection.Content) == "", RetainedRepository: b.ArchivePath, MetadataFingerprint: metadata}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	v.Fingerprint = hex.EncodeToString(sum[:])
	return v, nil
}

func (m Manager) RemoveReviewedTaskCheckout(ctx context.Context, p *Project, graphID, dir, fingerprint string) error {
	b, err := m.GetTaskCheckout(ctx, graphID)
	if errors.Is(err, storage.ErrNotFound) {
		return RemoveReviewedWorktree(ctx, p, dir, fingerprint)
	}
	if err != nil {
		return err
	}
	current, err := m.PreviewTaskCleanup(ctx, p, graphID, dir)
	if err != nil {
		return err
	}
	if !cleanupApproved(current, fingerprint) {
		return fmt.Errorf("%w: checkout or retained metadata changed; review again", storage.ErrConflict)
	}
	if dir != b.Path {
		return removeTaskReplica(ctx, b, dir)
	}
	archiveParent := filepath.Dir(b.ArchivePath)
	if err = os.MkdirAll(archiveParent, 0700); err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(archiveParent)
	if err != nil || real != archiveParent {
		return fmt.Errorf("%w: retained repository path changed", ErrInvalid)
	}
	// Retain every branch, reflog, checkpoint and object before removing the
	// clean checkout. Nothing is imported into the source repository. A failed
	// archive or partial removal is reported and never silently replayed.
	if err = os.Rename(filepath.Join(dir, ".git"), b.ArchivePath); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// The only linked worktrees admitted under a private task binding are the
// platform's existing per-node replica paths. Their repository remains owned by
// the task; no registered-project metadata is selected by a .git pointer.
func taskReplicaMetadata(b *TaskCheckout, dir string) (string, error) {
	if filepath.Dir(dir) != filepath.Join(filepath.Dir(b.Path), ".worktrees", b.GraphID) || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-[0-9]+$`).MatchString(filepath.Base(dir)) {
		return "", ErrInvalid
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || real != dir {
		return "", ErrInvalid
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(".git")
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrInvalid
	}
	f, err := openTaskMetadata(root, ".git")
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return "", errors.Join(err, closeErr)
	}
	if len(data) > 4096 || !strings.HasPrefix(string(data), "gitdir: ") {
		return "", ErrInvalid
	}
	metadata := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
	if !filepath.IsAbs(metadata) {
		metadata = filepath.Join(dir, metadata)
	}
	metadata = filepath.Clean(metadata)
	if filepath.Dir(metadata) != filepath.Join(b.Path, ".git", "worktrees") {
		return "", ErrInvalid
	}
	registry, err := os.OpenRoot(filepath.Join(b.Path, ".git"))
	if err != nil {
		return "", err
	}
	defer registry.Close()
	read := func(name string) (string, error) {
		f, err := openTaskMetadata(registry, filepath.Join("worktrees", filepath.Base(metadata), name))
		if err != nil {
			return "", err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 4097))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil {
			return "", errors.Join(readErr, closeErr)
		}
		if len(data) > 4096 {
			return "", ErrInvalid
		}
		return strings.TrimSpace(string(data)), nil
	}
	back, err := read("gitdir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(back) {
		back = filepath.Join(metadata, back)
	}
	common, err := read("commondir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(metadata, common)
	}
	if filepath.Clean(back) != filepath.Join(dir, ".git") || filepath.Clean(common) != filepath.Join(b.Path, ".git") {
		return "", ErrInvalid
	}
	return metadata, nil
}

func openTaskMetadata(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalid
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, ErrInvalid
	}
	return f, nil
}

func previewTaskReplicaCleanup(ctx context.Context, b *TaskCheckout, dir string) (*CleanupPreview, error) {
	metadata, err := taskReplicaMetadata(b, dir)
	if err != nil {
		return nil, err
	}
	fingerprint, err := taskMetadataFingerprint(ctx, filepath.Join(b.Path, ".git"), true)
	if err != nil {
		return nil, err
	}
	inspection, err := (&workspace.Manager{GitBin: "git"}).InspectCleanup(ctx, dir, b.Path)
	if err != nil {
		return nil, err
	}
	if inspection.Branch == "(detached)" {
		return nil, ErrInvalid
	}
	head, err := checkoutGit(ctx, b.Path, "--git-dir="+metadata, "--work-tree="+dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, err
	}
	v := &CleanupPreview{Path: dir, Branch: inspection.Branch, Head: head, Status: inspection.Content, Clean: strings.TrimSpace(inspection.Content) == "", RetainedRepository: filepath.Join(b.Path, ".git"), MetadataFingerprint: fingerprint}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	v.Fingerprint = hex.EncodeToString(sum[:])
	return v, nil
}

func removeTaskReplica(ctx context.Context, b *TaskCheckout, dir string) error {
	metadata, err := taskReplicaMetadata(b, dir)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Git worktree remove would run dirty checks using agent-editable config
	// again. The reviewed snapshot already proved cleanliness; remove only
	// these two confined directories, retaining refs/checkpoints in the base.
	parent, err := os.OpenRoot(filepath.Dir(dir))
	if err != nil {
		return err
	}
	defer parent.Close()
	if err = parent.RemoveAll(filepath.Base(dir)); err != nil {
		return err
	}
	registry, err := os.OpenRoot(filepath.Join(b.Path, ".git", "worktrees"))
	if err != nil {
		return err
	}
	defer registry.Close()
	return registry.RemoveAll(filepath.Base(metadata))
}

// Include all retained metadata in the reviewed fence. Refuse symlinked,
// oversized, locked or externally linked metadata rather than follow it.
func taskMetadataFingerprint(ctx context.Context, root string, allowWorktrees bool) (string, error) {
	h := sha256.New()
	parent, err := os.OpenRoot(filepath.Dir(root))
	if err != nil {
		return "", err
	}
	defer parent.Close()
	confined, err := parent.OpenRoot(filepath.Base(root))
	if err != nil {
		return "", err
	}
	defer confined.Close()
	var bytes int64
	files := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(d.Name(), ".lock") || (allowWorktrees && d.Name() == "locked") {
			return fmt.Errorf("%w: task repository is locked", storage.ErrConflict)
		}
		if rel == "worktrees" && !allowWorktrees {
			directory, e := confined.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if e != nil {
				return e
			}
			info, e := directory.Stat()
			if e != nil || !info.IsDir() {
				directory.Close()
				return ErrInvalid
			}
			children, e := directory.ReadDir(1)
			closeErr := directory.Close()
			if e == io.EOF {
				e = nil
			}
			if closeErr != nil {
				return closeErr
			}
			if e != nil {
				return e
			}
			if len(children) > 0 {
				return fmt.Errorf("%w: task repository still has linked worktrees", storage.ErrConflict)
			}
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		bytes += info.Size()
		if !info.Mode().IsRegular() || files > 100000 || bytes > 2<<30 {
			return fmt.Errorf("%w: task metadata cannot be safely retained", ErrInvalid)
		}
		fmt.Fprintf(h, "%d:%s:%d:", len(rel), rel, info.Size())
		f, err := openTaskMetadata(confined, rel)
		if err != nil {
			return err
		}
		copied, copyErr := io.Copy(h, io.LimitReader(f, info.Size()+1))
		if copied != info.Size() {
			copyErr = errors.Join(copyErr, storage.ErrConflict)
		}
		closeErr := f.Close()
		return errors.Join(copyErr, closeErr)
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
