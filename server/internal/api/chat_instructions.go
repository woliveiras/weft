package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
)

func (s *Server) reviewChatInstructions(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must change workflow instructions."
		return out
	}
	change := plan.Instructions
	g, rec, err := s.Engine.GetGraphDocument(ctx, change.Graph, 0)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			out.Plan.Kind, out.Text = chat.Clarify, "Which workflow and node should receive these local instructions? Supply their exact IDs."
		} else {
			out.Text, out.Retryable = "The workflow document is temporarily unavailable.", true
		}
		return out
	}
	n := g.Node(change.Node)
	if n == nil {
		out.Plan.Kind, out.Text = chat.Clarify, "Which node in the selected workflow should receive these instructions?"
		return out
	}
	change.Before, change.Version, change.SourceDigest = n.LocalInstructions, rec.Version, installation.Digest(g)
	if change.Before == change.After {
		out.Text = "These workflow-local instructions are already saved."
		return out
	}
	if n.Agent != nil {
		change.CatalogID, change.CatalogVersion = n.Agent.ID, n.Agent.Version
	}
	n.LocalInstructions = change.After
	if g.Metadata.Version != rec.Version || s.Engine.ResolveAgentBindings(ctx, g) != nil || !s.Engine.Validate(g).Valid {
		out.Text = "This change does not validate against the current workflow. Review the node and its prompt parameters in Workflows."
		return out
	}
	out.Review, err = chat.NewInstructionReview(actor.chatOwner, id, change, time.Now())
	if err != nil {
		out.Text = "The local instructions exceed the chat preview or contain sensitive fields. Review them in the workflow editor."
		return out
	}
	out.Text = "Review the change to this node’s workflow-local instructions."
	return out
}

func (s *Server) applyChatInstructions(ctx context.Context, actor Identity, id string, r *chat.OperationReview) (*chat.Session, error) {
	p := r.Instructions
	g, rec, err := s.Engine.GetGraphDocument(ctx, p.Graph, 0)
	finish := func(state string, version int) (*chat.Session, error) {
		return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, state, version)
	}
	if errors.Is(err, engine.ErrNotFound) {
		return finish(chat.ReviewConflict, 0)
	}
	if err != nil {
		return finish(chat.ReviewUnknown, 0)
	}
	if rec.Version != p.Version || g.Metadata.Version != p.Version || installation.Digest(g) != p.SourceDigest || g.Node(p.Node) == nil {
		return finish(chat.ReviewConflict, 0)
	}
	g.Node(p.Node).LocalInstructions = p.After
	if err = s.validateEnvironmentOrigin(ctx, g); err != nil {
		return finish(chat.ReviewConflict, 0)
	}
	saved, _, err := s.Engine.SaveGraph(ctx, g, p.Version, actor.Name)
	if err == nil {
		return finish(chat.ReviewApplied, saved.Metadata.Version)
	}
	if errors.Is(err, engine.ErrConflict) || errors.Is(err, engine.ErrInvalid) || errors.Is(err, mcpconfig.ErrInvalid) {
		return finish(chat.ReviewConflict, 0)
	}
	return finish(chat.ReviewUnknown, 0)
}
