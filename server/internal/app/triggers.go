package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"github.com/woliveiras/weft/server/internal/trigger"
)

// triggerTasks creates and observes trigger tasks with the same managers
// the task API uses, as the person whose consent the trigger carries.
type triggerTasks struct {
	a      *App
	runner func() taskrun.Manager
}

func (x triggerTasks) Start(ctx context.Context, t *trigger.Trigger, i trigger.Issue) (string, error) {
	title := i.Key + ": " + strings.ReplaceAll(i.Title, "\x00", "")
	if len(title) > 200 {
		title = title[:200]
		for !utf8.ValidString(title) {
			title = title[:len(title)-1]
		}
	}
	in := taskconfig.Input{Title: title, Request: trigger.Request(i), Outcome: "implement", GraphID: t.Workflow,
		GraphVersion: t.Consent.WorkflowVersion, NodeID: t.NodeID, ResultNodeID: t.ResultNodeID, SCMConnection: t.SCMConnection,
		ExecutionMode: t.ExecutionMode}
	if t.ProjectID != "" {
		p, err := (projectconfig.Manager{Store: x.a.Store}).Get(ctx, t.ProjectID)
		if err != nil {
			return "", trigger.NewLaunchError("project", err)
		}
		in.ProjectID, in.BaseCommit = p.ID, p.Head
	}
	actor := t.Consent.Actor
	githubIssueKey := ""
	if i.Source == "github" {
		githubIssueKey = i.Key
	}
	d, err := (taskconfig.Manager{Store: x.a.Store}).SaveTriggered(ctx, in, actor, githubIssueKey)
	if err != nil {
		return "", trigger.NewLaunchError("draft", err)
	}
	taskID := d.ID
	fail := func(stage string, cause error, blockers ...string) (string, error) {
		failure := trigger.NewLaunchError(stage, cause, blockers...)
		return taskID, errors.Join(failure, x.recordLaunchFailure(ctx, taskID, actor, failure))
	}
	auth := taskauth.Manager{Store: x.a.Store, Tools: x.a.Tools}
	review, err := auth.Review(ctx, d.ID)
	if err != nil {
		return fail("authorization", err)
	}
	if _, err := auth.Save(ctx, d.ID, t.Consent.Authorization, review.Scope, 0, actor, true); err != nil {
		return fail("authorization", err)
	}
	launchReview, err := x.runner().Review(ctx, d.ID)
	if err != nil {
		return fail("review", err)
	}
	if launchReview.EnvironmentDefinition != nil {
		if !launchReview.CanPrepareEnvironment {
			return fail("preparation", taskconfig.ErrInvalid, launchReview.Blockers...)
		}
		if t.Consent.Authorization == taskauth.Ask {
			d.EnvironmentApprovalID = domain.NewID("apr")
			d, err = (taskconfig.Manager{Store: x.a.Store}).Transition(ctx, d, "draft", actor)
			if err != nil {
				return fail("preparation", err)
			}
			detail, err := json.Marshal(struct {
				TaskID, Scope string
				Environment   *domain.EnvironmentSpec
			}{d.ID, review.Scope, launchReview.EnvironmentDefinition})
			if err != nil {
				return fail("preparation", err)
			}
			expires := time.Now().Add(24 * time.Hour)
			_, err = x.a.Approvals.Create(ctx, &domain.Approval{ID: d.EnvironmentApprovalID, GraphID: d.GraphID, NodeID: d.NodeID, Kind: domain.ApprovalTaskEnvironment,
				Title: "Prepare environment for task " + d.ID, Detail: detail, ExpiresAt: &expires})
			if err != nil {
				return fail("preparation", err)
			}
			return d.ID, nil
		}
		if _, err := x.runner().PrepareEnvironment(ctx, d.ID, d.Revision, actor); err != nil {
			return fail("preparation", err)
		}
		return d.ID, nil
	}
	if !launchReview.CanStart {
		return fail("start", taskconfig.ErrInvalid, launchReview.Blockers...)
	}
	if _, err := x.runner().Start(ctx, d.ID, d.Revision, actor); err != nil {
		return fail("start", err)
	}
	return d.ID, nil
}

