package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) reviewChatBudgetRaise(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must review a budget increase."
		return out
	}
	draft := plan.Budget
	now := time.Now()
	var id, subject string
	if draft.Scope == cost.TaskRun {
		taskID, err := s.resolveChatTask(ctx, draft.Target)
		if errors.Is(err, storage.ErrNotFound) {
			out.Text, out.Plan.Kind = "Which task should this raise apply to? Enter its ID or exact title.", chat.Clarify
			return out
		}
		if err != nil {
			out.Text, out.Retryable = "Could not resolve the task. Retry the saved request.", true
			return out
		}
		task, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, taskID)
		if err != nil {
			out.Text, out.Retryable = "Could not read the saved task. Retry the saved request.", true
			return out
		}
		if task.ExecutionGraphID == "" || task.Status != "starting" && task.Status != "running" {
			out.Text, out.Destination = "This task has no active attempt to raise. Inspect its status in Tasks.", "tasks"
			return out
		}
		id, subject = task.ExecutionGraphID, task.Title
		if chat.ValidateMessage(subject) != nil {
			subject = taskID
		}
	} else {
		id, subject = draft.Target, draft.Target
		if draft.Scope == cost.Installation {
			id, subject = "*", "Installation"
		}
		id = cost.MonthlyID(id, cost.Period(now))
	}
	target, err := s.budgets().ReviewRaise(ctx, draft.Scope, id, now)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		out.Text, out.Destination = "No existing ledger matches that target for the current period or attempt. Inspect the budget settings in Models.", "budgets"
		return out
	case errors.Is(err, cost.ErrInvalidLedger):
		out.Text, out.Destination = "The budget ledger requires repair. Inspect it in Models before proposing a change.", "budgets"
		return out
	case err != nil:
		out.Text, out.Retryable = "Could not read the budget ledger. Retry the saved request.", true
		return out
	}
	if draft.Currency != target.Currency {
		out.Text, out.Plan.Kind = "The currency differs from this ledger. Check Models → Budgets and specify the new limit in the ledger's currency.", chat.Clarify
		return out
	}
	if draft.LimitMicros() <= target.Limit {
		out.Text, out.Plan.Kind = "The proposed limit must exceed the current allowance. What higher total should apply?", chat.Clarify
		return out
	}
	out.Review, err = chat.NewBudgetRaiseReview(actor.chatOwner, sessionID, chat.BudgetRaise{Target: target, Subject: subject, Actor: actor.Name, Limit: draft.LimitMicros()}, now)
	if err != nil {
		out.Text, out.Destination = "This ledger needs a manual review in Models. Open the budget settings to inspect the full scope.", "budgets"
		return out
	}
	out.Text = "Review the new limit for this ledger and any runs held by it before confirming."
	return out
}

func (s *Server) applyChatBudgetRaise(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	raise := review.BudgetRaise
	err := storage.ErrConflict
	if actor.Name == raise.Actor {
		err = s.Engine.RaiseReviewedBudget(ctx, raise.Target, raise.Limit, actor.Name, review.ID)
	}
	observe, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if result, state, observed := s.chatBudgetRaiseResult(observe, review); observed {
		return s.chatStore().RecordBudgetRaise(observe, actor.chatOwner, sessionID, review.ID, state, result)
	}
	var invalid *cost.ValidationError
	state := chat.ReviewUnknown
	if errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) || errors.Is(err, cost.ErrInvalidLedger) || errors.As(err, &invalid) {
		state = chat.ReviewConflict
	}
	return s.chatStore().RecordBudgetRaise(observe, actor.chatOwner, sessionID, review.ID, state, chat.BudgetRaiseResult{})
}

func (s *Server) chatBudgetRaiseResult(ctx context.Context, review *chat.OperationReview) (chat.BudgetRaiseResult, string, bool) {
	raise := review.BudgetRaise
	receipt, err := s.budgets().ReviewedRaiseReceipt(ctx, raise.Target, raise.Limit, raise.Actor, review.ID)
	if err != nil {
		s.Log.Warn("read reviewed budget receipt", "operation", review.ID, "err", err)
	}
	if err != nil || receipt == nil {
		return chat.BudgetRaiseResult{}, "", false
	}
	result := chat.BudgetRaiseResult{Revision: receipt.Revision, Reached: receipt.Reached}
	state := chat.ReviewApplied
	for _, action := range receipt.Actions {
		outcome, err := s.Engine.ObserveBudgetResume(ctx, action)
		if err != nil {
			s.Log.Warn("observe budget resumption", "operation", review.ID, "err", err)
			outcome = "pending"
		}
		if outcome == "pending" {
			state = chat.ReviewAccepted
		}
		result.Resumes = append(result.Resumes, chat.BudgetResume{Graph: action.Graph, Run: action.RunID, State: outcome})
	}
	return result, state, true
}

func (s *Server) observeChatBudgetRaise(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	if result, state, observed := s.chatBudgetRaiseResult(ctx, review); observed {
		return s.chatStore().RecordBudgetRaise(ctx, owner, session.ID, review.ID, state, result)
	}
	if review.State != chat.ReviewUnknown && time.Since(review.ClaimedAt) > 20*time.Second {
		var recorded chat.BudgetRaiseResult
		if review.BudgetResult != nil {
			recorded = *review.BudgetResult
		}
		return s.chatStore().RecordBudgetRaise(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, recorded)
	}
	return session, nil
}
