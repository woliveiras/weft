package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) reviewChatTaskCorrection(ctx context.Context, actor Identity, id string, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan, Destination: "tasks"}
	if !actor.Role.Allows(config.RoleOperator) {
		out.Text = "An operator must edit a task draft."
		return out
	}
	task, err := s.resolveChatTask(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text, out.Plan.Kind = "Which draft should I edit? Enter its ID or unique title.", chat.Clarify
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not look up the task.", true
		return out
	}
	d, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, task)
	if err != nil {
		out.Text, out.Retryable = "Could not read the draft.", true
		return out
	}
	if d.Status != "draft" || d.GraphID == "" {
		out.Text = "Open Tasks to resolve the workflow or inspect the task’s current state."
		return out
	}
	g, err := s.Store.GetGraph(ctx, d.GraphID, 0)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && g.Metadata.Version != d.GraphVersion) {
		out.Text = "Review the workflow binding in Tasks before editing this draft."
		return out
	}
	if err != nil {
		out.Text, out.Retryable = "Could not read the bound workflow.", true
		return out
	}
	digest, err := chatWorkflowRevision(g)
	if err != nil {
		out.Text, out.Retryable = "Could not verify the workflow revision.", true
		return out
	}
	if plan.CheckoutAlias != "" {
		return s.reviewChatCheckout(ctx, actor, id, plan, d, digest)
	}
	if plan.CorrectionPayload != "" {
		out.Review, err = chat.NewTaskPayloadCorrection(actor.chatOwner, id, d, plan.CorrectionPayload, digest, time.Now())
		if err == nil {
			out.Text, out.Destination = "Compare the JSON for this channel and message type before saving the replacement.", ""
		} else {
			out.Text = "The input payload could not be reviewed in Chat. Open Tasks to check the saved channel and current payload."
		}
		return out
	}
	out.Review, err = chat.NewTaskCorrection(actor.chatOwner, id, d, plan.Correction, digest, time.Now())
	if err != nil {
		out.Text = "Open Tasks to review the current text, channel payload and bindings before editing."
		return out
	}
	out.Text, out.Destination = "Compare the task text before saving the correction.", ""
	return out
}

func (s *Server) applyChatTaskCorrection(ctx context.Context, actor Identity, id string, r *chat.OperationReview) (*chat.Session, error) {
	p := r.TaskCorrection
	manager := taskconfig.Manager{Store: s.Store}
	err := s.Engine.WithGraphTransition(ctx, p.Workflow, func(ctx context.Context) error {
		g, err := s.Store.GetGraph(ctx, p.Workflow, 0)
		if err != nil {
			return err
		}
		digest, err := chatWorkflowRevision(g)
		if err != nil {
			return err
		}
		if g.Metadata.Version != p.WorkflowVersion || digest != p.WorkflowDigest {
			return storage.ErrConflict
		}
		d, err := manager.Get(ctx, p.Task)
		if err != nil {
			return err
		}
		if d.Revision != p.Revision || d.GraphID != p.Workflow {
			return storage.ErrConflict
		}
		if change := p.Repository; change != nil {
			guards := []storage.SettingRevision{projectconfig.RevisionGuard(change.After.ProjectID, change.ProjectRevision)}
			if change.After.SCMConnection != "" {
				project, err := (projectconfig.Manager{Store: s.Store}).Get(ctx, change.After.ProjectID)
				if err != nil {
					return err
				}
				conn, guard, err := s.checkoutDelivery(ctx, project, change.After.SCMConnection, change.After.TargetBranch, d.ID)
				if err != nil {
					return err
				}
				if conn.Repository != change.After.Repository || conn.UpdatedAt.UTC().Format(time.RFC3339Nano) != change.After.ConnectionRevision {
					return storage.ErrConflict
				}
				guards = append(guards, guard)
			}
			_, err = manager.ReplaceRepositoryForOperation(ctx, d.ID, d.Revision, change.Before, change.After, actor.Name, r.ID, guards...)
			return err
		}
		if q := p.Payload; q != nil {
			if d.ChannelID != q.Channel || d.MessageType != q.MessageType || d.PayloadJSON != q.Before {
				return storage.ErrConflict
			}
			_, err = manager.ReplacePayloadForOperation(ctx, d.ID, d.Revision, q.After, actor.Name, r.ID)
			return err
		}
		if p.Before == nil || p.After == nil || d.Text() != *p.Before {
			return storage.ErrConflict
		}
		_, err = manager.CorrectForOperation(ctx, d, *p.After, actor.Name, r.ID)
		return err
	})
	state := chat.ReviewApplied
	version := int(p.Revision + 1)
	if err != nil {
		state, version = chat.ReviewUnknown, 0
		if errors.Is(err, storage.ErrConflict) || errors.Is(err, engine.ErrConflict) || errors.Is(err, taskconfig.ErrInvalid) || errors.Is(err, storage.ErrNotFound) {
			state = chat.ReviewConflict
		}
	}
	return s.chatStore().FinishReview(ctx, actor.chatOwner, id, r.ID, state, version)
}

