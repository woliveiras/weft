package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func (s *Server) reviewChatTaskCancel(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must cancel a running task."
		return out
	}
	id, err := s.resolveChatTask(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which running task should I cancel? Enter its task ID or unique title.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not find the task. Retry the saved request.", true
		return out
	}
	view, err := s.taskRunner().View(ctx, id)
	if err != nil {
		out.Text, out.Retryable = "Could not verify the task state. Retry the saved request.", true
		return out
	}
	task := view.Task
	if (view.State != "running" && view.State != "starting") || task.ExecutionGraphID != domain.TaskExecutionID(id) {
		out.Text, out.Destination = "This task does not have a confirmed running attempt. Inspect its state and controls in Tasks.", "tasks"
		return out
	}
	graph, run, err := s.Engine.GetGraphDocument(ctx, task.ExecutionGraphID, 0)
	if err != nil || graph.Metadata.Labels[domain.LabelTask] != id || run.RunID == "" || run.Version != graph.Metadata.Version {
		out.Text, out.Destination = "Could not verify the exact active run. Inspect this task in Tasks before trying again.", "tasks"
		return out
	}
	title := task.Title
	if chat.ValidateMessage(title) != nil {
		title = id
	}
	out.Review, err = chat.NewTaskCancellationReview(actor.chatOwner, sessionID, chat.TaskCancellation{
		ID: id, Title: title, Revision: task.Revision, ExecutionGraphID: task.ExecutionGraphID,
		ExecutionVersion: graph.Metadata.Version, RunID: run.RunID, PendingApprovals: len(view.PendingApprovals),
	}, time.Now())
	if err != nil {
		out.Text, out.Retryable = "Could not prepare the cancellation review. Retry the saved request.", true
		return out
	}
	out.Text = "Review the exact task attempt before requesting cancellation."
	return out
}

func (s *Server) applyChatTaskCancel(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	target := review.TaskCancel
	draft, err := s.taskRunner().CancelForOperation(ctx, target.ID, actor.Name, taskrun.ReviewedCancellation{
		Revision: target.Revision, Version: target.ExecutionVersion, RunID: target.RunID, Operation: review.ID,
	})
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	if draft != nil && draft.CancelOperation == review.ID {
		return s.recordChatTaskCancellation(saveCtx, actor.chatOwner, sessionID, review.ID, draft)
	}
	state := chat.ReviewUnknown
	if saved, readErr := (taskconfig.Manager{Store: s.Store}).Get(saveCtx, target.ID); readErr == nil && saved.CancelOperation == review.ID {
		return s.recordChatTaskCancellation(saveCtx, actor.chatOwner, sessionID, review.ID, saved)
	}
	if errors.Is(err, engine.ErrConflict) || errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) {
		state = chat.ReviewConflict
	}
	return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) recordChatTaskCancellation(ctx context.Context, owner, sessionID, operation string, draft *taskconfig.Draft) (*chat.Session, error) {
	state, err := s.observeChatTask(ctx, draft.ID)
	if err != nil {
		state = chat.TaskSummary{ID: draft.ID, GraphID: draft.ExecutionGraphID, State: draft.Status}
	}
	return s.chatStore().FinishTaskCancel(ctx, owner, sessionID, operation, state)
}

func (s *Server) reconcileChatTaskCancel(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	draft, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, review.TaskCancel.ID)
	if err == nil && draft.CancelOperation == review.ID {
		return s.recordChatTaskCancellation(ctx, owner, session.ID, review.ID, draft)
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) > 20*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}
