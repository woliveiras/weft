package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) reviewChatScale(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must change a workflow's replica count."
		return out
	}
	workflow, err := s.findChatWorkflow(ctx, plan.Scale.Workflow)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which reusable workflow should I scale? Enter its exact ID or unique name.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not find the workflow. Retry the saved request.", true
		return out
	}
	node := resolveChatControlNode(workflow, plan.Scale.Node)
	if node == "" {
		out.Text, out.Plan.Kind = "Which agent should I scale? Enter its exact node ID.", chat.Clarify
		return out
	}
	scope, err := s.Engine.WorkflowScaleScope(ctx, workflow.Metadata.ID, node)
	if err != nil {
		out.Text, out.Destination = "Could not verify the reusable workflow and its replica controls. Inspect the node in Workflows.", "workflows"
		return out
	}
	count, ok := plan.Scale.SizeFor(scope)
	if !ok {
		out.Text, out.Destination = "This count does not match the node's current replica policy or lifecycle. Inspect the existing count and policy in Workflows.", "workflows"
		return out
	}
	out.Review, err = chat.NewWorkflowScaleReview(actor.chatOwner, sessionID, chat.WorkflowScale{Scope: scope, Size: count}, time.Now())
	if err != nil {
		out.Text, out.Retryable = "Could not prepare a safe replica review. Retry the saved request.", true
		return out
	}
	out.Text = "Review the node, version and scaling effect before changing its replica policy."
	return out
}

func (s *Server) applyChatScale(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	target := review.WorkflowScale
	err := s.Engine.ApplyReviewedWorkflowScale(ctx, target.Scope, target.Size, actor.Name, review.ID)
	observe, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	result, found, readErr := s.Engine.ObserveReviewedWorkflowScale(observe, target.Scope, target.Size, review.ID)
	if found {
		return s.chatStore().RecordWorkflowScale(observe, actor.chatOwner, sessionID, review.ID, scaleReviewState(result), result)
	}
	if readErr != nil && !errors.Is(readErr, engine.ErrNotFound) && !errors.Is(readErr, engine.ErrConflict) {
		s.Log.Warn("could not recover scale receipt", "operation", review.ID, "err", readErr)
	}
	var invalid *engine.ValidationError
	state := chat.ReviewUnknown
	if errors.Is(err, engine.ErrConflict) || errors.Is(err, engine.ErrNotFound) || errors.Is(err, engine.ErrInvalid) || errors.As(err, &invalid) {
		state = chat.ReviewConflict
	}
	return s.chatStore().RecordWorkflowScale(observe, actor.chatOwner, sessionID, review.ID, state, result)
}

func scaleReviewState(result engine.WorkflowScaleObservation) string {
	switch result.State {
	case "saved", "observed":
		return chat.ReviewApplied
	case "superseded", "failed":
		return chat.ReviewIncomplete
	default:
		return chat.ReviewAccepted
	}
}

func (s *Server) observeChatScale(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	target := review.WorkflowScale
	result, found, err := s.Engine.ObserveReviewedWorkflowScale(ctx, target.Scope, target.Size, review.ID)
	if err != nil && !errors.Is(err, engine.ErrConflict) && !errors.Is(err, engine.ErrNotFound) {
		return nil, err
	}
	if found {
		state := scaleReviewState(result)
		if state == chat.ReviewAccepted && time.Since(review.ClaimedAt) > 2*time.Minute {
			state = chat.ReviewIncomplete
		}
		return s.chatStore().RecordWorkflowScale(ctx, owner, session.ID, review.ID, state, result)
	}
	if review.State != chat.ReviewUnknown && time.Since(review.ClaimedAt) > 20*time.Second {
		if review.ScaleResult != nil {
			result = *review.ScaleResult
		}
		return s.chatStore().RecordWorkflowScale(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, result)
	}
	return session, nil
}
