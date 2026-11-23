package projectconfig

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestTaskCheckoutBindingValidation(t *testing.T) {
	p := &Project{ID: "prj_fixture", Input: Input{Path: "/source"}}
	b := TaskCheckout{ProjectID: p.ID, GraphID: "task-run-fixture", Base: strings.Repeat("a", 40), Path: "/work/task-run-fixture", ArchivePath: "/work/.task-checkouts/task-run-fixture.git"}
	if !b.valid(p, b.GraphID, b.Path) {
		t.Fatal("valid binding refused")
	}
	for _, change := range []func(*TaskCheckout){func(b *TaskCheckout) { b.ProjectID = "prj_other" }, func(b *TaskCheckout) { b.GraphID = "task-run-other" }, func(b *TaskCheckout) { b.Base = "HEAD" }, func(b *TaskCheckout) { b.Path = "/source" }, func(b *TaskCheckout) { b.ArchivePath = "/source/.git" }} {
		copy := b
		change(&copy)
		if copy.valid(p, b.GraphID, b.Path) {
			t.Fatal("changed provenance accepted")
		}
	}
	if b.valid(nil, b.GraphID, b.Path) || b.valid(p, "../fixture", b.Path) || b.valid(p, b.GraphID, "/other/task-run-fixture") {
		t.Fatal("invalid binding accepted")
	}
}