func (s *Server) observeChatTaskCorrection(ctx context.Context, owner string, session *chat.Session, r *chat.OperationReview) (*chat.Session, error) {
	d, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, r.TaskCorrection.Task)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if d != nil && d.CorrectionOperation == r.ID && d.CorrectionRevision == r.TaskCorrection.Revision+1 {
		return s.chatStore().FinishReview(ctx, owner, session.ID, r.ID, chat.ReviewApplied, int(d.CorrectionRevision))
	}
	if r.State == chat.ReviewApplying && time.Since(r.ClaimedAt) > 20*time.Second {
		return s.chatStore().FinishReview(ctx, owner, session.ID, r.ID, chat.ReviewUnknown, 0)
	}
	return session, nil
}

func (s *Server) reviewChatCheckout(ctx context.Context, actor Identity, id string, plan chat.Plan, d *taskconfig.Draft, digest string) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	var old domain.TaskRepository
	for _, r := range d.Repositories {
		if r.Alias == plan.CheckoutAlias {
			old = r
			break
		}
	}
	if old.Alias == "" {
		out.Plan.Kind = chat.Clarify
		out.Text = "Which existing checkout alias should I replace?"
		return out
	}
	if old.SCMConnection != "" && plan.DeliveryConnection == "" {
		out.Plan.Kind = chat.Clarify
		out.Text = checkoutChoicePrompt
		return out
	}
	if old.SCMConnection == "" && plan.DeliveryConnection != "" {
		out.Text = "This task uses local delivery. Replace the checkout locally or review the delivery settings for every repository in Tasks."
		return out
	}
	projects, err := (projectconfig.Manager{Store: s.Store}).List(ctx)
	if err != nil {
		out.Text = "Could not read registered projects. Retry this request."
		out.Retryable = true
		return out
	}
	target, err := findDraftProject(projects, plan.ReplacementProject)
	if err != nil {
		out.Plan.Kind = chat.Clarify
		out.Text = "Which registered replacement project should I use? Enter its exact ID or unique name."
		return out
	}
	next := domain.TaskRepository{Alias: old.Alias, ProjectID: target.ID, BaseCommit: target.Head, SourceRef: target.RepositoryRef, Checks: target.Checks, RequiredChecks: old.RequiredChecks}
	if old.SCMConnection != "" {
		c, _, err := s.checkoutDelivery(ctx, target, plan.DeliveryConnection, plan.DeliveryBranch, d.ID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) {
				out.Plan.Kind = chat.Clarify
				out.Text = "Choose a connection whose repository matches the registered project's origin and a valid PR base branch. " + checkoutChoicePrompt
			} else {
				out.Text, out.Retryable = "Could not verify the delivery connection. Retry this request.", true
			}
			return out
		}
		next.SCMConnection, next.Repository, next.TargetBranch = c.Name, c.Repository, plan.DeliveryBranch
		next.ConnectionRevision = c.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	out.Review, err = chat.NewTaskCheckoutReview(actor.chatOwner, id, d, chat.TaskCheckoutChange{Before: old, After: next, ProjectRevision: target.Revision}, digest, time.Now())
	if err != nil {
		out.Text = "Could not prepare this replacement. Check the distinct project, its required checks and the editable draft state."
		return out
	}
	out.Text = "Review the replacement checkout and its checks before saving."
	return out
}

const checkoutChoicePrompt = "Include the new PR destination in your message as connection=<name> branch=<base-branch>."

func checkoutDestinationChoice(input string) (string, string, string) {
	var connection, branch string
	var haveConnection, haveBranch bool
	for _, field := range strings.Fields(input) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "connection":
			if haveConnection || value == "" {
				return "", "", checkoutChoicePrompt
			}
			connection, haveConnection = value, true
		case "branch":
			if haveBranch || !scm.ValidBaseBranch(value) {
				return "", "", checkoutChoicePrompt
			}
			branch, haveBranch = value, true
		}
	}
	if haveConnection != haveBranch {
		return "", "", checkoutChoicePrompt
	}
	return connection, branch, ""
}

func (s *Server) checkoutDelivery(ctx context.Context, project *projectconfig.Project, connection, branch, task string) (scm.View, storage.SettingRevision, error) {
	c, guard, err := (scm.Manager{Store: s.Store}).ReviewConnection(ctx, connection)
	if err != nil {
		return scm.View{}, storage.SettingRevision{}, err
	}
	if !scm.ValidBaseBranch(branch) || branch == c.Prefix()+domain.TaskExecutionID(task) {
		return scm.View{}, storage.SettingRevision{}, storage.ErrConflict
	}
	if err = projectconfig.MatchDelivery(ctx, project, c.Repository, c.PublicRemoteURL(), c.Repository); err != nil {
		return scm.View{}, storage.SettingRevision{}, storage.ErrConflict
	}
	return c, guard, nil
}
