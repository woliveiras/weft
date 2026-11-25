package scm_test

import (
	"encoding/json"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestLongTaskPullRequestUsesVerifiedIssueOwner(t *testing.T) {
	for _, invalid := range []string{"", "owner", "binding", "missing-graph"} {
		t.Run(invalid, func(t *testing.T) {
			gh := newGitHub(t)
			m, st := manager(t)
			if _, err := m.Save(ctx, scm.Input{Name: "gh", Kind: "github", Repository: "acme/app", APIURL: gh.URL, Token: ptr(token), RequirePullRequestApproval: ptr(false)}, "admin", false); err != nil {
				t.Fatal(err)
			}
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "workflow"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker"}}}}
			if _, err := st.SaveGraph(ctx, g, 0); err != nil {
				t.Fatal(err)
			}
			tasks := taskconfig.Manager{Store: st}
			source, err := tasks.Save(ctx, "", taskconfig.Input{Title: "Fix", Request: "Work", Outcome: "implement", GraphID: "workflow", GraphVersion: 1, NodeID: "worker"}, 0, "admin")
			if err != nil {
				t.Fatal(err)
			}
			source, err = tasks.Transition(ctx, source, "failed", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			draft, _, err := tasks.RetryFailed(ctx, source.ID, source.Revision, "operator")
			if err != nil {
				t.Fatal(err)
			}
			// Seed trusted historical issue attribution on this long-ID fixture.
			draft.TriggerGitHubIssue = "acme/app#7"
			gid := domain.TaskExecutionID(draft.ID)
			draft.ExecutionGraphID = gid
			if invalid == "binding" {
				draft.ExecutionGraphID = "task-run-other"
			}
			if _, err = tasks.Transition(ctx, draft, "running", "fixture"); err != nil {
				t.Fatal(err)
			}
			owner := draft.ID
			if invalid == "owner" {
				owner = source.ID
			}
			if invalid != "missing-graph" {
				copy := &domain.Graph{Metadata: domain.GraphMetadata{ID: gid, Labels: map[string]string{domain.LabelTask: owner}}}
				if _, err = st.SaveGraph(ctx, copy, 0); err != nil {
					t.Fatal(err)
				}
			}
			p := principal("", domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"gh"}})
			p.GraphID = gid
			out, err := gateway(t, m).Invoke(ctx, p, "scm.pull_request.create", json.RawMessage(`{"connection":"gh","branch":"weft/fix","title":"Fix","body":"Verified change."}`))
			if err != nil {
				t.Fatal(err)
			}
			if invalid != "" {
				if !out.IsError || len(gh.pulls) != 0 {
					t.Fatal("invalid owner opened pull request", out, gh.pulls)
				}
				return
			}
			if out.IsError || len(gh.pulls) != 1 || gh.pulls[0]["body"] != "Verified change.\n\nCloses #7" {
				t.Fatal("long task issue attribution lost", out, gh.pulls)
			}
		})
	}
}
