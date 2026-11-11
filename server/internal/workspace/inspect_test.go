package workspace

import (
	"context"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectionArgs(t *testing.T) {
	for _, path := range []string{"../x", "a/../x", "/etc/passwd", ".git/config", "a/.git/HEAD", "x\x00"} {
		if _, err := InspectionArgs("diff", path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if _, err := InspectionArgs("reset", ""); err == nil {
		t.Fatal("accepted mutation")
	}
	args, err := InspectionArgs("diff", ":(glob)*")
	if err != nil || args[len(args)-2] != "--" {
		t.Fatalf("literal path: %v %v", args, err)
	}
}
func TestBoundedInspectionOutput(t *testing.T) {
	var out boundedOutput
	if _, err := out.Write(make([]byte, InspectionLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("x")); err != ErrInspectionLimit {
		t.Fatal(err)
	}
}
func TestInspectRepositoryIntegration(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "", false)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	inspect := func(view, path string) *Inspection {
		t.Helper()
		got, err := m.Inspect(context.Background(), dir, dir, view, path)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if inspect("status", "").Repository {
		t.Fatal("non-repository")
	}
	git("init", "-q", "-b", "main")
	if !strings.Contains(inspect("history", "").Content, "No commits") {
		t.Fatal("unborn history")
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "initial fixture")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := inspect("status", ""); got.Branch != "main" || !strings.Contains(got.Content, "file.txt") || got.Attribution != "inconclusive" {
		t.Fatalf("%+v", got)
	}
	if got := inspect("diff", "file.txt"); !strings.Contains(got.Content, "+after") {
		t.Fatal(got.Content)
	}
	git("add", ".")
	if !strings.Contains(inspect("diff", "").Content, "+after") {
		t.Fatal("staged diff missing")
	}
	if !strings.Contains(inspect("history", "").Content, "initial fixture") {
		t.Fatal("history missing")
	}
	git("checkout", "--detach", "-q")
	if inspect("status", "").Branch != "(detached)" {
		t.Fatal("detached branch")
	}
	m.GitBin = filepath.Join(t.TempDir(), "missing")
	if _, err := m.Inspect(context.Background(), dir, dir, "status", ""); err == nil {
		t.Fatal("missing git accepted")
	}
	m.GitBin = "git"
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, ".git", "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Inspect(context.Background(), dir, dir, "status", ""); err == nil {
		t.Fatal("metadata symlink accepted")
	}
}

func TestInspectionRejectsExecutableConfiguration(t *testing.T) {
	for _, key := range []string{"filter.evil.clean", "include.path", "includeIf.gitdir:/.path", "core.sshCommand", "core.fsmonitor", "diff.external", "extensions.worktreeConfig", "core.worktree"} {
		if safeInspectionConfig(key) {
			t.Errorf("accepted %s", key)
		}
	}
	dir := t.TempDir()
	m, _ := NewManager(dir, "", false)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	git("init", "-q")
	git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "fixture")
	sentinel := filepath.Join(t.TempDir(), "executed")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt filter=evil\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "tracked filter fixture")
	git("config", "filter.evil.clean", "touch '"+sentinel+"'; cat")
	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"status", "diff", "history"} {
		if _, err := m.Inspect(context.Background(), dir, dir, view, ""); err == nil {
			t.Errorf("accepted filter for %s", view)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("filter executed: %v", err)
	}
}

func TestInspectManagedWorktreeAndLimits(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, "", false)
	g := graphWithRepo("")
	g.Spec.Workspace.Git = true
	base, err := m.Ensure(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	node := &domain.Node{ID: "worker", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}
	dir, branch, err := m.Replica(context.Background(), g, node, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Inspect(context.Background(), dir, base, "status", "")
	if err != nil || got.Branch != branch {
		t.Fatalf("worktree: %+v %v", got, err)
	}
	// Detached worktree metadata must not point at an unrelated repository.
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /outside/fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Inspect(context.Background(), dir, base, "status", ""); err == nil {
		t.Fatal("external gitdir accepted")
	}
	path := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(path, make([]byte, 4097), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMetadata(path); err == nil {
		t.Fatal("metadata limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Inspect(ctx, base, base, "status", ""); err == nil {
		t.Fatal("cancellation ignored")
	}
	// A huge tracked diff must fail, never return a plausible truncated patch.
	file := filepath.Join(base, "large.txt")
	if err := os.WriteFile(file, []byte("small\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.git(context.Background(), base, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := m.git(context.Background(), base, "commit", "-qm", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("large line\n", InspectionLimit/10+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Inspect(context.Background(), base, base, "diff", ""); !errors.Is(err, ErrInspectionLimit) {
		t.Fatalf("diff limit: %v", err)
	}
}

func TestInspectRejectsCommonDirectoryRedirection(t *testing.T) {
	m, _ := NewManager(t.TempDir(), "", false)
	g := graphWithRepo("")
	g.Spec.Workspace.Git = true
	dir, err := m.Ensure(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	other := graphWithRepo("")
	other.Metadata.ID = "other"
	other.Spec.Workspace.Git = true
	otherDir, err := m.Ensure(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.git(context.Background(), otherDir, "commit", "--allow-empty", "-qm", "exclusive-outside-marker"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "commondir"), []byte(filepath.Join(otherDir, ".git")), 0600); err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"status", "diff", "history"} {
		got, err := m.Inspect(context.Background(), dir, dir, view, "")
		if err == nil || got != nil {
			t.Fatalf("redirected %s: %+v %v", view, got, err)
		}
	}
}

func TestInspectionSnapshotSurvivesConcurrentMetadataReplacement(t *testing.T) {
	m, _ := NewManager(t.TempDir(), "", false)
	g := graphWithRepo("")
	g.Spec.Workspace.Git = true
	dir, err := m.Ensure(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{".gitattributes": "*.txt filter=evil\n", "target.txt": "before\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.git(context.Background(), dir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := m.git(context.Background(), dir, "commit", "-qm", "safe baseline"); err != nil {
		t.Fatal(err)
	}
	source, err := os.OpenRoot(filepath.Join(dir, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	snapshot, err := snapshotGitMetadata(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(snapshot)
	// Deterministic race: replace live metadata after capture, before executing Git.
	sentinel := filepath.Join(t.TempDir(), "executed")
	if _, err := m.git(context.Background(), dir, "config", "filter.evil.clean", "touch '"+sentinel+"'; cat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git", "commondir"), []byte(outside), 0600); err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"status", "diff", "history"} {
		got, err := m.inspectSnapshot(context.Background(), dir, snapshot, ".", view, "")
		if err != nil {
			t.Fatalf("%s: %v", view, err)
		}
		if view == "diff" && !strings.Contains(got.Content, "+after") {
			t.Fatal(got.Content)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("filter executed: %v", err)
	}
}

func TestSnapshotGitMetadataBounds(t *testing.T) {
	dir := t.TempDir()
	source, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	file, err := os.Create(filepath.Join(dir, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(metadataByteLimit + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := snapshotGitMetadata(context.Background(), source); err == nil {
		t.Fatal("accepted oversized metadata")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshotGitMetadata(ctx, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestInspectionDoesNotLoadExternalIncludedConfig(t *testing.T) {
	m, _ := NewManager(t.TempDir(), "", false)
	g := graphWithRepo("")
	g.Spec.Workspace.Git = true
	dir, err := m.Ensure(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "external-config")
	if err := os.WriteFile(outside, []byte("invalid external config fixture [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.git(context.Background(), dir, "config", "include.path", outside); err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"status", "diff", "history"} {
		if _, err := m.Inspect(context.Background(), dir, dir, view, ""); err == nil || !strings.Contains(err.Error(), "configuration is not supported") {
			t.Fatalf("external config was parsed for %s: %v", view, err)
		}
	}
}
