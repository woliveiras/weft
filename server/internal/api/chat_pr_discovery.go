package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/tools"
)

const (
	prRepositoryArea = "pr-repository"
	prReviewerArea   = "pr-reviewer"
	prSetupArea      = "pr-setup"
)

type prMatch struct {
	repo     prRepositoryTarget
	reader   manualPRTarget
	snapshot scm.ReviewSnapshot
}

func (s *Server) lookupChatPRMetadata(ctx context.Context, actor Identity, target manualPRTarget) (snapshot scm.ReviewSnapshot, state string) {
	if s.Gateway == nil {
		return snapshot, ""
	}
	p := target.proposal
	args, _ := json.Marshal(map[string]any{"connection": p.Connection, "connectionRevision": p.ConnectionRevision, "number": p.Number, "metadata": true})
	response, err := s.Gateway.Invoke(tools.WithoutApproval(ctx), &tools.Principal{Actor: actor.Name, GraphID: p.Graph, GraphVersion: p.Version,
		NodeID: p.Node, WorkspaceReadOnly: true, Grants: []domain.ToolGrant{target.grant}}, scm.CapPullRequestRead, args)
	var result struct {
		Connection, ConnectionRevision, Provider, Repository, Status string
		PullRequest                                                  scm.ReviewSnapshot
	}
	if err != nil || response == nil || response.IsError || response.Truncated || json.Unmarshal(response.Data, &result) != nil ||
		result.Connection != p.Connection || result.ConnectionRevision != p.ConnectionRevision || result.Provider != p.Provider || result.Repository != p.Repository {
		return snapshot, ""
	}
	if result.Status == "absent" {
		return snapshot, "absent"
	}
	if result.Status == "found" && result.PullRequest.Number == p.Number {
		return result.PullRequest, result.PullRequest.State
	}
	return snapshot, ""
}

func (s *Server) reviewChatPR(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator is required to discover or analyze pull requests."
		return out
	}
	selector := plan.PRSelector
	locator, _ := selector.NumberAndRepository()
	repos, err := s.prLookupTargets(ctx, selector)
	if errors.Is(err, errPRScope) || selector.ConnectionRevision != "" && len(repos) == 0 {
		out.Plan.Kind, out.Destination, out.Text = chat.Clarify, "workflows", "The selected scope could not be verified. Ask for a new search or enter the exact workflow and connection."
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "The configured repositories could not be inspected. Retry the search.", true
		return out
	}
	if len(repos) == 0 {
		out.Text = "No configured SCM connection matches this scope. Choose a repository from Connections or configure the matching connection, then retry the PR request."
		out.Destination = "connections"
		return out
	}
	if selector.WorkflowVersion > 0 && (len(repos) != 1 || len(repos[0].reviewers) != 1) {
		out.Plan.Kind, out.Text = chat.Clarify, "The selected reviewer changed or is unavailable. Ask for a fresh PR review."
		return out
	}
	lookup, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	incomplete := len(repos) > maxPRLookupConnections
	closed := false
	var matches []prMatch
	for _, repo := range repos[:min(len(repos), maxPRLookupConnections)] {
		// Limit each repository's grant attempt and leave later repositories
		// a chance to respond when one provider stalls.
		candidates := append(slices.Clone(repo.reviewers[:min(len(repo.reviewers), 2)]), repo.readers...)
		if len(candidates) == 0 {
			incomplete = true
			continue
		}
		repoCtx, done := context.WithTimeout(lookup, 2*time.Second)
		var snapshot scm.ReviewSnapshot
		var state string
		var reader manualPRTarget
		for _, candidate := range candidates[:min(len(candidates), 2)] {
			reader = candidate
			snapshot, state = s.lookupChatPRMetadata(repoCtx, actor, candidate)
			if state != "" {
				break
			}
		}
		done()
		switch state {
		case "open":
			matches = append(matches, prMatch{repo, reader, snapshot})
		case "closed", "merged":
			closed = true
		case "absent":
		default:
			incomplete = true
		}
	}
	if len(matches) == 0 {
		switch {
		case incomplete:
			if len(repos) == 1 && supportedPRConnection(repos[0].connection) && len(repos[0].reviewers)+len(repos[0].readers) == 0 {
				c := repos[0].connection
				plan.PRSelector.Connection, plan.PRSelector.ConnectionRevision = c.Name, c.UpdatedAt.UTC().Format(time.RFC3339Nano)
				result := chat.AskDiscovery(plan, prSetupArea, []chat.Choice{{ID: "setup", Label: "Show scoped reviewer setup"}},
					"This repository has no saved pull request reader available to Chat. The PR could not be verified.", true)
				result.Destination = "workflows"
				return result
			}
			out.Text = fmt.Sprintf("I could not verify PR #%s across this scope. Some repositories lack an authorized pull request reader or did not respond. Supply the PR link or check the scoped read access in Connections and Workflows.", locator.Number)
			out.Destination = "connections"
		case closed:
			out.Text = fmt.Sprintf("PR #%s is present, but closed or merged in the checked repositories.", locator.Number)
		default:
			out.Text = fmt.Sprintf("PR #%s was not found in the checked repositories.", locator.Number)
		}
		return out
	}
	if len(matches) > 1 || incomplete {
		choices := make([]chat.Choice, 0, len(matches))
		for _, match := range matches {
			c := match.repo.connection
			label := fmt.Sprintf("%s · %s · %s · PR #%s", c.Repository, c.Kind, c.Name, locator.Number)
			if title := chat.SafePRTitle(match.snapshot.Title); title != "" {
				label += " · " + title
			}
			choices = append(choices, chat.Choice{ID: c.Name + "@" + c.UpdatedAt.UTC().Format(time.RFC3339Nano), Label: label})
		}
		return chat.AskDiscovery(plan, prRepositoryArea, choices, "Which repository contains the PR you want to review?", incomplete)
	}
	return s.reviewPRMatch(ctx, actor, id, plan, matches[0])
}

