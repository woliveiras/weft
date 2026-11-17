package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// prepare is Prepare plus the SCM hand-off a task asks for.
func (m Manager) prepare(ctx context.Context, d *taskconfig.Draft, source *domain.Graph) (*domain.Graph, []string, error) {
	g, blockers, err := Prepare(d, source)
	if err != nil {
		return g, blockers, err
	}
	checkBlocks, err := m.attachChecks(ctx, d, g)
	if blockers = append(blockers, checkBlocks...); err != nil {
		return g, blockers, err
	}
	if d.SCMConnection == "" {
		return g, blockers, err
	}
	b, err := m.attachSCM(ctx, d, g)
	return g, append(blockers, b...), err
}

// attachChecks snapshots the project's declared checks into the task graph,
// merged with the workflow's, so check.run sees one fixed list for the
// whole task even if the project's checks change while it runs.
func (m Manager) attachChecks(ctx context.Context, d *taskconfig.Draft, g *domain.Graph) ([]string, error) {
	if d.ProjectID == "" {
		return nil, nil
	}
	p, err := (projectconfig.Manager{Store: m.Store}).Get(ctx, d.ProjectID)
	if errors.Is(err, storage.ErrNotFound) {
		return []string{"The task's project no longer exists."}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load project checks: %w", err)
	}
	merged := domain.MergeChecks(p.Checks, g.Spec.Checks)
	if err := domain.ValidateChecks(merged); err != nil {
		return []string{"Project and workflow checks cannot be combined: " + err.Error()}, nil
	}
	g.Spec.Checks = merged
	return nil, nil
}

// Branch is the task branch a hand-off pushes.
func Branch(c scm.Connection, taskID string) string {
	return c.Prefix() + "task-" + strings.TrimPrefix(taskID, "tsk_")
}

// attachSCM lets the result agent (the agent after the workflow's human
// gate) deliver through the task's SCM connection: commit on the task
// branch, push it and open a pull request, all through gateway tools whose
// grants name only that connection. Opening the pull request still waits
// for a person unless the connection relaxes it.
func (m Manager) attachSCM(ctx context.Context, d *taskconfig.Draft, g *domain.Graph) ([]string, error) {
	c, err := (scm.Manager{Store: m.Store}).Get(ctx, d.SCMConnection)
	if errors.Is(err, storage.ErrNotFound) {
		return []string{fmt.Sprintf("The SCM connection %q no longer exists. Choose another one or none.", d.SCMConnection)}, nil
	}
	if err != nil {
		return nil, err
	}
	nodeID := d.ResultNodeID
	if nodeID == "" {
		nodeID = d.NodeID
	}
	n := g.Node(nodeID)
	if n == nil {
		return []string{"Select the result agent that delivers to source control."}, nil
	}
	repos := []string{c.Name}
	n.Tools = append(n.Tools,
		domain.ToolGrant{Name: "git.read"},
		domain.ToolGrant{Name: "git.write"},
		domain.ToolGrant{Name: "git.push", Repositories: repos},
		domain.ToolGrant{Name: "scm.pull_request.create", Repositories: repos})
	n.Prompt += fmt.Sprintf(`

Source control hand-off (configured for this task; it replaces any earlier instruction not to commit or push):
1. Create and switch to the branch %q with git.branch (checkout=true) unless you are already on it.
2. Commit every change with git.commit (all=true) and a message summarising the task.
3. Push with git.push {"connection": %q}.
4. Open the pull request with scm.pull_request.create {"connection": %q, "title": %q} and a body that summarises the change, the evidence and the checks still to run.
If any step fails (a rejected push, a missing permission, a refused approval), stop and report the failure in your result. Never force a push and never target another branch.`,
		Branch(c, d.ID), c.Name, c.Name, d.Title)
	g.Metadata.Labels[domain.LabelTaskSCM] = c.Name
	return nil, nil
}

// Delivery is what a task's SCM hand-off did, from the task's events.
type Delivery struct {
	Connection        string `json:"connection"`
	Branch            string `json:"branch,omitempty"`
	Repository        string `json:"repository,omitempty"`
	PullRequestURL    string `json:"pullRequestUrl,omitempty"`
	PullRequestNumber int    `json:"pullRequestNumber,omitempty"`
	// Failures are the push and pull request calls that did not succeed.
	Failures []string `json:"failures,omitempty"`
}

func (m Manager) delivery(ctx context.Context, d *taskconfig.Draft) (*Delivery, error) {
	if d.SCMConnection == "" || d.ExecutionGraphID == "" {
		return nil, nil
	}
	out := &Delivery{Connection: d.SCMConnection}
	evs, err := m.Store.ListEvents(ctx, storage.EventFilter{GraphID: d.ExecutionGraphID, Ascending: true, Limit: 5000,
		Types: []domain.EventType{domain.EvBranchPushed, domain.EvPullRequestOpened, domain.EvToolCompleted, domain.EvToolDenied}})
	if err != nil {
		return nil, err
	}
	for _, ev := range evs {
		var data struct {
			Branch, Repository, URL, Tool, Outcome, Error string
			Number                                        int
		}
		_ = json.Unmarshal(ev.Data, &data)
		switch ev.Type {
		case domain.EvBranchPushed:
			out.Branch, out.Repository = data.Branch, data.Repository
		case domain.EvPullRequestOpened:
			out.PullRequestURL, out.PullRequestNumber = data.URL, data.Number
		default:
			if (data.Tool == "git.push" || data.Tool == "scm.pull_request.create") && data.Outcome != "ok" {
				msg := data.Tool + " " + data.Outcome
				if data.Error != "" {
					msg += ": " + data.Error
				}
				out.Failures = append(out.Failures, msg)
			}
		}
	}
	return out, nil
}
