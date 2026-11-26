package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) reviewChatTaskStart(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must start a task."
		return out
	}
	id, err := s.resolveChatTask(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which saved task should I start? Enter its task ID or unique title.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not find the saved task. Retry the request.", true
		return out
	}
	review, err := s.taskRunner().Review(ctx, id)
	if err != nil {
		out.Text, out.Destination = "Could not review the task. Inspect its readiness in Tasks.", "tasks"
		return out
	}
	draft := review.Task
	build := review.PreparedBuild()
	if !review.CanStart || (draft.Status != "draft" && build == nil) || (review.EnvironmentDefinition != nil && build == nil) {
		out.Text, out.Destination = "Inspect this task in Tasks to review its authorization, environment, repositories or existing run.", "tasks"
		return out
	}
	source, err := s.Store.GetGraph(ctx, draft.GraphID, draft.GraphVersion)
	if err != nil || (source.Spec.Workspace.Repo != "" && draft.ProjectID == "" && len(draft.Repositories) == 0) {
		out.Text, out.Destination = "Inspect the workflow version and its workspace in Tasks before starting.", "tasks"
		return out
	}
	authorization, err := (taskauth.Manager{Store: s.Store}).Review(ctx, id)
	if err != nil {
		out.Text, out.Destination = "Inspect the task's authorization in Tasks before starting.", "tasks"
		return out
	}
	if err := (cost.Budgets{Store: s.Store}).CheckAdmission(ctx, cost.Target{Projects: domain.RepositoryProjectIDs(draft.Repositories), User: draft.CreatedBy, Project: draft.ProjectID, Workflow: draft.GraphID}); err != nil {
		if errors.Is(err, cost.ErrBudgetReached) {
			out.Text, out.Destination = "The current budget blocks admission. Inspect Budgets before retrying.", "budgets"
		} else {
			out.Text, out.Retryable = "Could not check the current task budget. Retry the saved request.", true
		}
		return out
	}
	title := draft.Title
	if chat.ValidateMessage(title) != nil {
		title = draft.ID
	}
	start := chat.TaskAdmission{
		ID: draft.ID, Title: title, WorkflowID: draft.GraphID, Version: draft.GraphVersion,
		Revision: draft.Revision, Consent: authorization.StartConsent(), Build: build,
	}
	start.Repositories, err = s.chatTaskRepositories(ctx, draft)
	if err != nil {
		out.Text, out.Destination = "Inspect the task repositories and any external delivery settings in Tasks before starting.", "tasks"
		return out
	}
	if d := draft.Source; d != nil {
		tool, args, err := d.ReadCall()
		if err != nil {
			out.Text, out.Destination = "Inspect the issue source in Tasks.", "tasks"
			return out
		}
		start.IssueSource = fmt.Sprintf("%s · %s", tool, args)
	}
	for _, scope := range review.Agents {
		agent := chat.AgentStartGrant{ID: scope.ID, Isolation: string(scope.Isolation)}
		for _, grant := range scope.Grants {
			if len(grant.Paths)+len(grant.Commands)+len(grant.Hosts)+len(grant.Repositories) > 0 || grant.RequiresApproval || grant.RateLimit > 0 {
				raw, err := json.Marshal(grant)
				if err != nil || chat.ValidateMessage(string(raw)) != nil {
					out.Text, out.Destination = "Inspect the complete tool grant in Tasks before starting.", "tasks"
					return out
				}
				agent.Constraints = append(agent.Constraints, string(raw))
			}
			if chat.ValidateMessage(grant.Name) == nil {
				agent.Tools = append(agent.Tools, grant.Name)
			} else {
				agent.Tools = append(agent.Tools, "Restricted grant")
			}
		}
		start.AgentGrants = append(start.AgentGrants, agent)
	}
	out.Review, err = chat.NewTaskStartReview(actor.chatOwner, sessionID, start, time.Now())
	if err != nil {
		out.Text, out.Retryable = "Could not prepare a reviewed start. Retry the saved request.", true
		return out
	}
	out.Text = "Review task admission before starting. The actual result will appear separately."
	return out
}

func (s *Server) applyReviewedChatTaskStart(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	target := review.TaskStart
	err := s.Engine.WithGraphTransition(ctx, target.WorkflowID, func(ctx context.Context) error {
		var guards []storage.SettingRevision
		for _, repo := range target.Repositories {
			project, err := (projectconfig.Manager{Store: s.Store}).Get(ctx, repo.Project)
			if err != nil {
				return err
			}
			if project.Revision != repo.Revision {
				return storage.ErrConflict
			}
			guards = append(guards, projectconfig.RevisionGuard(repo.Project, repo.Revision))
		}
		if target.Build != nil {
			_, err := s.taskRunner().StartPreparedForOperation(ctx, target.ID, target.Revision, actor.Name, review.ID, target.Consent, *target.Build, guards...)
			return err
		}
		_, err := s.taskRunner().StartForOperation(ctx, target.ID, target.Revision, actor.Name, review.ID, target.Consent, guards...)
		return err
	})
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	current, readErr := (taskconfig.Manager{Store: s.Store}).Get(saveCtx, target.ID)
	if readErr == nil && current.StartOperation == review.ID {
		return s.recordChatTaskAdmission(saveCtx, actor.chatOwner, sessionID, review.ID, current)
	}
	state := chat.ReviewUnknown
	if readErr == nil && (errors.Is(err, storage.ErrConflict) || errors.Is(err, taskconfig.ErrInvalid) ||
		errors.Is(err, taskauth.ErrInvalid) || errors.Is(err, engine.ErrConflict) || errors.Is(err, cost.ErrBudgetReached)) {
		state = chat.ReviewConflict
	}
	return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) recordChatTaskAdmission(ctx context.Context, owner, sessionID, reviewID string, draft *taskconfig.Draft) (*chat.Session, error) {
	state, err := s.observeChatTask(ctx, draft.ID)
	if err != nil {
		state = chat.TaskSummary{ID: draft.ID, GraphID: draft.ExecutionGraphID, State: draft.Status}
	}
	return s.chatStore().FinishTaskStart(ctx, owner, sessionID, reviewID, state)
}

func (s *Server) reconcileChatTaskStart(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	draft, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, review.TaskStart.ID)
	if err == nil && draft.StartOperation == review.ID {
		return s.recordChatTaskAdmission(ctx, owner, session.ID, review.ID, draft)
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) > 35*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}
