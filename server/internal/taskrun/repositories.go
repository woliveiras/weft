package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func (m Manager) prepareCheckouts(ctx context.Context, d *taskconfig.Draft, g *domain.Graph, actor string) error {
	if d.ProjectID == "" && len(d.Repositories) == 0 {
		return nil
	}
	if m.Workspaces == nil {
		return errors.New("task workspaces unavailable")
	}
	projects := projectconfig.Manager{Store: m.Store}
	if d.ProjectID != "" {
		p, err := projects.Get(ctx, d.ProjectID)
		if err != nil {
			return err
		}
		_, err = projects.CreateTaskCheckout(ctx, p, d.BaseCommit, m.Workspaces.Root, g.Metadata.ID, actor)
		return err
	}
	if err := os.Mkdir(filepath.Join(m.Workspaces.Root, g.Metadata.ID), 0700); err != nil {
		return fmt.Errorf("cannot reserve a new task workspace: %w", err)
	}
	for _, r := range d.Repositories {
		p, err := projects.Get(ctx, r.ProjectID)
		if err == nil {
			_, err = projects.CreateTaskCheckout(ctx, p, r.BaseCommit, m.Workspaces.Root, g.Metadata.ID, actor, r.Alias)
		}
		if err != nil {
			recordErr := m.Store.AppendEvent(context.WithoutCancel(ctx), domain.NewEvent("task.repository_failed", g.Metadata.ID, map[string]string{"taskId": d.ID, "repository": r.Alias}).WithActor(actor))
			return errors.Join(fmt.Errorf("repository %s: checkout preparation failed: %w", r.Alias, err), recordErr)
		}
		if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.repository_prepared", g.Metadata.ID, map[string]string{"taskId": d.ID, "repository": r.Alias, "base": r.BaseCommit}).WithActor(actor)); err != nil {
			return err
		}
	}
	return nil
}

func (m Manager) reviewRepositories(ctx context.Context, d *taskconfig.Draft, g *domain.Graph) ([]string, error) {
	var out []string
	if m.Workspaces == nil {
		out = append(out, "Task workspaces are unavailable.")
	}
	paths := map[string]bool{}
	var connections []string
	for _, r := range d.Repositories {
		manager := projectconfig.Manager{Store: m.Store}
		p, err := manager.Get(ctx, r.ProjectID)
		if errors.Is(err, storage.ErrNotFound) {
			out = append(out, r.Alias+": project unavailable")
			continue
		}
		if err != nil {
			return nil, err
		}
		if paths[p.Path] {
			out = append(out, r.Alias+": the same registered source has already been selected")
		}
		paths[p.Path] = true
		sourceRef := p.RepositoryRef
		if sourceRef == "" {
			sourceRef = "HEAD"
		}
		if r.SourceRef != "" && r.SourceRef != sourceRef {
			out = append(out, r.Alias+": source reference differs from the registered project")
		}
		if err := projectconfig.Verify(ctx, p, r.BaseCommit); err != nil {
			out = append(out, r.Alias+": "+err.Error())
		}
		if !slices.Equal(p.Checks, r.Checks) {
			out = append(out, r.Alias+": declared checks changed; refresh the project selection before starting")
		}
		if r.SCMConnection != "" {
			c, err := (scm.Manager{Store: m.Store}).Get(ctx, r.SCMConnection)
			if errors.Is(err, storage.ErrNotFound) {
				out = append(out, r.Alias+": delivery connection unavailable")
				continue
			}
			if err != nil {
				return nil, err
			}
			if r.ConnectionRevision != c.UpdatedAt.UTC().Format(time.RFC3339Nano) {
				out = append(out, r.Alias+": delivery connection changed; select it again")
			}
			if err := projectconfig.MatchDelivery(ctx, p, c.Repository, c.PublicRemoteURL(), r.Repository); err != nil {
				out = append(out, r.Alias+": "+err.Error())
			}
			if !scm.ValidBaseBranch(r.TargetBranch) || r.TargetBranch == c.Prefix()+g.Metadata.ID {
				out = append(out, r.Alias+": invalid PR base or incompatible task branch prefix")
			}
			connections = append(connections, c.Name)
		}
	}
	if len(connections) > 0 {
		resultID := d.ResultNodeID
		if resultID == "" {
			resultID = d.NodeID
		}
		n := g.Node(resultID)
		if n == nil {
			return append(out, "Select a result agent for PR delivery."), nil
		}
		canReview := false
		for _, agent := range g.Spec.Nodes {
			if agent.Disabled {
				continue
			}
			for _, grant := range agent.Tools {
				if domain.GlobMatch(grant.Name, "review.request") {
					canReview = true
				}
			}
		}
		if !canReview {
			out = append(out, "PR delivery requires an agent with a review.request grant to review all repositories.")
		}
		for _, tool := range scm.Tools(scm.Manager{}) {
			if tool.Name() != "git.push" && tool.Name() != "scm.pull_request.create" {
				continue
			}
			existing := tools.MatchingGrants(&tools.Principal{Grants: n.Tools}, tool)
			if len(existing) == 0 {
				n.Tools = append(n.Tools, domain.ToolGrant{Name: tool.Name(), Repositories: connections})
				continue
			}
			// Preserve the workflow's approval and repository restrictions.
			grant, err := tools.Authorize(&tools.Principal{Grants: n.Tools}, tool)
			if err != nil {
				return nil, err
			}
			for _, connection := range connections {
				if tools.CheckRepository(grant, connection) != nil {
					out = append(out, tool.Name()+": workflow grant does not include "+connection)
				}
			}
		}
		for _, repo := range d.Repositories {
			n.Prompt += "\n" + repo.Alias + " uses SCM connection " + repo.SCMConnection + " to " + repo.Repository + " (" + repo.TargetBranch + ")."
		}
		n.Prompt += "\nFor each changed repository, stage and commit on its task branch. Run the required checks on the final commits. Request review of the complete workspace, then use git.push and scm.pull_request.create with the reviewed connection for each changed repository. Report partial delivery explicitly."
	}
	return out, nil
}

