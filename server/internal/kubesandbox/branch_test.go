package kubesandbox

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchTransferScripts(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "worker")
	t.Setenv("GIT_AUTHOR_EMAIL", "worker@localhost")
	t.Setenv("GIT_COMMITTER_NAME", "worker")
	t.Setenv("GIT_COMMITTER_EMAIL", "worker@localhost")
	t.Setenv("WEFT_POD_UID", "test-uid")
	const branch = "weft/test/worker-0"
	graph := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v", data, err)
		}
		return strings.TrimSpace(string(data))
	}
	transfer := func(dir, script, uid, commit, old string, bundle []byte, want int) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "sh", "-ec", script, "weft-git", uid, branch, commit, old)
		cmd.Dir = dir
		cmd.Stdin = bytes.NewReader(bundle)
		var out, diagnostic bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &diagnostic
		err := cmd.Run()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want {
			t.Fatalf("want exit %d, got %v: %s", want, err, diagnostic.Bytes())
		}
		return out.Bytes()
	}
	git(graph, "init", "-q", "-b", "main")
	git(graph, "commit", "-qm", "base", "--allow-empty")
	base := git(graph, "rev-parse", "HEAD")
	transfer(graph, exportBranch, "other", "", "", nil, 70)
	seed := transfer(graph, exportBranch, "test-uid", "", "", nil, 0)
	stage := t.TempDir()
	transfer(stage, seedBranch, "test-uid", "", "", seed, 0)
	clone := filepath.Join(stage, "checkout")
	git(clone, "commit", "-qm", "first", "--allow-empty")
	first := git(clone, "rev-parse", "HEAD")
	firstBundle := transfer(clone, exportBranch, "test-uid", first, "", nil, 0)
	transfer(graph, publishBranch, "test-uid", base, "", firstBundle, 72)
	transfer(graph, publishBranch, "test-uid", first, "", firstBundle, 0)
	transfer(graph, publishBranch, "test-uid", first, "", firstBundle, 0)
	git(clone, "reset", "--hard", base)
	git(clone, "commit", "-qm", "diverged", "--allow-empty")
	diverged := git(clone, "rev-parse", "HEAD")
	other := transfer(clone, exportBranch, "test-uid", diverged, "", nil, 0)
	transfer(graph, publishBranch, "test-uid", diverged, first, other, 73)
	if git(graph, "rev-parse", branch) != first || git(graph, "rev-parse", "HEAD") != base {
		t.Fatal("non-fast-forward changed graph history")
	}
	git(clone, "reset", "--hard", first)
	git(clone, "commit", "-qm", "second", "--allow-empty")
	next := git(clone, "rev-parse", "HEAD")
	forward := transfer(clone, exportBranch, "test-uid", next, "", nil, 0)
	git(graph, "checkout", "-q", branch)
	transfer(graph, publishBranch, "test-uid", next, first, forward, 73)
	if git(graph, "rev-parse", "HEAD") != first {
		t.Fatal("moved checked-out branch")
	}
}

func TestBundleSizeLimit(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "bundle")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := limitedBundle{file: f, remaining: 4}
	if n, err := w.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := w.Write([]byte("5")); n != 0 || err == nil || !errors.Is(w.err, err) {
		t.Fatal(n, err)
	}
}
