package projectconfig

import (
	"errors"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterAndCreateIndependentWorktrees(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, b)
		}
		return strings.TrimSpace(string(b))
	}
	run("init", "-q")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(root, "state.db")
	s, err := sqlstore.OpenSQLite(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	p, err := (Manager{Store: s}).Create(t.Context(), Input{Name: "Fixture", Path: repo}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	oldRevision := p.Revision
	p, err = (Manager{Store: s}).SetExecutionPreference(t.Context(), p.ID, "local", p.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (Manager{Store: s}).SetExecutionPreference(t.Context(), p.ID, "sandbox", oldRevision, "admin"); err == nil {
		t.Fatal("stale preference accepted")
	}
	checks := []domain.Check{{Name: "test", Command: "go test ./..."}}
	if _, err = (Manager{Store: s}).SetChecks(t.Context(), p.ID, []domain.Check{{Name: "test", Command: ""}}, p.Revision, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid checks accepted: %v", err)
	}
	if _, err = (Manager{Store: s}).SetChecks(t.Context(), p.ID, checks, oldRevision, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale checks accepted: %v", err)
	}
	if _, err = (Manager{Store: s}).SetChecks(t.Context(), "prj_missing", checks, 1, "admin"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	p, err = (Manager{Store: s}).SetChecks(t.Context(), p.ID, checks, p.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlstore.OpenSQLite(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err = (Manager{Store: s}).Get(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ExecutionMode != "local" || p.Revision <= oldRevision {
		t.Fatal("preference not retained", p)
	}
	if len(p.Checks) != 1 || p.Checks[0] != checks[0] {
		t.Fatal("checks not retained across reopen", p.Checks)
	}
	workspace := filepath.Join(root, "workspaces")
	if err = os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := CreateWorktree(t.Context(), p, p.Head, workspace, "task-run-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateWorktree(t.Context(), p, p.Head, workspace, "task-run-b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("shared workspace")
	}
	if err = os.WriteFile(filepath.Join(a, "change"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(b, "change")); !os.IsNotExist(err) {
		t.Fatal("cross task write", err)
	}
	if data, err := os.ReadFile(filepath.Join(repo, "dirty.txt")); err != nil || string(data) != "keep" {
		t.Fatal("dirty source changed", err)
	}
	if _, err = CreateWorktree(t.Context(), p, p.Head, workspace, "task-run-a"); err == nil {
		t.Fatal("replaced existing worktree")
	}
	if Verify(t.Context(), p, "HEAD") == nil {
		t.Fatal("accepted moving ref")
	}
	if _, _, err = Inspect(t.Context(), workspace); err == nil {
		t.Fatal("accepted non repository")
	}
	if run("rev-parse", "HEAD") != p.Head {
		t.Fatal("source HEAD changed")
	}
}

func TestImportedWorktreeIgnoresHostCheckoutFilters(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	for name, data := range map[string]string{".gitattributes": "*.txt filter=host-only\n", "source.txt": "original\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
	head := run("rev-parse", "HEAD")
	config := filepath.Join(root, "host.gitconfig")
	if err := os.WriteFile(config, []byte("[filter \"host-only\"]\n\tsmudge = false\n\trequired = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	workspace := filepath.Join(root, "workspaces")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	p := Project{ID: "fixture", Input: Input{Path: canonical}, Head: head, ImportID: "fixture-import"}
	path, err := CreateWorktree(t.Context(), &p, head, workspace, "task-run-imported")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(path, "source.txt"))
	if err != nil || string(data) != "original\n" {
		t.Fatalf("unexpected checkout: %q, %v", data, err)
	}
}
