package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

// fakeGit records its arguments and "clones" by creating the target's .git.
func fakeGit(t *testing.T) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "git"), filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$*" >> ` + log + `
for last; do :; done
case " $* " in *" clone "*) mkdir -p "$last/.git" ;; esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func graphWithRepo(repo string) *domain.Graph {
	return &domain.Graph{Metadata: domain.GraphMetadata{ID: "g"}, Spec: domain.GraphSpec{Workspace: domain.WorkspaceSpec{Repo: repo}}}
}

func TestEnsureClonesOnlyRemoteRepositories(t *testing.T) {
	m, err := NewManager(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	bin, log := fakeGit(t)
	m.GitBin = bin

	dir, err := m.Ensure(context.Background(), graphWithRepo("https://github.com/woliveiras/bookaneer.git"))
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	got := strings.TrimSpace(string(calls))
	for _, want := range []string{"protocol.allow=never", "protocol.https.allow=always", "clone -- https://github.com/woliveiras/bookaneer.git " + dir} {
		if !strings.Contains(got, want) {
			t.Errorf("clone call %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "protocol.file.allow") || strings.Contains(got, "protocol.ext.allow") {
		t.Errorf("local transports must stay disabled: %q", got)
	}
}

func TestEnsureRefusesLocalRepositoryPaths(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, "", false)
	bin, log := fakeGit(t)
	m.GitBin = bin
	for _, repo := range []string{filepath.Join(root, "other-graph"), "file:///etc", "ext::sh -c id", "--upload-pack=id"} {
		_, err := m.Ensure(context.Background(), graphWithRepo(repo))
		if err == nil || !strings.Contains(err.Error(), "not a remote repository URL") {
			t.Errorf("repo %q: want refusal, got %v", repo, err)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		calls, _ := os.ReadFile(log)
		t.Fatalf("git must not run for refused repositories, got %q", calls)
	}
}
