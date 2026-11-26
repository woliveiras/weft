package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/storage"
)

func workflowEnvironmentScope(g *domain.Graph) (chat.WorkflowEnvironment, error) {
	env := g.Spec.Environment
	workspace := g.Spec.Workspace
	if env == nil || env.Origin != nil || env.Context != "" || workspace.Repo != "" ||
		len(workspace.Repositories) != 0 || workspace.Path != "" && len(env.Setup) != 0 ||
		strings.HasPrefix(g.Metadata.ID, domain.TaskRunGraphPrefix) || g.Metadata.Labels[domain.LabelTask] != "" {
		return chat.WorkflowEnvironment{}, environment.ErrInvalidDefinition
	}
	digest, err := chatWorkflowRevision(g)
	if err != nil {
		return chat.WorkflowEnvironment{}, err
	}
	definition, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return chat.WorkflowEnvironment{}, err
	}
	return chat.WorkflowEnvironment{Workflow: g.Metadata.ID, Version: g.Metadata.Version, WorkflowDigest: digest,
		DefinitionHash: environment.Hash(env), Definition: string(definition)}, nil
}

func (s *Server) reviewChatWorkflowEnvironment(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan, Destination: "workflows"}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must request a workflow environment build."
		return out
	}
	workflow, err := s.findChatWorkflow(ctx, plan.EnvironmentWorkflow)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which reusable workflow should I prepare? Enter its ID or a unique name.", chat.Clarify
		return out
	}
	if err != nil || s.Environments == nil {
		out.Text, out.Retryable = "Could not read the workflow or its environment service.", true
		return out
	}
	g, record, err := s.Engine.GetGraph(ctx, workflow.Metadata.ID, 0)
	if err != nil || record.Version != g.Metadata.Version {
		out.Text, out.Retryable = "Could not verify the saved workflow version.", true
		return out
	}
	scope, err := workflowEnvironmentScope(g)
	if err != nil {
		out.Text = "Use the workflow's Environment settings to review a repository-backed definition or configure an environment."
		return out
	}
	if err := s.Environments.ValidatePolicy(ctx, g.Spec.Environment); err != nil {
		out.Text = "The installed environment policy blocks this definition. Examine the workflow's Environment settings."
		return out
	}
	out.Review, err = chat.NewWorkflowEnvironmentReview(actor.chatOwner, id, scope, time.Now())
	if err != nil {
		out.Text = "Use the workflow's Environment settings to review this definition securely."
		return out
	}
	out.Text, out.Destination = "Review the saved workflow environment before requesting its build.", ""
	return out
}

func (s *Server) applyChatWorkflowEnvironment(ctx context.Context, actor Identity, id string, review *chat.OperationReview) (*chat.Session, error) {
	expected := review.WorkflowEnvironment
	var result *environment.View
	err := s.Engine.WithGraphTransition(ctx, expected.Workflow, func(ctx context.Context) error {
		if s.Environments == nil {
			return engine.ErrNotFound
		}
		g, record, err := s.Engine.GetGraph(ctx, expected.Workflow, 0)
		if err != nil {
			return err
		}
		scope, err := workflowEnvironmentScope(g)
		if err != nil || scope != *expected || record.Version != scope.Version {
			return storage.ErrConflict
		}
		result, err = s.Environments.BuildReviewedSaved(ctx, g, actor.Name, review.ID)
		return err
	})
	store := s.chatStore()
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	state := chat.ReviewUnknown
	switch {
	case err == nil && result != nil && result.UpToDate && result.Hash == expected.DefinitionHash:
		state = chat.ReviewApplied
	case err == nil && result != nil && result.State == environment.StateBuilding && result.BuildOperation == review.ID:
		state = chat.ReviewQueued
	case errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) || errors.Is(err, engine.ErrConflict) ||
		errors.Is(err, engine.ErrNotFound) || errors.Is(err, environment.ErrInvalidDefinition) || errors.Is(err, environment.ErrBuildRunning) ||
		errors.Is(err, environment.ErrDeferredDeletion):
		state = chat.ReviewConflict
	}
	return store.RecordEnvironmentState(save, actor.chatOwner, id, review, state)
}

func (s *Server) observeChatWorkflowEnvironment(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	expected := review.WorkflowEnvironment
	state := chat.ReviewUnknown
	if s.Environments != nil {
		g, record, err := s.Engine.GetGraph(ctx, expected.Workflow, expected.Version)
		if err != nil && !errors.Is(err, engine.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			state, err = s.observedWorkflowEnvironmentState(ctx, g, record, review)
			if err != nil {
				return nil, err
			}
		}
	}
	if state == chat.ReviewUnknown && review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) < 35*time.Second {
		return session, nil
	}
	return s.chatStore().RecordEnvironmentState(ctx, owner, session.ID, review, state)
}

func (s *Server) observedWorkflowEnvironmentState(ctx context.Context, graph *domain.Graph, record *domain.GraphRecord, review *chat.OperationReview) (string, error) {
	expected := review.WorkflowEnvironment
	scope, err := workflowEnvironmentScope(graph)
	if err != nil || scope != *expected {
		return chat.ReviewUnknown, nil
	}
	view, err := s.Environments.Get(ctx, graph)
	if err != nil {
		return "", err
	}
	if view.BuildOperation != review.ID || view.Hash != expected.DefinitionHash {
		return chat.ReviewUnknown, nil
	}
	switch {
	case view.State == environment.StateBuilding:
		return chat.ReviewQueued, nil
	case view.UpToDate && record.Version == expected.Version:
		return chat.ReviewApplied, nil
	default:
		return chat.ReviewIncomplete, nil
	}
}
