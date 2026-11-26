package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/engine"
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
	if !review.CanStart || draft.Status != "draft" || review.EnvironmentDefinition != nil ||
		draft.Source != nil || draft.ProjectID != "" || len(draft.Repositories) != 0 || draft.SCMConnection != "" {
		out.Text, out.Destination = "Inspect this task in Tasks to review its authorization, environment, repositories or existing run.", "tasks"
		return out
	}
	source, err := s.Store.GetGraph(ctx, draft.GraphID, draft.GraphVersion)
	if err != nil || source.Spec.Workspace.Repo != "" {
		out.Text, out.Destination = "Inspect the workflow version and its workspace in Tasks before starting.", "tasks"
		return out
	}
	authorization, err := (taskauth.Manager{Store: s.Store}).Review(ctx, id)
	if err != nil {
		out.Text, out.Destination = "Inspect the task's authorization in Tasks before starting.", "tasks"
		return out
	}
	if err := (cost.Budgets{Store: s.Store}).CheckAdmission(ctx, cost.Target{User: draft.CreatedBy, Workflow: draft.GraphID}); err != nil {
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
		Revision: draft.Revision, Consent: authorization.StartConsent(),
	}
	for _, scope := range authorization.Capabilities {
		agent := chat.AgentStartGrant{ID: scope.ID, Isolation: string(scope.Isolation)}
		for _, grant := range scope.Grants {
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
		_, err := s.taskRunner().StartForOperation(ctx, target.ID, target.Revision, actor.Name, review.ID, target.Consent)
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
