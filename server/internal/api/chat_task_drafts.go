package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) reviewChatTaskDraft(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must create a task draft."
		return out
	}
	draft := plan.TaskDraft
	workflow, err := s.findChatWorkflow(ctx, draft.Workflow)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which saved workflow should this task use? Enter its exact ID or a unique workflow name.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not inspect saved workflows. Retry the saved request.", true
		return out
	}
	if !draft.MatchWorkflowVersion(workflow.Metadata.Version) {
		out.Text = fmt.Sprintf("This workflow is now at version %d. Which current workflow should the draft use?", workflow.Metadata.Version)
		out.Plan.Kind = chat.Clarify
		return out
	}
	request := draft.Input()
	refs, bindings, err := s.bindDraftRepositories(ctx, draft.Repositories)
	if err != nil {
		out.Text = "Choose each registered project by its exact ID or unique name and use its registered base commit."
		out.Plan.Kind = chat.Clarify
		if !errors.Is(err, storage.ErrNotFound) && !errors.Is(err, taskconfig.ErrInvalid) {
			out.Text = "Could not read registered projects. Retry the saved request."
			out.Retryable = true
		}
		return out
	}
	request.Repositories = refs
	input, err := taskconfig.BindWorkflowDraft(request, workflow)
	if err != nil {
		out.Text = "Choose the task's agent, input or repository settings in Tasks before saving."
		out.Destination = "tasks"
		return out
	}
	revision, err := chatWorkflowRevision(workflow)
	if err != nil {
		out.Text, out.Retryable = "Could not verify the workflow revision. Retry the saved request.", true
		return out
	}
	review, err := chat.NewTaskReview(actor.chatOwner, sessionID, input, revision, time.Now(), bindings...)
	if err != nil {
		out.Text, out.Retryable = "Could not prepare a reviewed task draft. Retry the saved request.", true
		return out
	}
	out.Text, out.Review = "Review the request and workflow version before saving the task draft.", review
	return out
}

func (s *Server) findChatWorkflow(ctx context.Context, selector string) (*domain.Graph, error) {
	if strings.HasPrefix(selector, domain.TaskRunGraphPrefix) {
		return nil, storage.ErrNotFound
	}
	graph, err := s.Store.GetGraph(ctx, selector, 0)
	if err == nil {
		if graph.Metadata.Labels[domain.LabelTask] != "" {
			return nil, storage.ErrNotFound
		}
		return graph, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	graphs, err := s.Store.ListGraphs(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range graphs {
		if item.Metadata.Labels[domain.LabelTask] != "" || strings.HasPrefix(item.Metadata.ID, domain.TaskRunGraphPrefix) ||
			!strings.EqualFold(item.Metadata.Name, selector) {
			continue
		}
		if graph != nil {
			return nil, storage.ErrNotFound
		}
		graph = item
	}
	if graph == nil {
		return nil, storage.ErrNotFound
	}
	return graph, nil
}

func chatWorkflowRevision(workflow *domain.Graph) (string, error) {
	body, err := json.Marshal(workflow)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Server) applyReviewedChatTaskDraft(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	input := review.TaskDraft.Input
	manager := taskconfig.Manager{Store: s.Store}
	var draft *taskconfig.Draft
	err := s.Engine.WithGraphTransition(ctx, input.GraphID, func(ctx context.Context) error {
		current, err := s.Store.GetGraph(ctx, input.GraphID, 0)
		if err != nil {
			return err
		}
		revision, err := chatWorkflowRevision(current)
		if err != nil {
			return err
		}
		if current.Metadata.Version != input.GraphVersion || revision != review.TaskDraft.WorkflowRevision {
			return storage.ErrConflict
		}
		var guards []storage.SettingRevision
		for _, repo := range review.TaskDraft.Repositories {
			guards = append(guards, projectconfig.RevisionGuard(repo.Project, repo.Revision))
		}
		draft, err = manager.CreateForOperation(ctx, input, actor.Name, review.ID, guards...)
		return err
	})
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err == nil {
		return s.chatStore().FinishTaskReview(saveCtx, actor.chatOwner, sessionID, review.ID, draft.ID)
	}
	if saved, readErr := manager.CreationForOperation(saveCtx, review.ID); readErr == nil {
		return s.chatStore().FinishTaskReview(saveCtx, actor.chatOwner, sessionID, review.ID, saved.ID)
	}
	state := chat.ReviewUnknown
	if errors.Is(err, engine.ErrConflict) || errors.Is(err, storage.ErrConflict) || errors.Is(err, storage.ErrNotFound) || errors.Is(err, taskconfig.ErrInvalid) {
		state = chat.ReviewConflict
	}
	return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) reconcileChatTaskDraft(ctx context.Context, owner string, session *chat.Session, review *chat.OperationReview) (*chat.Session, error) {
	draft, err := (taskconfig.Manager{Store: s.Store}).CreationForOperation(ctx, review.ID)
	if err == nil {
		return s.chatStore().FinishTaskReview(ctx, owner, session.ID, review.ID, draft.ID)
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if review.State == chat.ReviewApplying && time.Since(review.ClaimedAt) > 20*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, review.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}