func checkoutFixture(t *testing.T) (Manager, *Project, string, func(string, ...string) string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := sqlstore.OpenSQLite(t.Context(), filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-C", dir}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1"}
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("fixture Git failed: %v: %s", e, out)
		}
		return strings.TrimSpace(string(out))
	}
	repo := filepath.Join(root, "source")
	if err = os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	run(repo, "init", "-q", "-b", "main")
	for _, value := range []string{"ancestor\n", "reviewed\n"} {
		if err = os.WriteFile(filepath.Join(repo, "file.txt"), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		run(repo, "add", ".")
		run(repo, "commit", "-qm", value)
	}
	m := Manager{Store: s}
	p, err := m.Create(t.Context(), Input{Name: "Fixture", Path: repo}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return m, p, root, run
}

func TestTaskCheckoutOwnsHistoryWithoutSourceMetadata(t *testing.T) {
	m, p, root, run := checkoutFixture(t)
	base := p.Head
	// Unrelated refs, later changes, dirty files and executable config do not
	// belong to the reviewed task snapshot.
	if err := os.WriteFile(filepath.Join(p.Path, "file.txt"), []byte("later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(p.Path, "commit", "-qam", "later")
	later := run(p.Path, "rev-parse", "HEAD")
	run(p.Path, "branch", "private-unrelated")
	run(p.Path, "remote", "add", "private", "https://credential.example.invalid/private.git")
	run(p.Path, "config", "filter.fixture.required", "true")
	if err := os.WriteFile(filepath.Join(p.Path, "dirty.txt"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	sourceRefs := run(p.Path, "show-ref")
	sourceConfig, err := os.ReadFile(filepath.Join(p.Path, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	run(p.Path, "replace", base, later)
	dir, err := m.CreateTaskCheckout(t.Context(), p, base, root, "task-run-private", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got := run(dir, "rev-parse", "HEAD"); got != base {
		t.Fatalf("base moved: %s", got)
	}
	if got := run(dir, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatal("ancestry lost", got)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "file.txt")); err != nil || string(got) != "reviewed\n" {
		t.Fatalf("wrong snapshot: %q %v", got, err)
	}
	if info, err := os.Lstat(filepath.Join(dir, ".git")); err != nil || !info.IsDir() {
		t.Fatal("metadata not self contained", err)
	}
	for _, path := range []string{"objects/info/alternates", "objects/info/http-alternates", "commondir", "hooks", "refs/remotes", "refs/replace"} {
		if _, err := os.Lstat(filepath.Join(dir, ".git", path)); !os.IsNotExist(err) {
			t.Fatalf("source metadata copied: %s", path)
		}
	}
	if got := run(dir, "remote"); got != "" {
		t.Fatal("source remotes copied")
	}
	if got := run(dir, "for-each-ref", "--format=%(refname)"); got != "refs/heads/weft/task-run-private" {
		t.Fatal("unrelated refs copied", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "dirty.txt")); !os.IsNotExist(err) {
		t.Fatal("dirty source copied")
	}
	if got := run(p.Path, "show-ref"); !strings.Contains(got, sourceRefs) || run(p.Path, "rev-parse", "HEAD") != later {
		t.Fatal("source refs changed")
	}
	if got, _ := os.ReadFile(filepath.Join(p.Path, ".git", "config")); string(got) != string(sourceConfig) {
		t.Fatal("source config changed")
	}
	// Changing a private object must not mutate a source object through a hardlink.
	if err := filepath.WalkDir(filepath.Join(dir, ".git", "objects"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			if err := os.WriteFile(path, []byte("fixture-corrupt"), 0600); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := run(p.Path, "show", base+":file.txt"); got != "reviewed" {
		t.Fatal("source object changed")
	}
	if _, err = m.CreateTaskCheckout(t.Context(), p, base, root, "task-run-private", "admin"); err == nil {
		t.Fatal("checkout replaced")
	}
}

func TestTaskCheckoutInspectionCheckpointsAndRetainedCleanup(t *testing.T) {
	m, p, root, run := checkoutFixture(t)
	dir, err := m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-retain", "admin")
	if err != nil {
		t.Fatal(err)
	}
	wm, err := workspace.NewManager(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	base, err := m.TaskInspectionBase(t.Context(), p, "task-run-retain", dir)
	if err != nil || base != dir {
		t.Fatal(base, err)
	}
	if result, err := wm.Inspect(t.Context(), dir, base, "status", ""); err != nil || !result.Repository {
		t.Fatal(result, err)
	}
	if err = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("checkpoint-only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cp, created, err := wm.RecordCheckpoint(t.Context(), workspace.CheckpointScope{GraphID: "task-run-retain", NodeID: "worker", Dir: dir, Base: dir}, workspace.CheckpointMeta{Reason: workspace.CheckpointBeforeEdit})
	if err != nil || !created {
		t.Fatal(cp, created, err)
	}
	run(dir, "checkout", "--", "file.txt")
	v, err := m.PreviewTaskCleanup(t.Context(), p, "task-run-retain", dir)
	if err != nil || !v.Clean || v.RetainedRepository == "" {
		t.Fatal(v, err)
	}
	// A checkpoint added after review invalidates the deletion fence even
	// when HEAD, branch and all working files stay identical.
	run(dir, "update-ref", "refs/weft/checkpoints/extra", p.Head)
	if err = m.RemoveReviewedTaskCheckout(t.Context(), p, "task-run-retain", dir, v.Fingerprint); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale metadata review accepted", err)
	}
	v, err = m.PreviewTaskCleanup(t.Context(), p, "task-run-retain", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, ".git", "index.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.PreviewTaskCleanup(t.Context(), p, "task-run-retain", dir); err == nil {
		t.Fatal("locked repository accepted")
	}
	if err = os.Remove(filepath.Join(dir, ".git", "index.lock")); err != nil {
		t.Fatal(err)
	}
	if err = m.RemoveReviewedTaskCheckout(t.Context(), p, "task-run-retain", dir, v.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("checkout remains", err)
	}
	if got := run(root, "--git-dir="+v.RetainedRepository, "rev-parse", cp.Ref); got != cp.Commit {
		t.Fatal("checkpoint lost", got)
	}
	if got := run(root, "--git-dir="+v.RetainedRepository, "show", cp.Ref+":file.txt"); got != "checkpoint-only" {
		t.Fatal("checkpoint content lost", got)
	}
	if got := run(root, "--git-dir="+v.RetainedRepository, "rev-parse", "refs/heads/weft/task-run-retain"); got != p.Head {
		t.Fatal("branch lost")
	}
	if got := run(p.Path, "rev-parse", "HEAD"); got != p.Head {
		t.Fatal("source changed")
	}
}

func TestTaskCheckoutRejectsUnboundRedirectedAndIncompleteTargets(t *testing.T) {
	m, p, root, run := checkoutFixture(t)
	dir, err := m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-guards", "admin")
	if err != nil {
		t.Fatal(err)
	}
	other := *p
	other.ID = "prj_other"
	if _, err = m.TaskInspectionBase(t.Context(), &other, "task-run-guards", dir); err == nil {
		t.Fatal("different project accepted")
	}
	if _, err = m.PreviewTaskCleanup(t.Context(), p, "task-run-unbound", dir); err == nil {
		t.Fatal("unbound private repo accepted")
	}
	if err = os.WriteFile(filepath.Join(dir, ".git", "commondir"), []byte(filepath.Join(p.Path, ".git")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.PreviewTaskCleanup(t.Context(), p, "task-run-guards", dir); err == nil {
		t.Fatal("redirect accepted")
	}
	if err = os.Remove(filepath.Join(dir, ".git", "commondir")); err != nil {
		t.Fatal(err)
	}
	run(dir, "worktree", "add", "-b", "fixture-linked", filepath.Join(root, "linked"))
	if _, err = m.PreviewTaskCleanup(t.Context(), p, "task-run-guards", dir); err == nil {
		t.Fatal("linked worktree repository accepted")
	}
	partial := filepath.Join(root, "task-run-partial")
	if err = createIndependentCheckout(t.Context(), p.Path, strings.Repeat("a", 40), partial, "weft/task-run-partial"); err == nil {
		t.Fatal("missing objects accepted")
	}
	if _, err = os.Stat(partial); err != nil {
		t.Fatal("partial preparation deleted", err)
	}
	if _, err = m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-partial", "admin"); err == nil {
		t.Fatal("partial checkout replaced")
	}
}

func TestTaskCheckoutBindingPersistsAndFailureNeverReplacesData(t *testing.T) {
	m, p, root, run := checkoutFixture(t)
	dir, err := m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-persist", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlstore.OpenSQLite(t.Context(), filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	m = Manager{Store: reopened}
	if base, err := m.TaskInspectionBase(t.Context(), p, "task-run-persist", dir); err != nil || base != dir {
		t.Fatal(base, err)
	}
	archive := filepath.Join(root, ".task-checkouts", "task-run-existing.git")
	if err = os.MkdirAll(archive, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-existing", "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("archive overwritten", err)
	}
	if _, err = m.GetTaskCheckout(t.Context(), "task-run-existing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("conflicting archive created binding", err)
	}
	// Fail object transfer only, after verification and the durable binding.
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrappers := filepath.Join(root, "bin")
	if err = os.Mkdir(wrappers, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(wrappers, "git"), []byte("#!/bin/sh\nfor arg do if [ \"$arg\" = pack-objects ]; then exit 1; fi; done\nexec "+gitBin+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrappers+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err = m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-transfer", "admin"); err == nil {
		t.Fatal("failed transfer accepted")
	}
	b, err := m.GetTaskCheckout(t.Context(), "task-run-transfer")
	if err != nil {
		t.Fatal("durable intent lost", err)
	}
	if _, err = os.Stat(b.Path); err != nil {
		t.Fatal("partial checkout removed", err)
	}
	if _, err = m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-transfer", "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("partial transfer replaced", err)
	}
	if got := run(p.Path, "rev-parse", "HEAD"); got != p.Head {
		t.Fatal("failed transfer changed source")
	}
	if err = m.Store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CreateTaskCheckout(t.Context(), p, p.Head, root, "task-run-storefail", "admin"); err == nil {
		t.Fatal("storage failure accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "task-run-storefail")); !os.IsNotExist(err) {
		t.Fatal("checkout created before durable binding", err)
	}
}

func TestTaskCheckoutNativeReplicaCleanupAndScopeFences(t *testing.T) {
	m, p, root, run := checkoutFixture(t)
	const graphID = "task-run-replicas"
	dir, err := m.CreateTaskCheckout(t.Context(), p, p.Head, root, graphID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	replicas := filepath.Join(root, ".worktrees", graphID)
	if err = os.MkdirAll(replicas, 0700); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(replicas, "worker-0"), filepath.Join(replicas, "worker-1")
	run(dir, "worktree", "add", "-b", "weft/replica-a", a)
	run(dir, "worktree", "add", "-b", "weft/replica-b", b)
	if _, err = m.PreviewTaskCleanup(t.Context(), p, graphID, dir); err == nil {
		t.Fatal("base removed while replicas reference it")
	}
	gitA, err := os.ReadFile(filepath.Join(a, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitB, err := os.ReadFile(filepath.Join(b, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(a, ".git"), gitB, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.PreviewTaskCleanup(t.Context(), p, graphID, a); err == nil {
		t.Fatal("cross replica pointer accepted")
	}
	if err = os.WriteFile(filepath.Join(a, ".git"), gitA, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := m.PreviewTaskCleanup(t.Context(), p, graphID, a)
	if err != nil || !v.Clean {
		t.Fatal(v, err)
	}
	run(dir, "update-ref", "refs/weft/checkpoints/replica", p.Head)
	if err = m.RemoveReviewedTaskCheckout(t.Context(), p, graphID, a, v.Fingerprint); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale replica review accepted", err)
	}
	for _, replica := range []string{a, b} {
		v, err = m.PreviewTaskCleanup(t.Context(), p, graphID, replica)
		if err != nil {
			t.Fatal(err)
		}
		if err = m.RemoveReviewedTaskCheckout(t.Context(), p, graphID, replica, v.Fingerprint); err != nil {
			t.Fatal(err)
		}
	}
	if got := run(dir, "rev-parse", "refs/heads/weft/replica-a"); got != p.Head {
		t.Fatal("replica branch lost")
	}
	v, err = m.PreviewTaskCleanup(t.Context(), p, graphID, dir)
	if err != nil || !v.Clean {
		t.Fatal(v, err)
	}
	if err = m.RemoveReviewedTaskCheckout(t.Context(), p, graphID, dir, v.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if got := run(root, "--git-dir="+v.RetainedRepository, "rev-parse", "refs/weft/checkpoints/replica"); got != p.Head {
		t.Fatal("replica checkpoint lost")
	}
}

func TestTaskCheckoutCleanupNeverRunsAgentFiltersOrIncludes(t *testing.T) {
	for _, replica := range []bool{false, true} {
		for _, kind := range []string{"filter", "include"} {
			t.Run(fmt.Sprintf("replica=%v/%s", replica, kind), func(t *testing.T) {
				m, p, root, run := checkoutFixture(t)
				const graphID = "task-run-inert"
				dir, err := m.CreateTaskCheckout(t.Context(), p, p.Head, root, graphID, "admin")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt filter=unsafe\n"), 0600); err != nil {
					t.Fatal(err)
				}
				run(dir, "add", ".")
				run(dir, "commit", "-qm", "attributes")
				target := dir
				if replica {
					target = filepath.Join(root, ".worktrees", graphID, "worker-0")
					if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
						t.Fatal(err)
					}
					run(dir, "worktree", "add", "-b", "weft/replica", target)
				}
				marker := filepath.Join(root, "host-execution-must-not-run")
				if kind == "filter" {
					run(dir, "config", "filter.unsafe.clean", "touch "+marker+"; cat")
					run(dir, "config", "filter.unsafe.required", "true")
				} else {
					include := filepath.Join(root, "included-config")
					if err = os.WriteFile(include, []byte("[filter \"unsafe\"]\nclean = touch "+marker+"; cat\n"), 0600); err != nil {
						t.Fatal(err)
					}
					run(dir, "config", "include.path", include)
				}
				if err = os.WriteFile(filepath.Join(target, "file.txt"), []byte("changed\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err = m.PreviewTaskCleanup(t.Context(), p, graphID, target); err == nil {
					t.Fatal("executable config accepted")
				}
				if _, err = os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("host filter executed", err)
				}
				if _, err = os.Stat(filepath.Join(target, ".git")); err != nil {
					t.Fatal("failed review removed checkout", err)
				}
			})
		}
	}
}

func TestTaskCheckoutCleanupRefusesSpecialMetadataAndArchiveCollision(t *testing.T) {
	m, p, root, run := checkoutFixture(t)
	const graphID = "task-run-special"
	dir, err := m.CreateTaskCheckout(t.Context(), p, p.Head, root, graphID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := m.PreviewTaskCleanup(t.Context(), p, graphID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(preview.RetainedRepository, 0700); err != nil {
		t.Fatal(err)
	}
	if err = m.RemoveReviewedTaskCheckout(t.Context(), p, graphID, dir, preview.Fingerprint); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("existing archive replaced", err)
	}
	if _, err = os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal("archive failure removed metadata", err)
	}
	if err = os.Remove(preview.RetainedRepository); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Mkfifo(filepath.Join(dir, ".git", "fixture-fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.PreviewTaskCleanup(t.Context(), p, graphID, dir); err == nil {
		t.Fatal("FIFO metadata accepted")
	}
	if err = os.Remove(filepath.Join(dir, ".git", "fixture-fifo")); err != nil {
		t.Fatal(err)
	}
	replica := filepath.Join(root, ".worktrees", graphID, "worker-0")
	if err = os.MkdirAll(filepath.Dir(replica), 0700); err != nil {
		t.Fatal(err)
	}
	run(dir, "worktree", "add", "-b", "weft/replica", replica)
	b, err := m.GetTaskCheckout(t.Context(), graphID)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := taskReplicaMetadata(b, replica)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(metadata, "gitdir")); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Mkfifo(filepath.Join(metadata, "gitdir"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.PreviewTaskCleanup(t.Context(), p, graphID, replica); err == nil {
		t.Fatal("FIFO backlink accepted")
	}
}
