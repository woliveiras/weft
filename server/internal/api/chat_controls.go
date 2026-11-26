package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) reviewChatWorkflowControl(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must control a workflow or its agents."
		return out
	}
	draft := plan.Control
	workflow, err := s.findChatWorkflow(ctx, draft.Workflow)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which reusable workflow should I control? Enter its exact ID or a unique workflow name.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not find the workflow. Retry the saved request.", true
		return out
	}
	node := ""
	if draft.Node != "" {
		node = resolveChatControlNode(workflow, draft.Node)
		if node == "" {
			out.Text, out.Plan.Kind = "Which agent in this workflow should the control affect? Enter its exact node ID.", chat.Clarify
			return out
		}
	}
	scope, err := s.Engine.WorkflowControlScope(ctx, workflow.Metadata.ID, node)
	if err != nil {
		out.Text, out.Retryable = "Could not verify the current workflow controls. Retry the saved request.", true
		return out
	}
	if !scope.CanApply(draft.Action) {
		out.Text, out.Destination = "That action is unavailable in the current workflow or node state. Inspect Workflows for its current controls.", "workflows"
		return out
	}
	out.Review, err = chat.NewWorkflowControlReview(actor.chatOwner, sessionID, chat.WorkflowControl{Scope: scope, Action: draft.Action}, time.Now())
	if err != nil {
		out.Text, out.Retryable = "Could not prepare the reviewed workflow control. Retry the saved request.", true
		return out
	}
	out.Text = "Review the exact scope and effect before changing execution."
	return out
}

func resolveChatControlNode(workflow *domain.Graph, selector string) string {
	if exact := workflow.Node(selector); exact != nil {
		return exact.ID
	}
	var match string
	for _, node := range workflow.Spec.Nodes {
		if strings.EqualFold(node.DisplayName(), selector) {
			if match != "" {
				return ""
			}
			match = node.ID
		}
	}
	return match
}

func (s *Server) applyReviewedChatControl(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	command := review.WorkflowControl
	var before func(context.Context) error
	switch command.Action {
	case "stop", "kill", "pause":
		if command.Scope.NodeID == "" && s.Proposals != nil {
			before = func(ctx context.Context) error {
				return s.Proposals.DisableResume(ctx, command.Scope.WorkflowID)
			}
		}
	}
	err := s.Engine.ApplyReviewedWorkflowControl(ctx, command.Scope, command.Action, actor.Name, review.ID, before)
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	result, found, readErr := s.Engine.ObserveReviewedWorkflowControl(saveCtx, command.Scope, command.Action, review.ID)
	if readErr != nil && !errors.Is(readErr, engine.ErrNotFound) && !errors.Is(readErr, engine.ErrConflict) {
		return nil, fmt.Errorf("read reviewed workflow control receipt: %w", readErr)
	}
	state := chat.ReviewUnknown
	if found {
		state = chat.ReviewAccepted
		if err != nil || result.Incomplete {
			state = chat.ReviewIncomplete
		} else if result.Settled {
			state = chat.ReviewApplied
		}
		return s.chatStore().RecordWorkflowControl(saveCtx, actor.chatOwner, sessionID, review, state, &result)
	}
	var invalid *engine.ValidationError
	if errors.Is(err, engine.ErrConflict) || errors.Is(err, engine.ErrNotFound) || errors.As(err, &invalid) {
		state = chat.ReviewConflict
	}
	return s.chatStore().RecordWorkflowControl(saveCtx, actor.chatOwner, sessionID, review, state, nil)
}

func (s *Server) reconcileChatWorkflowControl(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	result, found, err := s.Engine.ObserveReviewedWorkflowControl(ctx, review.WorkflowControl.Scope, review.WorkflowControl.Action, review.ID)
	if err != nil && !errors.Is(err, engine.ErrNotFound) && !errors.Is(err, engine.ErrConflict) {
		return nil, err
	}
	if found {
		state := chat.ReviewAccepted
		if result.Incomplete {
			state = chat.ReviewIncomplete
		} else if result.Settled {
			state = chat.ReviewApplied
		}
		return s.chatStore().RecordWorkflowControl(ctx, owner, session.ID, review, state, &result)
	}
	if review.State == chat.ReviewAccepted || (review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) > 20*time.Second) {
		return s.chatStore().RecordWorkflowControl(ctx, owner, session.ID, review, chat.ReviewUnknown, review.ControlResult)
	}
	return session, nil
}
