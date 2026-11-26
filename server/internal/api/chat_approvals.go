package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) reviewChatApproval(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must decide a pending approval."
		return out
	}
	taskID := ""
	approvalID := plan.Approval.Approval
	if plan.Task != "" {
		id, err := s.resolveChatTask(ctx, plan.Task)
		if errors.Is(err, storage.ErrNotFound) {
			out.Text, out.Plan.Kind = "Enter the saved task ID or give me the exact approval ID.", chat.Clarify
			return out
		}
		if err != nil {
			out.Text, out.Retryable = "Could not resolve the task. Retry the request.", true
			return out
		}
		progress, err := s.taskRunner().View(ctx, id)
		if err != nil {
			out.Text, out.Retryable = "Could not inspect the task's approvals. Retry the request.", true
			return out
		}
		if len(progress.PendingApprovals) != 1 {
			out.Text, out.Plan.Kind = "This task does not identify one pending decision. Enter the exact approval ID.", chat.Clarify
			out.Destination = "approvals"
			return out
		}
		taskID, approvalID = id, progress.PendingApprovals[0].ID
	}
	native, err := s.Store.GetApproval(ctx, approvalID)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "That approval ID was not found. Enter the exact pending approval ID from Approvals.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not read the approval. Retry the request.", true
		return out
	}
	now := time.Now()
	if native.Status != domain.ApprovalPending || (native.ExpiresAt != nil && !now.Before(*native.ExpiresAt)) {
		out.Text, out.Destination = "This approval is already decided or expired. Inspect its current state in Approvals.", "approvals"
		return out
	}
	if native.Kind == domain.ApprovalQuestion && plan.Approval.Decision == "approve" && plan.Approval.Answer == "" {
		out.Text, out.Plan.Kind = "What reply should I send for this question? Include the approval ID.", chat.Clarify
		return out
	}
	decision := chat.ApprovalDecision{
		ID: native.ID, Title: native.Title, Kind: native.Kind, GraphID: native.GraphID, NodeID: native.NodeID,
		Replica: native.Replica, ExecutionID: native.ExecutionID, TaskID: taskID, Fingerprint: installation.Digest(native),
		Decision: plan.Approval.Decision, Answer: plan.Approval.Answer, Comment: plan.Approval.Comment,
	}
	if native.Kind == domain.ApprovalQuestion {
		var detail struct {
			Question string `json:"question"`
		}
		if json.Unmarshal(native.Detail, &detail) != nil || detail.Question == "" {
			out.Text, out.Destination = "Open Approvals to inspect the original question before deciding.", "approvals"
			return out
		}
		decision.Title = detail.Question
	}
	review, err := chat.NewApprovalDecisionReview(actor.chatOwner, sessionID, decision, native.ExpiresAt, now)
	if err != nil {
		out.Text, out.Destination = "Open Approvals to review this request and decide it with its full details.", "approvals"
		return out
	}
	out.Text, out.Review = "Review the exact pending approval and the proposed decision.", review
	return out
}

func (s *Server) applyChatApproval(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	decision := review.Approval
	native, err := s.Store.GetApproval(ctx, decision.ID)
	var resolved *domain.Approval
	if err == nil {
		if native.Status != domain.ApprovalPending || decision.Fingerprint != installation.Digest(native) ||
			(native.ExpiresAt != nil && !time.Now().Before(*native.ExpiresAt)) {
			err = hitl.ErrNotPending
		} else {
			resolved, err = s.Approvals.DecideReviewed(ctx, native, decision.Decision == "approve",
				actor.Name, decision.Comment, decision.Answer, review.ID)
		}
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	state := chat.ReviewUnknown
	if resolved != nil && resolved.DecisionOperation == review.ID {
		state = chat.ReviewApplied
	} else if saved, readErr := s.Store.GetApproval(saveCtx, decision.ID); readErr == nil && saved.DecisionOperation == review.ID {
		state = chat.ReviewApplied
	} else if errors.Is(err, hitl.ErrNotPending) || errors.Is(err, storage.ErrNotFound) ||
		(native != nil && native.ExpiresAt != nil && !time.Now().Before(*native.ExpiresAt)) {
		state = chat.ReviewConflict
	}
	return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) reconcileChatApproval(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	decision, err := s.Store.GetApproval(ctx, review.Approval.ID)
	if err == nil && decision.DecisionOperation == review.ID {
		return s.chatStore().FinishReview(ctx, owner, session.ID, review.ID, chat.ReviewApplied, 0)
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) > 20*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}
