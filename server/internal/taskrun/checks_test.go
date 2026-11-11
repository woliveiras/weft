package taskrun

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestPrepareSnapshotsProjectChecksMergedWithWorkflowChecks(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
	}
	pm := projectconfig.Manager{Store: st}
	p, err := pm.Create(ctx, projectconfig.Input{Name: "Fixture", Path: repo}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	p, err = pm.SetChecks(ctx, p.ID, []domain.Check{{Name: "test", Command: "make test"}, {Name: "lint", Command: "make lint"}}, p.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	source := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory", Version: 1}, Spec: domain.GraphSpec{
		Checks: []domain.Check{{Name: "test", Command: "go test ./..."}},
		Nodes:  []domain.Node{{ID: "dev", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}}},
	}}
	d := &taskconfig.Draft{ID: "tsk_abc", Input: taskconfig.Input{Title: "Fix", GraphVersion: 1, NodeID: "dev", ResultNodeID: "dev", ProjectID: p.ID, BaseCommit: p.Head}}
	m := Manager{Store: st}

	g, blocks, err := m.prepare(ctx, d, source)
	if err != nil || len(blocks) != 0 {
		t.Fatalf("prepare: %v %v", blocks, err)
	}
	if got := fmt.Sprint(g.Spec.Checks); got != "[{test go test ./...  0} {lint make lint  0}]" {
		t.Fatalf("merged checks %s", got)
	}
	if len(source.Spec.Checks) != 1 {
		t.Fatal("the source workflow was changed")
	}
	if g.Metadata.Labels[domain.LabelTaskBase] != p.Head {
		t.Fatalf("the task copy names its base commit for check evidence: %+v", g.Metadata.Labels)
	}

	// Without a project only the workflow's checks apply.
	g, _, _ = m.prepare(ctx, &taskconfig.Draft{ID: "tsk_abd", Input: taskconfig.Input{Title: "Fix", GraphVersion: 1, NodeID: "dev", ResultNodeID: "dev"}}, source)
	if len(g.Spec.Checks) != 1 || g.Spec.Checks[0].Command != "go test ./..." {
		t.Fatalf("workflow-only checks %+v", g.Spec.Checks)
	}

	// A combination over the bound is a reviewable blocker, not a failed start.
	many := make([]domain.Check, domain.MaxChecks)
	for i := range many {
		many[i] = domain.Check{Name: fmt.Sprintf("w%d", i), Command: "true"}
	}
	source.Spec.Checks = many
	_, blocks, err = m.prepare(ctx, d, source)
	if err != nil || len(blocks) != 1 || !strings.Contains(blocks[0], "cannot be combined") {
		t.Fatalf("over the bound: %v %v", blocks, err)
	}

	// A project that disappeared blocks the start.
	d.ProjectID = "prj_missing"
	_, blocks, err = m.prepare(ctx, d, source)
	if err != nil || len(blocks) != 1 || !strings.Contains(blocks[0], "no longer exists") {
		t.Fatalf("missing project: %v %v", blocks, err)
	}
}
