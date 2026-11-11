package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@localhost", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@localhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func writeT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// checkpointRepoT creates a committed repository and a manager over it.
func checkpointRepoT(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m, err := NewManager(filepath.Join(root, "ws"), "", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Root, "g")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "-q", "-b", "main")
	writeT(t, filepath.Join(dir, ".gitignore"), "*.log\n")
	writeT(t, filepath.Join(dir, "a.txt"), "one\n")
	writeT(t, filepath.Join(dir, "keep", "b.txt"), "two\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "init")
	return m, dir
}

func scopeT(dir string) CheckpointScope {
	return CheckpointScope{GraphID: "g", NodeID: "dev", Replica: 0, Dir: dir, Base: dir}
}

func treeFiles(t *testing.T, dir, rev string) string {
	t.Helper()
	return strings.TrimSpace(gitT(t, dir, "ls-tree", "-r", "--name-only", rev))
}

func TestRecordCheckpointLeavesIndexHeadAndBranchAlone(t *testing.T) {
	m, dir := checkpointRepoT(t)
	writeT(t, filepath.Join(dir, "a.txt"), "changed\n")
	writeT(t, filepath.Join(dir, "new.txt"), "new\n")
	writeT(t, filepath.Join(dir, "debug.log"), "ignored\n")
	writeT(t, filepath.Join(dir, ".weft", "dev", "r0", "notes.md"), "scratch\n")
	gitT(t, dir, "add", "new.txt") // a staged change must survive untouched
	head, status := gitT(t, dir, "rev-parse", "HEAD"), gitT(t, dir, "status", "--porcelain")
	cached := gitT(t, dir, "diff", "--cached", "--name-only")

	cp, created, err := m.RecordCheckpoint(context.Background(), scopeT(dir), CheckpointMeta{ExecutionID: "exe-1", Reason: CheckpointBeforeEdit, Tool: "filesystem.write"})
	if err != nil || !created {
		t.Fatalf("record: created=%v err=%v", created, err)
	}
	if cp.ID != "dev/r0/1" || cp.Ref != "refs/weft/checkpoints/g/dev/r0/00000001" || cp.ExecutionID != "exe-1" || cp.Tool != "filesystem.write" {
		t.Fatalf("checkpoint %+v", cp)
	}
	if got := gitT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatal("HEAD moved")
	}
	if got := gitT(t, dir, "status", "--porcelain"); got != status {
		t.Fatalf("status changed:\n%s\nwant\n%s", got, status)
	}
	if got := gitT(t, dir, "diff", "--cached", "--name-only"); got != cached {
		t.Fatalf("index changed: %q", got)
	}
	if branches := gitT(t, dir, "branch", "--list"); strings.TrimSpace(branches) != "* main" {
		t.Fatalf("branches %q", branches)
	}
	if files := treeFiles(t, dir, cp.Commit); files != ".gitignore\na.txt\nkeep/b.txt\nnew.txt" {
		t.Fatalf("checkpoint files:\n%s", files)
	}
	if got := gitT(t, dir, "show", cp.Commit+":a.txt"); got != "changed\n" {
		t.Fatalf("a.txt in checkpoint = %q", got)
	}
	if parent := gitT(t, dir, "rev-parse", cp.Commit+"^"); parent != head {
		t.Fatal("checkpoint parent is not HEAD")
	}
	list, err := m.Checkpoints(context.Background(), scopeT(dir))
	if err != nil || len(list) != 1 || list[0].ID != cp.ID || list[0].Reason != CheckpointBeforeEdit || list[0].ExecutionID != "exe-1" {
		t.Fatalf("list %+v %v", list, err)
	}
}

func TestRecordCheckpointIsANoOpWhenUnchanged(t *testing.T) {
	m, dir := checkpointRepoT(t)
	ctx := context.Background()
	first, created, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil || !created {
		t.Fatal(created, err)
	}
	// An ignored or scratch change is not a change of the checkpoint.
	writeT(t, filepath.Join(dir, "x.log"), "ignored\n")
	writeT(t, filepath.Join(dir, ".weft", "n.md"), "scratch\n")
	again, created, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("unchanged workspace recorded again: %+v created=%v err=%v", again, created, err)
	}
	writeT(t, filepath.Join(dir, "a.txt"), "two\n")
	next, created, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil || !created || next.Sequence != 2 {
		t.Fatalf("changed workspace: %+v created=%v err=%v", next, created, err)
	}
}

