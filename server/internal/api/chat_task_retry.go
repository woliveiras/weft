package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) reviewChatTaskRetry(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must prepare a retry."
		return out
	}
	id, err := s.resolveChatTask(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which failed manual task should I retry? Enter its task ID or unique title.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not find the task. Retry the saved request.", true
		return out
	}
	source, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, id)
	if err != nil {
		out.Text, out.Retryable = "Could not verify the task state. Retry the saved request.", true
		return out
	}
	if !source.CanRetryFailure() {
		out.Text, out.Destination = "This task is not an eligible failed manual task. Open Tasks to inspect its current state and recovery options.", "tasks"
		return out
	}
	title := source.Title
	if chat.ValidateMessage(title) != nil {
		title = source.ID
	}
	repos := len(source.Repositories)
	if source.ProjectID != "" {
		repos = 1
	}
	out.Review, err = chat.NewTaskRetryReview(actor.chatOwner, sessionID, chat.TaskRetry{
		ID: source.ID, Title: title, Revision: source.Revision, WorkflowID: source.GraphID,
		WorkflowVersion: source.GraphVersion, RepositoryCount: repos,
	}, time.Now())
	if err != nil {
		out.Text, out.Retryable = "Could not prepare the reviewed retry. Retry the saved request.", true
		return out
	}
	out.Text = "Review the failed attempt before preparing a linked retry draft."
	return out
}

func (s *Server) applyReviewedChatTaskRetry(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	target := review.TaskRetry
	manager := taskconfig.Manager{Store: s.Store}
	child, _, err := manager.RetryFailed(ctx, target.ID, target.Revision, actor.Name)
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err == nil {
		return s.chatStore().FinishTaskRetry(saveCtx, actor.chatOwner, sessionID, review.ID, child.ID, child.Revision)
	}
	state := chat.ReviewUnknown
	if errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) {
		state = chat.ReviewConflict
	} else if saved, readErr := manager.RetryDraft(saveCtx, target.ID, target.Revision); readErr == nil {
		return s.chatStore().FinishTaskRetry(saveCtx, actor.chatOwner, sessionID, review.ID, saved.ID, saved.Revision)
	}
	return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) reconcileChatTaskRetry(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	retry := review.TaskRetry
	draft, err := (taskconfig.Manager{Store: s.Store}).RetryDraft(ctx, retry.ID, retry.Revision)
	if err == nil {
		return s.chatStore().FinishTaskRetry(ctx, owner, session.ID, review.ID, draft.ID, draft.Revision)
	}
	if !errors.Is(err, storage.ErrNotFound) && !errors.Is(err, storage.ErrConflict) {
		return nil, err
	}
	if review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) > 20*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}
