package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) reviewChatTaskCorrection(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan, Destination: "tasks"}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must edit a task draft."
		return out
	}
	task, err := s.resolveChatTask(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which draft should I edit? Enter its ID or unique title.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not look up the task.", true
		return out
	}
	d, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, task)
	if err != nil {
		out.Text, out.Retryable = "Could not read the draft.", true
		return out
	}
	if d.Status != "draft" || d.GraphID == "" {
		out.Text = "Open Tasks to resolve the workflow or inspect the task’s current state."
		return out
	}
	g, err := s.Store.GetGraph(ctx, d.GraphID, 0)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && g.Metadata.Version != d.GraphVersion) {
		out.Text = "Review the workflow binding in Tasks before editing this draft."
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not read the bound workflow.", true
		return out
	}
	digest, err := chatWorkflowRevision(g)
	if err != nil {
		out.Text, out.Retryable = "Could not verify the workflow revision.", true
		return out
	}
	out.Review, err = chat.NewTaskCorrection(actor.chatOwner, id, d, plan.Correction, digest, time.Now())
	if err != nil {
		out.Text = "Open Tasks to review the current text, channel payload and bindings before editing."
		return out
	}
	out.Text, out.Destination = "Compare the task text before saving the correction.", ""
	return out
}

func (s *Server) applyChatTaskCorrection(ctx context.Context, actor Identity, id string, r *chat.OperationReview) (*chat.Session, error) {
	p := r.TaskCorrection
	manager := taskconfig.Manager{Store: s.Store}
	err := s.Engine.WithGraphTransition(ctx, p.Workflow, func(ctx context.Context) error {
		g, err := s.Store.GetGraph(ctx, p.Workflow, 0)
		if err != nil {
			return err
		}
		digest, err := chatWorkflowRevision(g)
		if err != nil {
			return err
		}
		if g.Metadata.Version != p.WorkflowVersion || digest != p.WorkflowDigest {
			return storage.ErrConflict
		}
		d, err := manager.Get(ctx, p.Task)
		if err != nil {
			return err
		}
		if d.Revision != p.Revision || d.GraphID != p.Workflow || d.Text() != p.Before {
			return storage.ErrConflict
		}
		_, err = manager.CorrectForOperation(ctx, d, p.After, actor.Name, r.ID)
		return err
	})
	state := chat.ReviewApplied
	version := int(p.Revision + 1)
	if err != nil {
		state, version = chat.ReviewUnknown, 0
		if errors.Is(err, storage.ErrConflict) || errors.Is(err, engine.ErrConflict) || errors.Is(err, taskconfig.ErrInvalid) || errors.Is(err, storage.ErrNotFound) {
			state = chat.ReviewConflict
		}
	}
	return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, state, version)
}

func (s *Server) observeChatTaskCorrection(ctx context.Context, owner string, session *chat.Session, r *chat.OperationReview) (*chat.Session, error) {
	d, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, r.TaskCorrection.Task)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if d != nil && d.CorrectionOperation == r.ID && d.CorrectionRevision == r.TaskCorrection.Revision+1 {
		return s.chatStore().FinishReview(ctx, owner, session.ID, r.ID, chat.ReviewApplied, int(d.CorrectionRevision))
	}
	if r.State == chat.ReviewApplying && time.Since(r.ClaimedAt) > 20*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, r.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}
