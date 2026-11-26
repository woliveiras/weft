package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func (s *Server) reviewChatTrigger(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must review issue intake changes."
		return out
	}
	manager := s.triggers()
	target, err := manager.Get(ctx, plan.Trigger.Target)
	if errors.Is(err, storage.ErrNotFound) {
		target, err = manager.FindByName(ctx, plan.Trigger.Target)
		if err != nil || target == nil {
			out.Text, out.Plan.Kind = "Which issue trigger should change? Enter its exact ID or a unique name.", chat.Clarify
			return out
		}
	}
	if err != nil {
		out.Text, out.Retryable = "Could not read the issue trigger. Retry the saved request.", true
		return out
	}
	scope, err := manager.ReviewIntakeControl(ctx, target, plan.Trigger.Action, actor.Name)
	if err != nil {
		out.Text, out.Destination = "The trigger cannot make this change in its current state. Open its workflow's Triggers settings to inspect the state and requirements.", "workflows"
		return out
	}
	out.Review, err = chat.NewTriggerControlReview(actor.chatOwner, sessionID, scope, time.Now())
	if err != nil {
		out.Text, out.Destination = "Review the trigger's complete scope in Workflows → Settings → Triggers.", "workflows"
		return out
	}
	out.Text = "Review the source and action for future issue intake. Work already admitted has its own lifecycle."
	return out
}

func (s *Server) applyChatTrigger(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	scope := review.TriggerControl
	manager := s.triggers()
	apply := func(ctx context.Context) error {
		_, err := manager.ApplyReviewedIntake(ctx, *scope, actor.Name, review.ID)
		return err
	}
	var err error
	if scope.Action == "resume" {
		err = s.Engine.WithGraphTransition(ctx, scope.Workflow, apply)
	} else {
		err = apply(ctx)
	}
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	state := chat.ReviewApplied
	if err != nil {
		var invalid *trigger.FieldError
		state = chat.ReviewUnknown
		if errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) || errors.Is(err, engine.ErrConflict) || errors.As(err, &invalid) {
			state = chat.ReviewConflict
		}
		if found, readErr := s.chatTriggerReceipt(save, review); found {
			state = chat.ReviewApplied
		} else if readErr != nil {
			s.Log.Warn("read reviewed issue intake receipt", "operation", review.ID, "err", readErr)
		}
	}
	return s.chatStore().FinishReview(save, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) chatTriggerReceipt(ctx context.Context, review *chat.OperationReview) (bool, error) {
	target, err := s.triggers().Get(ctx, review.TriggerControl.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return target.ControlOperation == review.ID, nil
}
