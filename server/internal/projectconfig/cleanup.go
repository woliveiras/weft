package projectconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/woliveiras/weft/server/internal/storage"
)

type CleanupPreview struct {
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Head        string `json:"head"`
	Status      string `json:"status"`
	Clean       bool   `json:"clean"`
	Fingerprint string `json:"fingerprint"`
}

// PreviewCleanup only accepts an existing linked checkout of the registered
// project. Ignored files also block deletion; no force/prune/reset is used.
func PreviewCleanup(ctx context.Context, p *Project, dir string) (*CleanupPreview, error) {
	source, _, err := Inspect(ctx, p.Path)
	if err != nil || source != p.Path {
		return nil, fmt.Errorf("%w: project path changed", ErrInvalid)
	}
	real, err := filepath.EvalSymlinks(dir)
	if os.IsNotExist(err) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if real != filepath.Clean(dir) || real == source {
		return nil, fmt.Errorf("%w: checkout path changed", ErrInvalid)
	}
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: expected a linked worktree", ErrInvalid)
	}
	common, err := git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	expected, err := git(ctx, source, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil {
		return nil, err
	}
	expected, err = filepath.EvalSymlinks(expected)
	if err != nil || common != expected {
		return nil, fmt.Errorf("%w: checkout belongs to another repository", ErrInvalid)
	}
	head, err := git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, err
	}
	branch, err := git(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%w: detached checkout cannot be removed here", ErrInvalid)
	}
	status, err := git(ctx, dir, "status", "--porcelain=v1", "--untracked-files=all", "--ignored")
	if err != nil {
		return nil, err
	}
	v := &CleanupPreview{Path: real, Branch: branch, Head: head, Status: status, Clean: strings.TrimSpace(status) == ""}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	v.Fingerprint = hex.EncodeToString(sum[:])
	return v, nil
}

func RemoveReviewedWorktree(ctx context.Context, p *Project, dir, fingerprint string) error {
	current, err := PreviewCleanup(ctx, p, dir)
	if err != nil {
		return err
	}
	if !cleanupApproved(current, fingerprint) {
		return fmt.Errorf("%w: checkout changed or contains files to preserve; review again", storage.ErrConflict)
	}
	_, err = git(ctx, p.Path, "worktree", "remove", "--", dir)
	return err
}

func cleanupApproved(v *CleanupPreview, fingerprint string) bool {
	return v != nil && v.Clean && fingerprint != "" && v.Fingerprint == fingerprint
}
