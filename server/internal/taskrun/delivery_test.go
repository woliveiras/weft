package taskrun

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestSCMHandOffGrantsOnlyTheTaskConnectionToTheResultAgent(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := Manager{Store: st}
	source := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory", Version: 1}, Spec: domain.GraphSpec{Nodes: []domain.Node{
		{ID: "planner", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}},
		{ID: "integrator", Prompt: "Hand off.", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}, Tools: []domain.ToolGrant{{Name: "git.read"}}},
	}}}
	d := &taskconfig.Draft{ID: "tsk_abc", Input: taskconfig.Input{Title: "Fix parser", GraphVersion: 1, NodeID: "planner", ResultNodeID: "integrator", SCMConnection: "gh"}}

	// The connection must exist.
	_, blocks, err := m.prepare(ctx, d, source)
	if err != nil || len(blocks) != 1 || !strings.Contains(blocks[0], `"gh" no longer exists`) {
		t.Fatalf("missing connection: %v %v", blocks, err)
	}
	box, _ := secrets.NewBox(bytes.Repeat([]byte{3}, secrets.KeySize))
	tok := "t"
	if _, err := (scm.Manager{Store: st, Box: box}).Save(ctx, scm.Input{Name: "gh", Kind: "github", Repository: "acme/app", BranchPrefix: "bots/", Token: &tok}, "admin", false); err != nil {
		t.Fatal(err)
	}
	g, blocks, err := m.prepare(ctx, d, source)
	if err != nil || len(blocks) != 0 {
		t.Fatalf("prepare: %v %v", blocks, err)
	}
	if source.Spec.Nodes[1].Prompt != "Hand off." || len(source.Spec.Nodes[1].Tools) != 1 {
		t.Fatal("the source workflow was changed")
	}
	planner, integrator := g.Node("planner"), g.Node("integrator")
	if len(planner.Tools) != 0 {
		t.Fatalf("only the result agent delivers: %+v", planner.Tools)
	}
	granted := map[string][]string{}
	for _, gr := range integrator.Tools {
		granted[gr.Name] = gr.Repositories
	}
	if len(granted["git.push"]) != 1 || granted["git.push"][0] != "gh" || len(granted["scm.pull_request.create"]) != 1 || granted["scm.pull_request.create"][0] != "gh" {
		t.Fatalf("grants = %+v", integrator.Tools)
	}
	for _, want := range []string{`"bots/task-abc"`, `git.push {"connection": "gh"}`, `"title": "Fix parser"`, "Never force a push"} {
		if !strings.Contains(integrator.Prompt, want) {
			t.Fatalf("prompt lacks %s: %s", want, integrator.Prompt)
		}
	}
	if g.Metadata.Labels[domain.LabelTaskSCM] != "gh" {
		t.Fatalf("labels = %+v", g.Metadata.Labels)
	}
	// Without a result agent the entry agent delivers.
	d.ResultNodeID = ""
	g, _, _ = m.prepare(ctx, d, source)
	if !strings.Contains(g.Node("planner").Prompt, "Source control hand-off") {
		t.Fatal("entry agent fallback")
	}
	// Tasks without a connection are untouched.
	d.SCMConnection = ""
	g, _, _ = m.prepare(ctx, d, source)
	if strings.Contains(g.Node("planner").Prompt, "Source control") || len(g.Node("integrator").Tools) != 1 {
		t.Fatal("hand-off added without a connection")
	}
}

func TestDeliveryReportsPushPullRequestAndFailures(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	bus := events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gid := "task-run-abc"
	bus.Publish(ctx, domain.NewEvent(domain.EvToolCompleted, gid, map[string]any{"tool": "git.push", "outcome": "error"}))
	bus.Publish(ctx, domain.NewEvent(domain.EvToolCompleted, gid, map[string]any{"tool": "filesystem.read", "outcome": "error"}))
	bus.Publish(ctx, domain.NewEvent(domain.EvBranchPushed, gid, map[string]any{"branch": "weft/task-abc", "repository": "acme/app"}))
	bus.Publish(ctx, domain.NewEvent(domain.EvToolDenied, gid, map[string]any{"tool": "scm.pull_request.create", "outcome": "rejected", "error": "not approved"}))
	bus.Publish(ctx, domain.NewEvent(domain.EvPullRequestOpened, gid, map[string]any{"url": "https://github.example/pr/7", "number": 7}))
	d := &taskconfig.Draft{ID: "tsk_abc", ExecutionGraphID: gid, Input: taskconfig.Input{SCMConnection: "gh"}}
	got, err := (Manager{Store: st}).delivery(ctx, d)
	if err != nil || got.Branch != "weft/task-abc" || got.Repository != "acme/app" || got.PullRequestNumber != 7 || got.PullRequestURL != "https://github.example/pr/7" {
		t.Fatalf("delivery = %+v %v", got, err)
	}
	if len(got.Failures) != 2 || got.Failures[0] != "git.push error" || got.Failures[1] != "scm.pull_request.create rejected: not approved" {
		t.Fatalf("failures = %+v", got.Failures)
	}
	if none, _ := (Manager{Store: st}).delivery(ctx, &taskconfig.Draft{ExecutionGraphID: gid}); none != nil {
		t.Fatalf("no connection, no delivery: %+v", none)
	}
}