func (x triggerTasks) Status(ctx context.Context, taskID string) (trigger.TaskStatus, error) {
	v, err := x.runner().View(ctx, taskID)
	if err != nil {
		return trigger.TaskStatus{}, err
	}
	if v.Task.LaunchError != "" && (v.Task.Status == "draft" || v.Task.Status == "environment_pending" || v.Task.Status == "interrupted") {
		return trigger.TaskStatus{State: "failed", LaunchFailure: true, Reason: v.Task.LaunchError}, nil
	}
	fail := func(stage string, cause error, blockers ...string) (trigger.TaskStatus, error) {
		failure := trigger.NewLaunchError(stage, cause, blockers...)
		if err := x.recordLaunchFailure(ctx, taskID, v.Task.CreatedBy, failure); err != nil {
			return trigger.TaskStatus{}, err
		}
		return trigger.TaskStatus{State: "failed", LaunchFailure: true, Reason: failure.Error()}, nil
	}
	if v.Task.Status == "preparing" || v.Task.Status == "environment_build_requested" {
		return fail("preparation", errors.New("interrupted preparation"), "Environment preparation was interrupted. Inspect retained resources before retrying.")
	}
	if v.Task.Status == "draft" && v.Task.EnvironmentApprovalID != "" {
		a, err := x.a.Store.GetApproval(ctx, v.Task.EnvironmentApprovalID)
		if err != nil {
			return fail("preparation", err)
		}
		if a.Status == domain.ApprovalPending && a.ExpiresAt != nil && !time.Now().Before(*a.ExpiresAt) {
			a, err = x.a.Approvals.Expire(ctx, a)
			if err != nil {
				return trigger.TaskStatus{}, err
			}
		}
		if a.Status == domain.ApprovalPending {
			return trigger.TaskStatus{State: "running"}, nil
		}
		if a.Status != domain.ApprovalApproved {
			return fail("approval", errors.New("preparation approval rejected or expired"))
		}
		if _, err := x.runner().PrepareEnvironment(ctx, taskID, v.Task.Revision, v.Task.CreatedBy); err != nil {
			return fail("preparation", err)
		}
		return trigger.TaskStatus{State: "running"}, nil
	}
	if v.Task.Status == "environment_pending" {
		if v.Environment == nil {
			return fail("preparation", errors.New("environment state unavailable"))
		}
		if v.Environment.State == environment.StateBuilding {
			return trigger.TaskStatus{State: "running"}, nil
		}
		if v.Environment.State == environment.StateFailed {
			return fail("build", errors.New(v.Environment.Error))
		}
		if !v.Environment.UpToDate {
			return fail("preparation", errors.New("prepared environment no longer matches"))
		}
		review, err := x.runner().Review(ctx, taskID)
		if err != nil {
			return fail("review", err)
		}
		if !review.CanStart {
			return fail("start", taskconfig.ErrInvalid, review.Blockers...)
		}
		if _, err := x.runner().Start(ctx, taskID, v.Task.Revision, v.Task.CreatedBy); err != nil {
			return fail("start", err)
		}
		return trigger.TaskStatus{State: "running"}, nil
	}
	st := trigger.TaskStatus{State: v.State}
	switch v.State {
	case "succeeded", "failed", "cancelled", "interrupted", "blocked":
	default:
		st.State = "running"
	}
	if v.State == "interrupted" {
		st.State = "failed"
	}
	if len(v.Blockers) > 0 {
		st.Reason = v.Blockers[0]
	}
	if v.Result != nil && v.Result.Summary != "" {
		st.Reason = v.Result.Summary
	}
	if v.Delivery != nil {
		st.PullRequestURL = v.Delivery.PullRequestURL
		if st.Reason == "" && len(v.Delivery.Failures) > 0 {
			st.Reason = v.Delivery.Failures[len(v.Delivery.Failures)-1]
		}
	}
	return st, nil
}

// Record against the latest revision: Start may already have retained an
// interrupted execution. Preserve its state and never replay input on failure.
func (x triggerTasks) recordLaunchFailure(ctx context.Context, id, actor string, failure *trigger.LaunchError) error {
	tm := taskconfig.Manager{Store: x.a.Store}
	ctx = context.WithoutCancel(ctx)
	d, err := tm.Get(ctx, id)
	if err == nil {
		d.LaunchError = failure.Error()
		_, err = tm.Transition(ctx, d, d.Status, actor)
	}
	if err != nil {
		return errors.New("launch diagnostic could not be persisted")
	}
	return nil
}
