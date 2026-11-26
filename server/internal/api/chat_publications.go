package api

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) reviewChatCatalogPublication(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must publish a shared agent version."
		return out
	}
	draft := plan.Publication
	manager := agentcatalog.Manager{Store: s.Store}
	item, err := findChatCatalogAgent(ctx, manager, draft.Agent)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which reusable catalog agent should receive these shared instructions? Enter its ID or unique catalog name.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not inspect the catalog. Retry the saved request.", true
		return out
	}
	current := item.Versions[len(item.Versions)-1]
	if current.Definition.Prompt == draft.Instructions {
		out.Text = "These shared instructions are already in the latest catalog version."
		return out
	}
	publication := chat.CatalogPublication{
		ID: item.ID, Name: current.Definition.Name, PreviousVersion: current.Version, Revision: item.Revision,
		DefinitionDigest: installation.Digest(current.Definition), Before: current.Definition.Prompt, After: draft.Instructions,
	}
	out.Review, err = chat.NewCatalogPublicationReview(actor.chatOwner, sessionID, publication, time.Now())
	if err != nil {
		out.Text, out.Destination = "The complete shared instructions cannot be safely reviewed here. Open Agents to review them.", "agents"
		return out
	}
	next := current.Definition
	next.Prompt = draft.Instructions
	checked := next
	if err := s.validateAgentDefinition(ctx, &checked); err != nil || !reflect.DeepEqual(next, checked) {
		out.Text, out.Destination, out.Review = "These instructions do not validate with the saved catalog settings. Inspect the agent in Agents.", "agents", nil
		return out
	}
	out.Text = "Review these shared instructions before publishing a catalog version. Workflows can adopt it separately."
	return out
}

func (s *Server) applyChatCatalogPublication(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	p := review.Publication
	manager := agentcatalog.Manager{Store: s.Store}
	finish := func(state string, version int) (*chat.Session, error) {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, version)
	}
	agent, err := manager.Get(ctx, p.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return finish(chat.ReviewConflict, 0)
	}
	if err != nil {
		return finish(chat.ReviewUnknown, 0)
	}
	old := agent.Versions[len(agent.Versions)-1]
	if agent.Revision != p.Revision || old.Version != p.PreviousVersion || installation.Digest(old.Definition) != p.DefinitionDigest {
		return finish(chat.ReviewConflict, 0)
	}
	next := old.Definition
	next.Prompt = p.After
	checked := next
	if err := s.validateAgentDefinition(ctx, &checked); err != nil || !reflect.DeepEqual(next, checked) {
		return finish(chat.ReviewConflict, 0)
	}
	agent, err = manager.PublishForOperation(ctx, p.ID, p.Revision, checked, actor.Name, review.ID)
	if err == nil {
		s.auditAgentPublication(ctx, agent, actor.Name)
		return finish(chat.ReviewCreated, agent.Versions[len(agent.Versions)-1].Version)
	}
	readCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	if version, found, readErr := s.chatPublicationReceipt(readCtx, review); readErr == nil && found {
		return finish(chat.ReviewCreated, version)
	}
	if errors.Is(err, storage.ErrConflict) || errors.Is(err, agentcatalog.ErrInvalid) {
		return finish(chat.ReviewConflict, 0)
	}
	return finish(chat.ReviewUnknown, 0)
}

func (s *Server) chatPublicationReceipt(ctx context.Context, review *chat.OperationReview) (int, bool, error) {
	p := review.Publication
	agent, err := (agentcatalog.Manager{Store: s.Store}).Get(ctx, p.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	for _, version := range agent.Versions {
		if version.Version == p.PreviousVersion+1 && version.Operation == review.ID {
			return version.Version, true, nil
		}
	}
	return 0, false, nil
}
