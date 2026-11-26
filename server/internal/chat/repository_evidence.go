package chat

import (
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

type RepositoryEvidence struct {
	Alias       string `json:"alias,omitempty"`
	Project     string `json:"project"`
	Base        string `json:"base"`
	Preparation string `json:"preparation"`
	Delivery    string `json:"delivery,omitempty"`
	PullRequest int    `json:"pullRequest,omitempty"`
}

// The task service owns checkout and delivery state. Keep identifiers and
// verified phases here. Raw provider errors, repository URLs and paths stay in Tasks.
func ProjectTaskEvidence(out *TaskSummary, view *taskrun.View) {
	d := view.Task
	if d == nil || d.ID != out.ID || d.ExecutionGraphID == "" || d.ExecutionGraphID != domain.TaskExecutionID(d.ID) {
		return
	}
	out.RetryOf = d.RetryOfTaskID
	if d.ProjectID != "" && shortLabel(d.ProjectID) && commitSHA.MatchString(d.BaseCommit) {
		row := RepositoryEvidence{Project: d.ProjectID, Base: d.BaseCommit, Preparation: "selected"}
		if delivery := view.Delivery; delivery != nil && delivery.Connection == d.SCMConnection {
			switch {
			case delivery.EventID != "" && delivery.PullRequestNumber > 0:
				row.Delivery, row.PullRequest = "pull_request confirmed", delivery.PullRequestNumber
			case len(delivery.Failures) > 0:
				row.Delivery = "delivery failure recorded"
			case delivery.Branch != "" && delivery.Repository != "":
				row.Delivery = "push confirmed"
			default:
				row.Delivery = "pending"
			}
		}
		out.Repositories = append(out.Repositories, row)
	}
	byAlias := map[string]taskrun.MemberProgress{}
	for _, member := range view.Repositories {
		byAlias[member.Alias] = member
	}
	for _, ref := range d.Repositories {
		if len(out.Repositories) >= InspectionLimit {
			out.OtherRepositories++
			continue
		}
		if !domain.ValidRepositoryAlias(ref.Alias) || !shortLabel(ref.ProjectID) || !commitSHA.MatchString(ref.BaseCommit) {
			continue
		}
		row := RepositoryEvidence{Alias: ref.Alias, Project: ref.ProjectID, Base: ref.BaseCommit, Preparation: "unverified"}
		if member, ok := byAlias[ref.Alias]; ok {
			switch member.Preparation {
			case "pending", "ready", "failed":
				row.Preparation = member.Preparation
			}
			if ref.SCMConnection != "" {
				row.Delivery = "unverified"
				if member.Delivery == "pending" {
					row.Delivery = "pending"
				}
				phase, state, _ := strings.Cut(member.Delivery, " ")
				if (phase == "push" || phase == "pull_request") && (state == "confirmed" || state == "failed" || state == "unverified" || state == "pending" || state == "blocked") {
					row.Delivery = phase + " " + state
				}
				if row.Delivery == "pull_request confirmed" && member.PullRequest != nil && member.PullRequest.Number > 0 {
					row.PullRequest = member.PullRequest.Number
				}
			}
		}
		out.Repositories = append(out.Repositories, row)
	}
}
