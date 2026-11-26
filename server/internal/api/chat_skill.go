package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) reviewChatSkill(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan, Destination: "skills"}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must save a managed skill."
		return out
	}
	d := plan.Skill
	old, err := (instructions.Manager{Store: s.Store}).Get(ctx, d.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Retryable = "The skill catalogue is temporarily unavailable.", true
		return out
	}
	if old == nil && d.Action != "create" {
		out.Plan.Kind, out.Text = chat.Clarify, "Which existing managed skill should I use? Enter its name or ID."
		return out
	}
	if old != nil && d.Action == "create" {
		out.Plan.Kind, out.Text = chat.Clarify, "That skill ID already exists. Choose a new ID or request a revision."
		return out
	}
	out.Review, err = chat.NewSkillReview(actor.chatOwner, id, d, old, time.Now())
	if err != nil {
		out.Text = "Review this skill in Skills and instructions. Its content or state is outside the Chat preview."
		return out
	}
	if out.Review.Skill.Delete {
		if err := s.checkSkillDeletion(ctx, old.Input); err != nil {
			out.Review = nil
			out.Text = "This skill is referenced by a workflow. Remove its association before deleting it."
			if !errors.Is(err, storage.ErrConflict) {
				out.Text, out.Retryable = "Could not verify the workflows using this skill.", true
			}
			return out
		}
	} else if d.Action != "revise" && old != nil && old.Input == out.Review.Skill.After {
		out.Review, out.Text = nil, "The saved skill already matches the request."
		return out
	}
	out.Destination = ""
	out.Text = "Review the managed skill text and the workflows it can affect."
	return out
}

func (s *Server) applyChatSkill(ctx context.Context, actor Identity, id string, r *chat.OperationReview) (*chat.Session, error) {
	p := r.Skill
	m := instructions.Manager{Store: s.Store}
	if p.Delete {
		if err := s.checkSkillDeletion(ctx, p.After); err != nil {
			if errors.Is(err, storage.ErrConflict) {
				return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, chat.ReviewConflict, 0)
			}
			return nil, err
		}
	}
	saved, err := m.SaveForOperation(ctx, p.After, p.Revision, actor.Name, r.ID, p.Delete)
	if err == nil {
		s.auditSavedSkill(ctx, saved)
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, chat.ReviewApplied, int(saved.Revision))
	}
	if errors.Is(err, storage.ErrConflict) || errors.Is(err, instructions.ErrBuiltinReadOnly) {
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, chat.ReviewConflict, 0)
	}
	return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, chat.ReviewUnknown, 0)
}

func (s *Server) observeChatSkill(ctx context.Context, owner string, session *chat.Session, r *chat.OperationReview) (*chat.Session, error) {
	skill, err := (instructions.Manager{Store: s.Store}).Get(ctx, r.Skill.After.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if skill != nil {
		for _, v := range append(skill.History, skill.Version) {
			if v.Operation == r.ID && v.Revision == r.Skill.Revision+1 && v.Input == r.Skill.After && v.Deleted == r.Skill.Delete {
				return s.chatStore().FinishReview(ctx, owner, session.ID, r.ID, chat.ReviewApplied, int(v.Revision))
			}
		}
	}
	if r.State == chat.ReviewApplying && time.Since(r.ClaimedAt) > 20*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, r.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}
