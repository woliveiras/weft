package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// The native review resolves repository and environment policy. The transcript
// carries only a bounded definition and immutable project/commit references.
func (s *Server) chatEnvironmentTarget(ctx context.Context, id string) (chat.EnvironmentPreparation, error) {
	var p chat.EnvironmentPreparation
	review, err := s.taskRunner().Review(ctx, id)
	if err != nil {
		return p, err
	}
	d := review.Task
	if !review.CanPrepareEnvironment || d.Status != "draft" || review.CurrentGraphVersion != d.GraphVersion || review.EnvironmentDefinition == nil || review.Environment == nil {
		return p, storage.ErrConflict
	}
	if err := (cost.Budgets{Store: s.Store}).CheckAdmission(ctx, cost.Target{Projects: domain.RepositoryProjectIDs(d.Repositories), User: d.CreatedBy, Project: d.ProjectID, Workflow: d.GraphID}); err != nil {
		return p, err
	}
	graph, err := s.Store.GetGraph(ctx, d.GraphID, 0)
	if err != nil {
		return p, err
	}
	// An unbound workspace may materialize a mutable remote ref. Keep that
	// case on the native review until a pinned repository has been selected.
	if graph.Spec.Workspace.Repo != "" && d.ProjectID == "" && len(d.Repositories) == 0 {
		return p, taskconfig.ErrInvalid
	}
	digest, err := chatWorkflowRevision(graph)
	if err != nil {
		return p, err
	}
	body, err := json.MarshalIndent(review.EnvironmentDefinition, "", "  ")
	if err != nil {
		return p, err
	}
	p = chat.EnvironmentPreparation{Task: id, Revision: d.Revision, Workflow: d.GraphID, Version: d.GraphVersion, WorkflowDigest: digest, DefinitionHash: review.Environment.CurrentHash, Definition: string(body)}
	if d.ProjectID != "" {
		p.Repositories = append(p.Repositories, d.ProjectID+" @ "+d.BaseCommit)
	}
	for _, repo := range d.Repositories {
		p.Repositories = append(p.Repositories, fmt.Sprintf("%s · %s @ %s", repo.Alias, repo.ProjectID, repo.BaseCommit))
	}
	return p, nil
}

func (s *Server) reviewChatEnvironment(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan, Destination: "tasks"}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must prepare a task environment."
		return out
	}
	task, err := s.resolveChatTask(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which task environment should I prepare? Enter the task ID or unique title.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not find the task.", true
		return out
	}
	p, err := s.chatEnvironmentTarget(ctx, task)
	switch {
	case errors.Is(err, cost.ErrBudgetReached):
		out.Text, out.Destination = "The current budget blocks preparation. Review the budget before retrying.", "budgets"
	case errors.Is(err, storage.ErrConflict) || errors.Is(err, taskconfig.ErrInvalid):
		out.Text = "Open Tasks to inspect the environment definition, pinned repositories and current readiness."
	case err != nil:
		out.Text, out.Retryable = "The environment review is temporarily unavailable.", true
	default:
		out.Review, err = chat.NewEnvironmentReview(actor.chatOwner, id, p, time.Now())
		if err != nil {
			out.Text = "Open Tasks to review this environment definition securely."
			return out
		}
		out.Text, out.Destination = "Review the environment definition before requesting its build.", ""
	}
	return out
}

func (s *Server) applyChatEnvironment(ctx context.Context, actor Identity, id string, r *chat.OperationReview) (*chat.Session, error) {
	p := r.Environment
	err := s.Engine.WithGraphTransition(ctx, p.Workflow, func(ctx context.Context) error {
		now, err := s.chatEnvironmentTarget(ctx, p.Task)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(now, *p) {
			return storage.ErrConflict
		}
		_, err = s.taskRunner().PrepareEnvironmentForOperation(ctx, p.Task, p.Revision, actor.Name, r.ID)
		return err
	})
	clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	d, readErr := (taskconfig.Manager{Store: s.Store}).Get(clean, p.Task)
	state := chat.ReviewUnknown
	if readErr == nil && d.EnvironmentOperation == r.ID {
		state = chat.ReviewQueued
		if d.LaunchError != "" || d.Status == "interrupted" {
			state = chat.ReviewIncomplete
		}
	} else if errors.Is(err, storage.ErrConflict) || errors.Is(err, engine.ErrConflict) || errors.Is(err, taskconfig.ErrInvalid) || errors.Is(err, cost.ErrBudgetReached) {
		state = chat.ReviewConflict
	}
	return s.chatStore().RecordEnvironmentState(clean, actor.chatOwner, id, r, state)
}

func (s *Server) observeChatEnvironment(ctx context.Context, owner string, session *chat.Session, r *chat.OperationReview) (*chat.Session, error) {
	p := r.Environment
	view, err := s.taskRunner().View(ctx, p.Task)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	next := chat.ReviewUnknown
	if err == nil && view.Task.EnvironmentOperation == r.ID {
		switch {
		case view.Task.LaunchError != "" || view.Task.Status == "interrupted" || view.Task.Status == "cancelled":
			next = chat.ReviewIncomplete
		case view.Environment != nil && view.Environment.Hash == p.DefinitionHash && view.Environment.UpToDate:
			next = chat.ReviewApplied
		case view.Environment != nil && view.Environment.Hash == p.DefinitionHash && view.Environment.State == environment.StateFailed:
			next = chat.ReviewIncomplete
		case view.Task.Status == "preparing" || view.Task.Status == "environment_build_requested" || view.Environment != nil && view.Environment.State == environment.StateBuilding:
			next = chat.ReviewQueued
		}
	} else if r.State == chat.ReviewApplying && time.Since(r.ClaimedAt) < 35*time.Second {
		return session, nil
	}
	return s.chatStore().RecordEnvironmentState(ctx, owner, session.ID, r, next)
}
