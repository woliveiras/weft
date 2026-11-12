package app

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

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
			return "", fmt.Errorf("project %s: %w", t.ProjectID, err)
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
		return "", err
	}
	auth := taskauth.Manager{Store: x.a.Store, Tools: x.a.Tools}
	review, err := auth.Review(ctx, d.ID)
	if err != nil {
		return d.ID, err
	}
	if _, err := auth.Save(ctx, d.ID, t.Consent.Authorization, review.Scope, 0, actor, true); err != nil {
		return d.ID, fmt.Errorf("record task authorization: %w", err)
	}
	if _, err := x.runner().Start(ctx, d.ID, d.Revision, actor); err != nil {
		return d.ID, err
	}
	return d.ID, nil
}

func (x triggerTasks) Status(ctx context.Context, taskID string) (trigger.TaskStatus, error) {
	v, err := x.runner().View(ctx, taskID)
	if err != nil {
		return trigger.TaskStatus{}, err
	}
	st := trigger.TaskStatus{State: v.State}
	switch v.State {
	case "succeeded", "failed", "cancelled", "interrupted", "blocked":
	default:
		st.State = "running"
	}
	if v.State == "interrupted" || v.State == "blocked" {
		st.State = "failed"
	}
	if len(v.Blockers) > 0 {
		st.Reason = v.Blockers[0]
	}
	if v.Delivery != nil {
		st.PullRequestURL = v.Delivery.PullRequestURL
		if st.Reason == "" && len(v.Delivery.Failures) > 0 {
			st.Reason = v.Delivery.Failures[len(v.Delivery.Failures)-1]
		}
	}
	return st, nil
}