func TestRestoreCheckpointReturnsToTheRecordedFiles(t *testing.T) {
	m, dir := checkpointRepoT(t)
	ctx := context.Background()
	writeT(t, filepath.Join(dir, "untracked.txt"), "kept\n")
	good, _, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{ExecutionID: "exe-1", Reason: CheckpointBeforeEdit, Tool: "filesystem.write"})
	if err != nil {
		t.Fatal(err)
	}
	// A bad round: edit, delete, create files and folders, change modes.
	writeT(t, filepath.Join(dir, "a.txt"), "broken\n")
	if err := os.Remove(filepath.Join(dir, "keep", "b.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(dir, "deep", "er", "junk.txt"), "junk\n")
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(dir, "build.log"), "ignored stays\n")
	status := gitT(t, dir, "diff", "--cached", "--name-only")

	res, err := m.RestoreCheckpoint(ctx, scopeT(dir), "", CheckpointMeta{ExecutionID: "exe-2", Tool: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Restored.ID != good.ID || res.Saved.Reason != CheckpointBeforeRestore || res.Saved.Sequence != 2 || res.Removed != 2 || res.Written != 3 {
		t.Fatalf("result %+v", res)
	}
	if got := readT(t, filepath.Join(dir, "a.txt")); got != "one\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if readT(t, filepath.Join(dir, "keep", "b.txt")) != "two\n" || readT(t, filepath.Join(dir, "untracked.txt")) != "kept\n" {
		t.Fatal("deleted files not restored")
	}
	if exists(filepath.Join(dir, "deep")) || exists(filepath.Join(dir, "link")) {
		t.Fatal("files created after the checkpoint survived the restore")
	}
	if readT(t, filepath.Join(dir, "build.log")) != "ignored stays\n" {
		t.Fatal("ignored file touched")
	}
	if got := gitT(t, dir, "diff", "--cached", "--name-only"); got != status {
		t.Fatal("restore changed the index")
	}
	// The restore is reversible: the state before it is a checkpoint.
	if _, err := m.RestoreCheckpoint(ctx, scopeT(dir), res.Saved.ID, CheckpointMeta{}); err != nil {
		t.Fatal(err)
	}
	if readT(t, filepath.Join(dir, "deep", "er", "junk.txt")) != "junk\n" || readT(t, filepath.Join(dir, "a.txt")) != "broken\n" {
		t.Fatal("undoing the restore did not bring the later state back")
	}
	if target, err := os.Readlink(filepath.Join(dir, "link")); err != nil || target != "a.txt" {
		t.Fatalf("symlink not restored: %q %v", target, err)
	}
	if exists(filepath.Join(dir, "keep", "b.txt")) {
		t.Fatal("file deleted in the later state came back")
	}
}

func TestRestoreCheckpointKeepsExecutableBitAndNeverWritesThroughLinks(t *testing.T) {
	m, dir := checkpointRepoT(t)
	ctx := context.Background()
	outside := t.TempDir()
	writeT(t, filepath.Join(dir, "bin", "run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(dir, "bin", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	good, _, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil {
		t.Fatal(err)
	}
	// The agent replaces bin/ with a link to a directory outside the repository.
	if err := os.RemoveAll(filepath.Join(dir, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RestoreCheckpoint(ctx, scopeT(dir), good.ID, CheckpointMeta{}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(outside, "run.sh")) {
		t.Fatal("restore wrote through a link outside the repository")
	}
	info, err := os.Lstat(filepath.Join(dir, "bin", "run.sh"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("run.sh not restored as executable: %v %v", info, err)
	}
	// The newest checkpoint is the state before the restore, so this records
	// again; its tree must be the restored one.
	cp, _, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil || cp.Tree != good.Tree {
		t.Fatalf("restored tree differs from the checkpoint: %+v err=%v", cp, err)
	}
}

func TestCheckpointLinksAreRecordedNotFollowed(t *testing.T) {
	m, dir := checkpointRepoT(t)
	secret := filepath.Join(t.TempDir(), "secret")
	writeT(t, secret, "host secret\n")
	if err := os.Symlink(secret, filepath.Join(dir, "leak")); err != nil {
		t.Fatal(err)
	}
	cp, _, err := m.RecordCheckpoint(context.Background(), scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil {
		t.Fatal(err)
	}
	if got := gitT(t, dir, "cat-file", "-p", cp.Commit+":leak"); got != secret {
		t.Fatalf("link stored as %q", got)
	}
}

func TestCheckpointRunsNoRepositoryProgramsOrFilters(t *testing.T) {
	m, dir := checkpointRepoT(t)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "evil.sh")
	writeT(t, script, "#!/bin/sh\ntouch "+marker+"\ncat\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "config", "filter.evil.clean", script)
	gitT(t, dir, "config", "filter.evil.smudge", script)
	gitT(t, dir, "config", "core.fsmonitor", script)
	gitT(t, dir, "config", "commit.gpgSign", "true")
	gitT(t, dir, "config", "gpg.program", script)
	writeT(t, filepath.Join(dir, ".git", "hooks", "reference-transaction"), "#!/bin/sh\ntouch "+marker+"\n")
	if err := os.Chmod(filepath.Join(dir, ".git", "hooks", "reference-transaction"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(dir, ".gitattributes"), "* filter=evil\n")
	writeT(t, filepath.Join(dir, "a.txt"), "changed\n")
	ctx := context.Background()
	cp, _, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(dir, "a.txt"), "again\n")
	if _, err := m.RestoreCheckpoint(ctx, scopeT(dir), cp.ID, CheckpointMeta{}); err != nil {
		t.Fatal(err)
	}
	if exists(marker) {
		t.Fatal("a repository-configured program ran during checkpoint or restore")
	}
	if readT(t, filepath.Join(dir, "a.txt")) != "changed\n" {
		t.Fatal("restore did not apply")
	}
}

func TestCheckpointsAreBoundedPerScope(t *testing.T) {
	m, dir := checkpointRepoT(t)
	m.MaxCheckpoints = 3
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		writeT(t, filepath.Join(dir, "a.txt"), strings.Repeat("x", i+1))
		if _, created, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit}); err != nil || !created {
			t.Fatal(created, err)
		}
	}
	list, err := m.Checkpoints(ctx, scopeT(dir))
	if err != nil || len(list) != 3 || list[0].Sequence != 5 || list[2].Sequence != 3 {
		t.Fatalf("kept %+v %v", list, err)
	}
	if refs := strings.Count(gitT(t, dir, "for-each-ref", CheckpointRefPrefix), "\n"); refs != 3 {
		t.Fatalf("%d refs kept", refs)
	}
}

func TestCheckpointScopeOfASubdirectoryLeavesTheRestAlone(t *testing.T) {
	m, dir := checkpointRepoT(t)
	ctx := context.Background()
	scope := CheckpointScope{GraphID: "g", NodeID: "web", Replica: 1, Dir: filepath.Join(dir, "keep"), Base: dir}
	good, _, err := m.RecordCheckpoint(ctx, scope, CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(dir, "a.txt"), "other node\n")
	writeT(t, filepath.Join(dir, "keep", "b.txt"), "mine\n")
	writeT(t, filepath.Join(dir, "keep", "c.txt"), "mine too\n")
	res, err := m.RestoreCheckpoint(ctx, scope, good.ID, CheckpointMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || res.Removed != 1 || readT(t, filepath.Join(dir, "keep", "b.txt")) != "two\n" || exists(filepath.Join(dir, "keep", "c.txt")) {
		t.Fatalf("scope not restored: %+v", res)
	}
	if readT(t, filepath.Join(dir, "a.txt")) != "other node\n" {
		t.Fatal("restore touched a file outside its scope")
	}
	if all, err := m.GraphCheckpoints(ctx, dir, "g"); err != nil || len(all) != 2 || all[0].NodeID != "web" || all[0].Replica != 1 {
		t.Fatalf("graph checkpoints %+v %v", all, err)
	}
}

func TestCheckpointOfAWorktreeReplicaSharesTheGraphRefs(t *testing.T) {
	m, err := NewManager(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "wt"}, Spec: domain.GraphSpec{Workspace: domain.WorkspaceSpec{Git: true}}}
	n := &domain.Node{ID: "dev", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}
	ctx := context.Background()
	dir, _, err := m.Replica(ctx, g, n, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want, err := m.ReplicaDir(g, n, 2); err != nil || want != dir {
		t.Fatalf("ReplicaDir %q, Replica %q (%v)", want, dir, err)
	}
	base, _ := m.GraphDir(g)
	writeT(t, filepath.Join(dir, "work.txt"), "w\n")
	if _, _, err := m.RecordCheckpoint(ctx, CheckpointScope{GraphID: "wt", NodeID: "dev", Replica: 2, Dir: dir, Base: base}, CheckpointMeta{Reason: CheckpointBeforeEdit}); err != nil {
		t.Fatal(err)
	}
	list, err := m.GraphCheckpoints(ctx, base, "wt")
	if err != nil || len(list) != 1 || list[0].ID != "dev/r2/1" {
		t.Fatalf("graph checkpoints %+v %v", list, err)
	}
	if got := treeFiles(t, base, list[0].Commit); !strings.Contains(got, "work.txt") {
		t.Fatalf("worktree file missing: %s", got)
	}
}

func TestCheckpointOfAnUnbornRepository(t *testing.T) {
	m, err := NewManager(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Root, "g")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "-q", "-b", "main")
	writeT(t, filepath.Join(dir, "first.txt"), "1\n")
	cp, created, err := m.RecordCheckpoint(context.Background(), scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit})
	if err != nil || !created || treeFiles(t, dir, cp.Commit) != "first.txt" {
		t.Fatalf("%+v %v %v", cp, created, err)
	}
}

func TestCheckpointErrors(t *testing.T) {
	ctx := context.Background()
	m, dir := checkpointRepoT(t)
	plain := t.TempDir()
	if _, _, err := m.RecordCheckpoint(ctx, scopeT(plain), CheckpointMeta{}); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("plain directory: %v", err)
	}
	// A folder inside some other repository is never checkpointed as that repository.
	nested := filepath.Join(dir, "keep")
	if _, _, err := m.RecordCheckpoint(ctx, scopeT(nested), CheckpointMeta{}); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("enclosing repository: %v", err)
	}
	if _, err := m.RestoreCheckpoint(ctx, scopeT(dir), "", CheckpointMeta{}); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("restore without checkpoints: %v", err)
	}
	if _, _, err := m.RecordCheckpoint(ctx, scopeT(dir), CheckpointMeta{Reason: CheckpointBeforeEdit}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RestoreCheckpoint(ctx, scopeT(dir), "dev/r0/9", CheckpointMeta{}); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := m.RestoreCheckpoint(ctx, scopeT(dir), "other/r0/1", CheckpointMeta{}); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("id of another scope: %v", err)
	}
	bad := scopeT(dir)
	bad.NodeID = "../x"
	if _, _, err := m.RecordCheckpoint(ctx, bad, CheckpointMeta{}); err == nil {
		t.Fatal("invalid node accepted")
	}
	if _, _, err := m.RecordCheckpoint(ctx, scopeT(filepath.Join(dir, "missing")), CheckpointMeta{}); err == nil {
		t.Fatal("missing directory accepted")
	}
	if list, err := m.GraphCheckpoints(ctx, plain, "g"); err != nil || len(list) != 0 {
		t.Fatalf("non-repository base: %v %v", list, err)
	}
}

func TestParseCheckpointID(t *testing.T) {
	node, replica, seq, err := ParseCheckpointID("dev-1/r3/12")
	if err != nil || node != "dev-1" || replica != 3 || seq != 12 {
		t.Fatal(node, replica, seq, err)
	}
	for _, id := range []string{"", "dev", "dev/3/1", "dev/r-1/1", "dev/r1/0", "dev/r1/01", "Dev/r1/1", "dev/r1/1/x", "../r1/1", "dev/r01/1"} {
		if _, _, _, err := ParseCheckpointID(id); err == nil {
			t.Errorf("%q accepted", id)
		}
	}
	if CheckpointID("dev", 3, 12) != "dev/r3/12" {
		t.Fatal("CheckpointID")
	}
}
