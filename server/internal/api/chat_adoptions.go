package api

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/woliveiras/weft/server/internal/agentcatalog"
	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) reviewChatAdoption(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must change a workflow's catalog pin."
		return out
	}
	request := plan.Adoption
	source, err := s.findChatWorkflow(ctx, request.Workflow)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which workflow should adopt the version? Enter its ID or unique name.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not read the workflow. Retry the saved request.", true
		return out
	}
	node := source.Node(resolveChatControlNode(source, request.Node))
	if node == nil || node.Agent == nil {
		out.Text, out.Plan.Kind = "Which node with a pinned catalog agent should adopt this version?", chat.Clarify
		return out
	}
	catalog := agentcatalog.Manager{Store: s.Store}
	agent, err := catalog.Get(ctx, node.Agent.ID)
	if err != nil {
		out.Text, out.Destination = "Inspect the node's catalog binding in Workflows before changing the pin.", "workflows"
		return out
	}
	target, ok := selectChatAgentVersion(agent, request.Version)
	current, available := catalogVersion(agent, node.Agent.Version)
	if !ok || !available {
		out.Text, out.Plan.Kind = "Enter a saved version number for the node's catalog agent.", chat.Clarify
		return out
	}
	if target.Version == current.Version {
		out.Text = "This node already uses that catalog version."
		return out
	}
	if !sameCatalogSettings(current.Definition, target.Definition) {
		out.Text, out.Destination = "These versions also differ in runtime, grants or other shared settings. Review their full effect in Workflows.", "workflows"
		return out
	}
	review, err := chat.NewAgentAdoptionReview(actor.chatOwner, sessionID, chat.AgentAdoption{
		WorkflowID: source.Metadata.ID, NodeID: node.ID, WorkflowVersion: source.Metadata.Version,
		WorkflowCreated: source.Metadata.CreatedAt, WorkflowDigest: installation.Digest(source),
		AgentID: node.Agent.ID, CurrentVersion: current.Version, TargetVersion: target.Version,
		AgentDigest: installation.Digest(target.Definition), Before: current.Definition.Prompt, After: target.Definition.Prompt,
	}, time.Now())
	if err != nil {
		out.Text, out.Destination = "The full instructions cannot be safely reviewed here. Compare the catalog versions in Workflows.", "workflows"
		return out
	}
	node.Agent.Version = target.Version
	if err := s.Engine.ResolveAgentBindings(ctx, source); err != nil || !s.Engine.Validate(source).Valid {
		out.Text, out.Destination = "The new catalog version needs additional workflow configuration. Inspect the binding in Workflows.", "workflows"
		return out
	}
	out.Text, out.Review = "Review the version change for this workflow node.", review
	return out
}

func catalogVersion(agent agentcatalog.Agent, version int) (agentcatalog.Version, bool) {
	if version < 1 || version > len(agent.Versions) || agent.Versions[version-1].Version != version {
		return agentcatalog.Version{}, false
	}
	return agent.Versions[version-1], true
}

func sameCatalogSettings(a, b domain.AgentDefinition) bool {
	a.Prompt, b.Prompt = "", ""
	return reflect.DeepEqual(a, b)
}

func (s *Server) applyChatAdoption(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	p := review.Adoption
	finish := func(state string, version int) (*chat.Session, error) {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, version)
	}
	workflow, record, err := s.Engine.GetGraphDocument(ctx, p.WorkflowID, 0)
	if errors.Is(err, engine.ErrNotFound) {
		return finish(chat.ReviewConflict, 0)
	}
	if err != nil {
		return finish(chat.ReviewUnknown, 0)
	}
	node := workflow.Node(p.NodeID)
	if workflow.Metadata.Version != p.WorkflowVersion || record.Version != p.WorkflowVersion ||
		!workflow.Metadata.CreatedAt.Equal(p.WorkflowCreated) || installation.Digest(workflow) != p.WorkflowDigest ||
		node == nil || node.Agent == nil || node.Agent.ID != p.AgentID || node.Agent.Version != p.CurrentVersion {
		return finish(chat.ReviewConflict, 0)
	}
	a, err := (agentcatalog.Manager{Store: s.Store}).Get(ctx, p.AgentID)
	if errors.Is(err, storage.ErrNotFound) {
		return finish(chat.ReviewConflict, 0)
	}
	if err != nil {
		return finish(chat.ReviewUnknown, 0)
	}
	newVersion, ok := catalogVersion(a, p.TargetVersion)
	oldVersion, available := catalogVersion(a, p.CurrentVersion)
	if !ok || !available || installation.Digest(newVersion.Definition) != p.AgentDigest ||
		!sameCatalogSettings(oldVersion.Definition, newVersion.Definition) {
		return finish(chat.ReviewConflict, 0)
	}
	node.Agent.Version = p.TargetVersion
	if err := s.validateEnvironmentOrigin(ctx, workflow); err != nil {
		return finish(chat.ReviewConflict, 0)
	}
	g, _, err := s.Engine.SaveReviewedGraph(ctx, workflow, p.WorkflowVersion, actor.Name,
		engine.GraphSaveReview{CreatedAt: p.WorkflowCreated, Operation: review.ID})
	if err == nil {
		return finish(chat.ReviewCreated, g.Metadata.Version)
	}
	readCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	if version, found, readErr := s.chatAdoptionReceipt(readCtx, review); readErr == nil && found {
		return finish(chat.ReviewCreated, version)
	}
	var invalid *engine.ValidationError
	if errors.Is(err, engine.ErrConflict) || errors.Is(err, mcpconfig.ErrInvalid) || errors.As(err, &invalid) {
		return finish(chat.ReviewConflict, 0)
	}
	return finish(chat.ReviewUnknown, 0)
}

func (s *Server) chatAdoptionReceipt(ctx context.Context, review *chat.OperationReview) (int, bool, error) {
	p := review.Adoption
	graph, record, err := s.Engine.GetGraphDocument(ctx, p.WorkflowID, 0)
	if errors.Is(err, engine.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	node := graph.Node(p.NodeID)
	found := record.ChangeOperation == review.ID && graph.Metadata.CreatedAt.Equal(p.WorkflowCreated) &&
		record.Version == graph.Metadata.Version && record.Version == p.WorkflowVersion+1 &&
		node != nil && node.Agent != nil && node.Agent.ID == p.AgentID && node.Agent.Version == p.TargetVersion
	return record.Version, found, nil
}