type MemberProgress struct {
	Alias       string           `json:"alias"`
	Preparation string           `json:"preparation"`
	Delivery    string           `json:"delivery,omitempty"`
	PullRequest *scm.PullRequest `json:"pullRequest,omitempty"`
	Details     string           `json:"details,omitempty"`
}

func (m Manager) repositoryProgress(ctx context.Context, d *taskconfig.Draft) ([]MemberProgress, error) {
	events, err := m.Store.ListEvents(ctx, storage.EventFilter{GraphID: d.ExecutionGraphID, Types: []domain.EventType{"task.repository_prepared", "task.repository_failed"}, Limit: 5000})
	if err != nil {
		return nil, err
	}
	stages := map[string]string{}
	for _, ev := range events {
		var data struct{ Repository string }
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			return nil, err
		}
		if _, ok := stages[data.Repository]; !ok {
			if ev.Type == "task.repository_prepared" {
				stages[data.Repository] = "ready"
			} else {
				stages[data.Repository] = "failed"
			}
		}
	}
	var rows []MemberProgress
	for _, repo := range d.Repositories {
		row := MemberProgress{Alias: repo.Alias, Preparation: stages[repo.Alias]}
		if row.Preparation == "" {
			row.Preparation = "pending"
		}
		if row.Preparation == "failed" {
			row.Details = d.LaunchError
		}
		if repo.SCMConnection != "" {
			row.Delivery = "pending"
			for _, phase := range []string{"push", "pull_request"} {
				r, _, err := scm.ReadRepositoryReceipt(ctx, m.Store, d.ExecutionGraphID, repo.Alias, phase)
				if err != nil {
					return nil, err
				}
				if r == nil {
					continue
				}
				row.Delivery = phase + " " + r.State
				row.Details = r.Message
				if r.PullRequest != nil {
					row.PullRequest = r.PullRequest
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (m Manager) repositoryOutcome(ctx context.Context, v *View) ([]string, error) {
	d := v.Task
	if len(d.Repositories) == 0 {
		return nil, nil
	}
	wantPR := d.Repositories[0].SCMConnection != ""
	if !wantPR && len(tools.RequiredRepositoryChecks(d.Repositories)) == 0 {
		return nil, nil
	}
	if m.Workspaces == nil {
		return []string{"Cannot verify repository workspace state."}, nil
	}
	states, err := workspace.RepositoryStates(ctx, filepath.Join(m.Workspaces.Root, d.ExecutionGraphID), d.Repositories)
	if err != nil {
		return []string{"Cannot verify repository state: " + err.Error()}, nil
	}
	blockers, err := tools.RepositoryCheckBlockers(ctx, m.Store, d.ExecutionGraphID, d.Repositories, states)
	if err != nil || !wantPR {
		return blockers, err
	}
	approved, approvalErr := review.New(review.Options{Store: m.Store}).ApprovedRepositories(ctx, d.ExecutionGraphID, states)
	changes := 0
	for i, repo := range d.Repositories {
		state := states[repo.Alias]
		if !state.Changed {
			v.Repositories[i].Delivery = "unchanged"
			continue
		}
		changes++
		pr := v.Repositories[i].PullRequest
		receipt, _, err := scm.ReadRepositoryReceipt(ctx, m.Store, d.ExecutionGraphID, repo.Alias, "pull_request")
		if err != nil {
			return nil, err
		}
		if state.Dirty || pr == nil || receipt == nil || approvalErr != nil || receipt.Candidate != approved.ID || receipt.State != "confirmed" || receipt.Head != state.Head || receipt.Connection != repo.SCMConnection || receipt.Repository != repo.Repository || pr.HeadSHA != state.Head || pr.Base != repo.TargetBranch || pr.Head != receipt.Branch {
			blockers = append(blockers, repo.Alias+": missing confirmed PR for the reviewed head")
		}
	}
	if changes == 0 {
		blockers = append(blockers, "PR delivery was requested but no repository changed.")
	}
	if approvalErr != nil {
		blockers = append(blockers, "The current combined candidate requires an approved review.")
	}
	return blockers, nil
}

func deliveryStarted(rows []MemberProgress) bool {
	for _, row := range rows {
		if row.Delivery != "" && row.Delivery != "pending" && row.Delivery != "unchanged" {
			return true
		}
	}
	return false
}

// CurrentRepositoryStates resolves the task-owned bindings before exposing
// fingerprints to the console. Any unreadable member makes the set unknown.
func (m Manager) CurrentRepositoryStates(ctx context.Context, d *taskconfig.Draft) (map[string]workspace.RepositoryState, error) {
	if m.Workspaces == nil || d.ExecutionGraphID != ExecutionID(d.ID) || len(d.Repositories) == 0 {
		return nil, errors.New("task repository checkouts unavailable")
	}
	root, err := filepath.EvalSymlinks(m.Workspaces.Root)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, d.ExecutionGraphID)
	pm := projectconfig.Manager{Store: m.Store}
	for _, r := range d.Repositories {
		project, err := pm.Get(ctx, r.ProjectID)
		if err != nil {
			return nil, err
		}
		if _, err = pm.TaskInspectionBase(ctx, project, d.ExecutionGraphID, filepath.Join(base, r.Alias), r.Alias); err != nil {
			return nil, err
		}
	}
	return workspace.RepositoryStates(ctx, base, d.Repositories)
}
