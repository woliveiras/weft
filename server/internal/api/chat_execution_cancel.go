package api

import (
	"context"
	"errors"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"github.com/woliveiras/weft/server/internal/storage"
	"time"
)

func (s *Server) reviewChatExecutionCancel(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must request execution cancellation."
		return out
	}
	manager := executioncontrol.Manager{Store: s.Store}
	tree, err := manager.Tree(ctx, plan.ExecutionID)
	if err != nil {
		out.Text = "Could not resolve that execution tree. Inspect the execution in Activity."
		out.Destination = "activity"
		return out
	}
	scope := executioncontrol.CancelScope{Selected: tree.SelectedID, Root: tree.RootID, Mode: plan.ExecutionScope}
	active := false
	for _, n := range tree.Nodes {
		x := n.Execution
		if x.ID == scope.Selected {
			scope.Graph, scope.Run = x.GraphID, x.RunID
		}
		if (scope.Mode == "tree" || x.ID == scope.Selected) && !x.Status.Terminal() {
			active = true
		}
		if x.ID == scope.Selected && n.Cancellation != nil {
			out.Text = "A cancellation is already recorded for this execution. Inspect its current state."
			return out
		}
	}
	if !active {
		out.Text = "The selected execution scope is already terminal."
		return out
	}
	out.Review, err = chat.NewExecutionCancelReview(actor.chatOwner, sessionID, scope, time.Now())
	if err != nil {
		out.Text = "Choose an exact delegated child or its execution tree."
		out.Plan.Kind = chat.Clarify
		return out
	}
	out.Text = "Review the exact execution and cancellation scope."
	return out
}
func (s *Server) applyChatExecutionCancel(ctx context.Context, actor Identity, sessionID string, r *chat.OperationReview) (*chat.Session, error) {
	manager := executioncontrol.Manager{Store: s.Store}
	result, err := manager.CancelReviewed(ctx, *r.ExecutionCancel, actor.Name, r.ID)
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	if err == nil {
		if err = s.publishExecutionCancellation(save, actor.Name, result); err != nil {
			s.Log.Warn("execution cancellation event unavailable", "operation", r.ID)
		}
	}
	if errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) || errors.Is(err, executioncontrol.ErrInvalid) || result.AlreadyTerminal && result.Request == nil {
		return s.chatStore().RecordExecutionCancel(save, actor.chatOwner, sessionID, r.ID, chat.ReviewConflict)
	}
	session, readErr := s.chatStore().Get(save, actor.chatOwner, sessionID)
	if readErr != nil {
		return nil, readErr
	}
	return s.observeChatExecutionCancel(save, actor.chatOwner, session, r)
}
func (s *Server) observeChatExecutionCancel(ctx context.Context, owner string, session *chat.Session, r *chat.OperationReview) (*chat.Session, error) {
	found, settled, cancelled, err := (executioncontrol.Manager{Store: s.Store}).ObserveReview(ctx, *r.ExecutionCancel, r.ID)
	state := chat.ReviewUnknown
	switch {
	case errors.Is(err, storage.ErrConflict):
		state = chat.ReviewConflict
	case err != nil && !errors.Is(err, storage.ErrNotFound):
		return nil, err
	case found && settled && cancelled:
		state = chat.ReviewApplied
	case found && settled:
		state = chat.ReviewIncomplete
	case found:
		state = chat.ReviewAccepted
	case time.Since(r.ClaimedAt) < 20*time.Second:
		return session, nil
	}
	return s.chatStore().RecordExecutionCancel(ctx, owner, session.ID, r.ID, state)
}
