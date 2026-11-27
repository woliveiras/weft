//go:build kubernetes_e2e

package kubesandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestKubernetesPrivateBranch(t *testing.T) {
	root := os.Getenv("WEFT_FIXTURE_WORKSPACES")
	if root == "" {
		t.Fatal("fixture claim is required")
	}
	m, err := workspace.NewManager(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	m.TransferReplicaBranch = func(ctx context.Context, r workspace.ReplicaBranchTransfer) error {
		return TransferBranch(ctx, root, "weft/sandbox-go:dev", r)
	}
	id, err := os.MkdirTemp(root, "private-")
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: filepath.Base(id)}}
	g.Spec.Nodes = []domain.Node{{ID: "worker", Workspace: domain.NodeWorkspace{Mode: domain.WorkspaceWorktree}}}
	n := &g.Spec.Nodes[0]
	ctx := t.Context()
	a, branch, err := m.Replica(ctx, g, n, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := m.Replica(ctx, g, n, 1)
	if err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		c := exec.CommandContext(ctx, "git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	base := git(id, "rev-parse", "HEAD")
	run := func(script string) string {
		t.Helper()
		sb, err := New(Replica{Graph: g.Metadata.ID, Node: n.ID, Index: 0, Root: root, Workdir: a, Image: "weft/sandbox-go:dev"})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := sb.Close(); err != nil {
				t.Error(err)
			}
		}()
		res, err := sb.Exec(ctx, tools.ExecRequest{Shell: script})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("sandbox Git: %v %+v", err, res)
		}
		return strings.TrimSpace(res.Stdout)
	}
	if got := run("git status --porcelain && git branch --show-current"); got != branch {
		t.Fatal(got)
	}
	tip := run("echo revision > feature && git add feature && git commit -qm first && echo scratch > local && git rev-parse HEAD")
	if git(b, "rev-parse", "HEAD") != base || git(id, "branch", "--list", branch) != "" || git(a, "remote") != "" {
		t.Fatal("private commit or remote visible before publication")
	}
	recovered, _, err := m.Replica(ctx, g, n, 0)
	if err != nil || recovered != a || git(a, "rev-parse", "HEAD") != tip {
		t.Fatal("restart lost private commit", err)
	}
	if _, err := os.Stat(filepath.Join(a, "local")); err != nil {
		t.Fatal(err)
	}
	inspect, err := m.Inspect(ctx, a, id, "status", "")
	if err != nil || inspect.Branch != branch {
		t.Fatal(inspect, err)
	}
	publish := func(commit, prev string) error {
		_, err := m.PublishReplicaBranch(ctx, g, "worker", 0, commit, prev)
		return err
	}
	if err := publish(tip, ""); err != nil {
		t.Fatal(err)
	}
	if err := publish(tip, ""); err != nil {
		t.Fatal("retry", err)
	}
	if git(id, "rev-parse", "refs/heads/"+branch) != tip || git(id, "rev-parse", "HEAD") != base || git(b, "rev-parse", "HEAD") != base {
		t.Fatal("publication changed another ref")
	}
	if _, err := os.Stat(filepath.Join(id, "local")); !os.IsNotExist(err) {
		t.Fatal("untracked file published")
	}
	next := run("git commit --allow-empty -qm second && git rev-parse HEAD")
	if err := publish(tip, ""); err != nil {
		t.Fatal("retry after source advanced", err)
	}
	if err := publish(next, base); !errors.Is(err, workspace.ErrBranchConflict) {
		t.Fatal("stale destination", err)
	}
	if err := publish(next, tip); err != nil {
		t.Fatal(err)
	}
	if err := publish(tip, next); !errors.Is(err, workspace.ErrBranchConflict) {
		t.Fatal("stale source", err)
	}
	if git(id, "rev-parse", "refs/heads/"+branch) != next {
		t.Fatal("wrong published commit")
	}
}