func (s *Server) reviewPRMatch(ctx context.Context, actor Identity, id string, plan chat.Plan, match prMatch) chat.Interpretation {
	c := match.repo.connection
	explicit := plan.PRSelector.Connection != "" || plan.PRSelector.Repository != "" || plan.PRSelector.URL != ""
	var selection *chat.Selection
	if explicit {
		selection = &chat.Selection{Area: "scm", ID: c.Name, Role: config.RoleOperator}
	}
	plan.PRSelector.Connection, plan.PRSelector.ConnectionRevision = c.Name, c.UpdatedAt.UTC().Format(time.RFC3339Nano)
	reviewers := match.repo.reviewers
	if len(reviewers) == 0 {
		choices := []chat.Choice{{ID: "setup", Label: "Show reviewer setup for " + c.Repository}}
		seen := make(map[string]bool)
		for _, starter := range match.repo.starters {
			p := starter.proposal
			if seen[p.Graph] {
				continue
			}
			seen[p.Graph] = true
			choices = append(choices, chat.Choice{ID: startPRChoice(p), Label: fmt.Sprintf("Review starting stopped workflow %s · Version %d", p.Graph, p.Version)})
		}
		out := chat.AskDiscovery(plan, prSetupArea, choices,
			fmt.Sprintf("I found PR #%d in %s, and no running reviewer is eligible for this repository.", match.snapshot.Number, c.Repository), false)
		out.Selection = selection
		return out
	}
	if len(reviewers) > 1 {
		choices := make([]chat.Choice, 0, min(len(reviewers), maxPRLookupConnections))
		for _, reviewer := range reviewers[:min(len(reviewers), maxPRLookupConnections)] {
			p := reviewer.proposal
			choices = append(choices, chat.Choice{ID: fmt.Sprintf("%s/%s@%d@%s", p.Graph, p.Node, p.Version, p.Run),
				Label: fmt.Sprintf("%s / %s · workflow version %d · model %s/%s", p.Graph, p.Node, p.Version, p.ModelProvider, p.ModelName)})
		}
		out := chat.AskDiscovery(plan, prReviewerArea, choices, fmt.Sprintf("Which reviewer should analyze PR #%d in %s?", match.snapshot.Number, c.Repository), len(reviewers) > len(choices))
		out.Selection = selection
		return out
	}
	target := reviewers[0]
	snapshot := match.snapshot
	if target.proposal != match.reader.proposal {
		var state string
		snapshot, state = s.lookupChatPRMetadata(ctx, actor, target)
		if state != "open" {
			return chat.Interpretation{Plan: plan, Selection: selection, Destination: "connections", Text: "The selected reviewer could not read this PR with its current grant. Check the connection and approvals before trying again."}
		}
	}
	p := target.proposal
	p.Head, p.Base, p.HeadRef, p.BaseRef, p.Draft = snapshot.HeadSHA, snapshot.BaseSHA, snapshot.Head, snapshot.Base, snapshot.Draft
	result, err := chat.NewPRReview(actor.chatOwner, id, p, time.Now())
	if err != nil {
		return chat.Interpretation{Plan: plan, Selection: selection, Text: "The selected PR lacks a complete, displayable revision identity."}
	}
	return chat.Interpretation{Plan: plan, Selection: selection, Review: result, Text: fmt.Sprintf("Review the source and destination for analysis of PR #%d in %s.", p.Number, p.Repository)}
}

