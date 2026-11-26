package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) reviewChatWorkflow(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must create workflows."
		return out
	}
	draft := plan.Workflow
	if _, err := s.Store.GetGraph(ctx, draft.ID, 0); !errors.Is(err, storage.ErrNotFound) {
		if err == nil {
			out.Text, out.Plan.Kind = "That workflow ID is already in use. What should the new workflow be called?", chat.Clarify
		} else {
			out.Text, out.Retryable = "Could not inspect workflows. Retry the saved request.", true
		}
		return out
	}

	agent, err := findChatCatalogAgent(ctx, agentcatalog.Manager{Store: s.Store}, draft.Agent)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which catalog agent should the workflow use? Enter its exact agent ID or a unique catalog name.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not inspect the catalog. Retry the saved request.", true
		return out
	}
	selected, ok := selectChatAgentVersion(agent, draft.AgentVersion)
	if !ok {
		latest := agent.Versions[len(agent.Versions)-1].Version
		out.Text = fmt.Sprintf("Which version of catalog agent %s should the workflow use? The latest saved version is %d.", agent.ID, latest)
		out.Plan.Kind = chat.Clarify
		return out
	}
	binding := domain.AgentBinding{ID: agent.ID, Version: selected.Version}
	candidate := draft.Graph(binding)
	err = s.Engine.ResolveAgentBindings(ctx, &candidate)
	var invalid *engine.ValidationError
	if errors.As(err, &invalid) {
		out.Text = "This catalog version needs additional configuration. Choose another version or configure its parameters in Workflows."
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not resolve the catalog version. Retry the saved request.", true
		return out
	}
	if !s.Engine.Validate(&candidate).Valid {
		out.Text = "The proposed workflow failed validation. Inspect the catalog agent and its runtime in the console before trying again."
		return out
	}
	if s.MCP != nil {
		if err := s.MCP.ValidateGrants(ctx, &candidate); err != nil {
			out.Text = "Could not validate the workflow's connection grants. Inspect Connections or retry the saved request."
			out.Retryable = !errors.Is(err, mcpconfig.ErrInvalid)
			return out
		}
	}

	definition := selected.Definition
	creation := chat.WorkflowCreation{
		Graph: draft.Graph(binding), AgentName: definition.Name,
		Runtime: string(candidate.Spec.Nodes[0].Runtime.Kind), ReadOnly: definition.ReadOnly,
	}
	for _, grant := range definition.Tools {
		creation.ToolNames = append(creation.ToolNames, grant.Name)
	}
	review, err := chat.NewWorkflowReview(actor.chatOwner, sessionID, creation, time.Now())
	if err != nil {
		out.Text, out.Retryable = "Could not prepare a reviewed workflow. Retry the saved request.", true
		return out
	}
	out.Text, out.Review = "Review the workflow and its pinned catalog agent before creating it.", review
	return out
}

func findChatCatalogAgent(ctx context.Context, catalog agentcatalog.Manager, selector string) (agentcatalog.Agent, error) {
	agent, err := catalog.Get(ctx, selector)
	if !errors.Is(err, storage.ErrNotFound) {
		return agent, err
	}
	items, err := catalog.List(ctx)
	if err != nil {
		return agentcatalog.Agent{}, err
	}
	var id string
	for _, item := range items {
		if strings.EqualFold(item.Name, selector) {
			if id != "" {
				return agentcatalog.Agent{}, storage.ErrNotFound
			}
			id = item.ID
		}
	}
	if id == "" {
		return agentcatalog.Agent{}, storage.ErrNotFound
	}
	return catalog.Get(ctx, id)
}

func selectChatAgentVersion(agent agentcatalog.Agent, selector string) (agentcatalog.Version, bool) {
	if selector == "latest" || (selector == "" && len(agent.Versions) == 1) {
		return agent.Versions[len(agent.Versions)-1], true
	}
	version, err := strconv.Atoi(selector)
	if err != nil {
		return agentcatalog.Version{}, false
	}
	return catalogVersion(agent, version)
}

func (s *Server) applyReviewedChatWorkflow(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	graph := review.Workflow.Graph
	saved, _, err := s.Engine.CreateGraph(ctx, &graph, actor.Name)
	state, version := chat.ReviewUnknown, 0
	var invalid *engine.ValidationError
	switch {
	case err == nil:
		state, version = chat.ReviewCreated, saved.Metadata.Version
	case errors.Is(err, engine.ErrConflict), errors.As(err, &invalid), errors.Is(err, mcpconfig.ErrInvalid):
		state = chat.ReviewConflict
	}
	// Persist the outcome with a fresh deadline, even if the engine operation
	// used its deadline. A lost outcome cannot be inferred from graph existence.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, version)
}
