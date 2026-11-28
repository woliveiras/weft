package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) reviewChatTaskAuthorization(ctx context.Context, actor Identity, sessionID string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleAdmin) {
		out.Text = "An administrator must record task authorization."
		return out
	}
	id, err := s.resolveChatTask(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which task should this authorization apply to? Enter its ID or exact title.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not find the task. Retry the saved request.", true
		return out
	}
	draft, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, id)
	if err != nil {
		out.Text, out.Retryable = "Could not read the saved task revision. Retry the saved request.", true
		return out
	}
	native, err := (taskauth.Manager{Store: s.Store, Tools: s.Tools}).Review(ctx, id)
	if err != nil {
		out.Text, out.Destination = "Open Settings & access in Tasks to repair or review the current scope.", "tasks"
		return out
	}
	mode := plan.TaskAuthorizationMode
	if native.WorkflowProfile != "" && !mode.IsProfile() {
		out.Text = "Choose Manual, Assisted or Automatic for this workflow."
		return out
	}
	running := draft.Status == "running" || draft.Status == "starting"
	if draft.Status != "draft" && draft.Status != "environment_pending" && !running {
		out.Text, out.Destination = "This task cannot change its current authorization. Inspect the task state in Tasks.", "tasks"
		return out
	}
	if running && (native.Record == nil || native.Record.Scope != native.Scope || !taskauth.Reduction(native.Record.Mode, mode)) {
		out.Text, out.Destination = "A running task can only narrow its current authorization. Inspect Settings & access in Tasks.", "tasks"
		return out
	}
	if err := taskauth.ValidateMode(mode, native.Agents, s.Tools); err != nil {
		out.Text, out.Destination = "This mode is unavailable for the workflow's isolation or grants. Inspect Settings & access in Tasks.", "tasks"
		return out
	}
	title := draft.Title
	if chat.ValidateMessage(title) != nil {
		title = draft.ID
	}
	auth := chat.TaskAuthorization{
		ID: draft.ID, Title: title, TaskRevision: draft.Revision, WorkflowID: draft.GraphID, WorkflowVersion: draft.GraphVersion,
		ExecutionMode: draft.ExecutionMode, Current: native.StartConsent(), Mode: mode,
		Agents: append([]taskauth.AgentScope{}, native.Capabilities...),
	}
	for _, repo := range draft.Repositories {
		auth.Repositories = append(auth.Repositories, chat.AuthorizedRepository{
			Alias: repo.Alias, ProjectID: repo.ProjectID, BaseCommit: repo.BaseCommit, SCMConnection: repo.SCMConnection,
			Repository: repo.Repository, TargetBranch: repo.TargetBranch,
		})
	}
	if draft.ProjectID != "" {
		auth.Repositories = append(auth.Repositories, chat.AuthorizedRepository{
			ProjectID: draft.ProjectID, BaseCommit: draft.BaseCommit, SCMConnection: draft.SCMConnection,
		})
	}
	out.Review, err = chat.NewTaskAuthorizationReview(actor.chatOwner, sessionID, auth, time.Now())
	if err != nil {
		out.Text, out.Destination = "The full scope cannot be safely reviewed here. Open Settings & access in Tasks.", "tasks"
		return out
	}
	out.Text = "Review this task's capabilities and proposed authorization mode before recording consent."
	return out
}

func (s *Server) applyChatTaskAuthorization(ctx context.Context, actor Identity, sessionID string, review *chat.OperationReview) (*chat.Session, error) {
	auth := review.TaskAuthorization
	manager := taskauth.Manager{Store: s.Store, Tools: s.Tools}
	var saved *taskauth.Record
	err := s.Engine.WithGraphTransition(ctx, auth.WorkflowID, func(ctx context.Context) error {
		var err error
		saved, err = manager.SaveReviewed(ctx, auth.ID, auth.Mode, auth.Current.Scope, auth.Current.Revision, auth.TaskRevision, actor.Name, review.ID)
		return err
	})
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err == nil {
		return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, chat.ReviewApplied, int(saved.Revision))
	}
	if version, found, readErr := s.chatTaskAuthorizationReceipt(saveCtx, review); readErr == nil && found {
		return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, chat.ReviewApplied, version)
	}
	state := chat.ReviewUnknown
	if errors.Is(err, taskauth.ErrInvalid) || errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) || errors.Is(err, engine.ErrConflict) {
		state = chat.ReviewConflict
	}
	return s.chatStore().FinishReview(saveCtx, actor.chatOwner, sessionID, review.ID, state, 0)
}

func (s *Server) chatTaskAuthorizationReceipt(ctx context.Context, review *chat.OperationReview) (int, bool, error) {
	auth := review.TaskAuthorization
	native, err := (taskauth.Manager{Store: s.Store}).Review(ctx, auth.ID)
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if native.Record == nil || native.Record.Operation != review.ID || native.Record.Mode != auth.Mode {
		return 0, false, nil
	}
	return int(auth.Current.Revision + 1), true, nil
}