func (s *Server) resolveChatPRChoice(ctx context.Context, actor Identity, session string, plan chat.Plan, area, choiceID string) chat.Interpretation {
	stale := chat.Interpretation{Plan: chat.Plan{Kind: chat.Clarify}, Text: "The saved choice is no longer available. Ask for a fresh PR search."}
	if !actor.Role.Allows(config.RoleOperator) {
		return stale
	}
	switch area {
	case prRepositoryArea:
		connection, revision, ok := strings.Cut(choiceID, "@")
		if _, err := time.Parse(time.RFC3339Nano, revision); !ok || connection == "" || err != nil {
			return stale
		}
		plan.PRSelector.Connection, plan.PRSelector.ConnectionRevision = connection, revision
	case prReviewerArea:
		parts := strings.Split(choiceID, "@")
		if len(parts) != 3 || parts[2] == "" {
			return stale
		}
		workflow, node, ok := strings.Cut(parts[0], "/")
		version, err := strconv.Atoi(parts[1])
		if !ok || err != nil || workflow == "" || node == "" || version < 1 || plan.PRSelector.ConnectionRevision == "" {
			return stale
		}
		plan.PRSelector.Workflow, plan.PRSelector.Node, plan.PRSelector.WorkflowVersion, plan.PRSelector.Run = workflow, node, version, parts[2]
	case prSetupArea:
		repos, err := s.prLookupTargets(ctx, plan.PRSelector)
		if err != nil || len(repos) != 1 || !supportedPRConnection(repos[0].connection) {
			return stale
		}
		name := repos[0].connection.Name
		selection := &chat.Selection{Area: "scm", ID: name, Role: config.RoleOperator}
		if choiceID == "setup" {
			return chat.Interpretation{Plan: plan, Selection: selection, Destination: "workflows",
				Text: fmt.Sprintf("For %s, ask in this chat to propose a Graph through installation configuration. Specify a native event reviewer, its model, one external input feeding that node alone, and a pull request read grant scoped to connection %s. Review the Graph and its grants before applying. Then review starting the workflow and ask for the PR again. Use Connections for credentials and Workflows for inspection.", repos[0].connection.Repository, name)}
		}
		for _, candidate := range repos[0].starters {
			p := candidate.proposal
			if startPRChoice(p) != choiceID {
				continue
			}
			start := chat.Plan{Kind: chat.ControlWorkflow, Control: chat.WorkflowControlDraft{Workflow: p.Graph, Action: "start"}}
			out := s.reviewChatWorkflowControl(ctx, actor, session, start)
			if out.Review == nil || out.Review.WorkflowControl.Scope.Version != p.Version || out.Review.WorkflowControl.Scope.RunID != p.Run {
				return stale
			}
			out.Selection = selection
			return out
		}
		return stale
	default:
		return stale
	}
	return s.reviewChatPR(ctx, actor, session, plan)
}

func startPRChoice(target chat.PRAnalysis) string {
	return fmt.Sprintf("start/%s@%d@%s", target.Graph, target.Version, target.Run)
}
