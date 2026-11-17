package projectconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCleanupRequiresFreshCleanReview(t *testing.T) {
	if cleanupApproved(nil, "a") || cleanupApproved(&CleanupPreview{Clean: true}, "") || cleanupApproved(&CleanupPreview{Clean: false, Fingerprint: "a"}, "a") || cleanupApproved(&CleanupPreview{Clean: true, Fingerprint: "a"}, "b") {
		t.Fatal("invalid cleanup accepted")
	}
	if !cleanupApproved(&CleanupPreview{Clean: true, Fingerprint: "a"}, "a") {
		t.Fatal("review rejected")
	}
}

func TestReviewedCleanupPreservesBranchesAndOtherWorktrees(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
	}
	gitRun("init", "-q")
	gitRun("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial")
	path, head, err := Inspect(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	p := &Project{Input: Input{Name: "Fixture", Path: path}, Head: head}
	// Use canonical paths: macOS temporary directories can have a symlinked prefix.
	root = filepath.Dir(path)
	first, err := CreateWorktree(t.Context(), p, head, root, "task-run-one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateWorktree(t.Context(), p, head, root, "task-run-two")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewCleanup(t.Context(), p, first)
	if err != nil || !preview.Clean {
		t.Fatal(preview, err)
	}
	if err = RemoveReviewedWorktree(t.Context(), p, first, "stale"); err == nil {
		t.Fatal("stale preview removed checkout")
	}
	if err = os.WriteFile(filepath.Join(first, "untracked"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = RemoveReviewedWorktree(t.Context(), p, first, preview.Fingerprint); err == nil {
		t.Fatal("untracked data removed")
	}
	// Add ignored data; it must still block cleanup after untracked content is committed.
	gitRun("-C", first, "add", "untracked")
	gitRun("-C", first, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "retain work")
	gitRun("-C", first, "config", "--local", "core.excludesFile", filepath.Join(root, "ignore"))
	if err = os.WriteFile(filepath.Join(root, "ignore"), []byte("ignored-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(first, "ignored-file")
	if err = os.WriteFile(ignored, []byte("keep ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	dirty, err := PreviewCleanup(t.Context(), p, first)
	if err != nil || dirty.Clean {
		t.Fatal(dirty, err)
	}
	if err = RemoveReviewedWorktree(t.Context(), p, first, dirty.Fingerprint); err == nil {
		t.Fatal("ignored data removed")
	}
	// Only this synthetic fixture file is removed to exercise the clean path.
	if err = os.Remove(ignored); err != nil {
		t.Fatal(err)
	}
	clean, err := PreviewCleanup(t.Context(), p, first)
	if err != nil || !clean.Clean {
		t.Fatal(clean, err)
	}
	if clean.Fingerprint == preview.Fingerprint {
		t.Fatal("changed HEAD not reflected")
	}
	gitRun("worktree", "lock", first)
	if err = RemoveReviewedWorktree(t.Context(), p, first, clean.Fingerprint); err == nil {
		t.Fatal("locked checkout removed")
	}
	if _, err = os.Stat(first); err != nil {
		t.Fatal("failed removal changed checkout", err)
	}
	gitRun("worktree", "unlock", first)
	if err = RemoveReviewedWorktree(t.Context(), p, first, clean.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("checkout remains", err)
	}
	gitRun("rev-parse", "--verify", "refs/heads/weft/task-run-one")
	if _, err = os.Stat(second); err != nil {
		t.Fatal("other checkout changed", err)
	}
	if _, err = PreviewCleanup(t.Context(), p, repo); err == nil {
		t.Fatal("source checkout accepted")
	}
}
