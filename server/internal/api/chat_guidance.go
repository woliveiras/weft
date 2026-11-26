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
)

func (s *Server) reviewChatGuidance(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must send guidance to a workflow."
		return out
	}
	g := plan.Guidance
	replica, err := g.ReplicaIndex()
	if err != nil {
		out.Plan.Kind = chat.Clarify
		out.Text = "Which replica should receive this message? Choose a replica number or explicitly select any replica."
		return out
	}
	rec, err := s.Engine.GuidanceTarget(ctx, g.Graph, g.Node, replica)
	if err != nil {
		out.Text = "Select a running workflow, an enabled node and its replica. Use Tasks for a task-owned execution."
		if !errors.Is(err, engine.ErrNotFound) && !errors.Is(err, engine.ErrConflict) && !errors.Is(err, engine.ErrInvalid) {
			out.Text = "Could not read the running workflow. Retry the saved request."
			out.Retryable = true
		}
		return out
	}
	g.Run, g.Version = rec.RunID, rec.Version
	out.Review, err = chat.NewGuidanceReview(actor.chatOwner, id, g, time.Now())
	if err != nil {
		out.Text = "This guidance proposal is invalid."
		return out
	}
	out.Text = "Review the one-time operator message for the selected run."
	return out
}

func (s *Server) applyChatGuidance(ctx context.Context, actor Identity, id string, r *chat.OperationReview) (*chat.Session, error) {
	g := r.Guidance
	rep, err := g.ReplicaIndex()
	if err != nil {
		return nil, err
	}
	m := &domain.OperatorMessage{ID: r.ID, GraphID: g.Graph, RunID: g.Run, NodeID: g.Node, Replica: rep, Text: g.Text, Author: actor.Name, CreatedAt: time.Now().UTC()}
	if err := s.Engine.SendReviewedGuidance(ctx, m, g.Version); err != nil {
		state := chat.ReviewUnknown
		if errors.Is(err, engine.ErrInvalid) || errors.Is(err, engine.ErrConflict) || errors.Is(err, engine.ErrNotFound) {
			state = chat.ReviewConflict
		}
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, state, 0)
	}
	return s.chatStore().ObserveGuidance(ctx, actor.chatOwner, id, r.ID, r.State, chat.ReviewQueued, nil)
}

func (s *Server) reconcileChatGuidance(ctx context.Context, owner string, session *chat.Session, r *chat.OperationReview) (*chat.Session, error) {
	g := r.Guidance
	m, err := s.Store.GetOperatorMessage(ctx, r.ID)
	if errors.Is(err, storage.ErrNotFound) && r.State == chat.ReviewApplying && time.Since(r.ClaimedAt) < 20*time.Second {
		return session, nil
	}
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		return s.chatStore().ObserveGuidance(ctx, owner, session.ID, r.ID, r.State, chat.ReviewUnknown, nil)
	}
	replica, err := g.ReplicaIndex()
	if err != nil || m.GraphID != g.Graph || m.RunID != g.Run || m.NodeID != g.Node || m.Text != g.Text || m.Replica != replica {
		return s.chatStore().ObserveGuidance(ctx, owner, session.ID, r.ID, r.State, chat.ReviewUnknown, nil)
	}
	state := chat.ReviewQueued
	if m.Consumed != nil {
		state = chat.ReviewConsumed
	} else {
		_, rec, err := s.Engine.GetGraphDocument(ctx, g.Graph, 0)
		if err != nil && !errors.Is(err, engine.ErrNotFound) {
			return nil, err
		}
		if rec == nil || rec.RunID != g.Run || rec.DesiredState == domain.GraphStopped {
			state = chat.ReviewRunEnded
		}
	}
	return s.chatStore().ObserveGuidance(ctx, owner, session.ID, r.ID, r.State, state, m.Consumed)
}
