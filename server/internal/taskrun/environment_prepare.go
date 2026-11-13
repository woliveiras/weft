package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (m Manager) preparedEnvironmentGraph(ctx context.Context, d *taskconfig.Draft) (*domain.Graph, error) {
	if d.ExecutionGraphID != ExecutionID(d.ID) {
		return nil, storage.ErrConflict
	}
	g, err := m.Store.GetGraph(ctx, d.ExecutionGraphID, 0)
	if err != nil {
		return nil, err
	}
	if g.Metadata.Labels["weft.task"] != d.ID || g.Metadata.Labels["weft.workflow"] != d.GraphID || g.Spec.Environment == nil {
		return nil, storage.ErrConflict
	}
	return g, nil
}

// PrepareEnvironment makes a durable stopped copy before a separately reviewed
// build. Neither successful build nor a repeated request dispatches task input.
func (m Manager) PrepareEnvironment(ctx context.Context, id string, revision int64, actor string) (*taskconfig.Draft, error) {
	if m.Environments == nil {
		return nil, errors.New("environment preparation unavailable")
	}
	tm := taskconfig.Manager{Store: m.Store}
	d, err := tm.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.Revision != revision {
		return nil, storage.ErrConflict
	}
	if d.Status == "environment_pending" {
		if err := m.checkDraftBudget(ctx, d); err != nil {
			return nil, err
		}
		g, err := m.preparedEnvironmentGraph(ctx, d)
		if err != nil {
			return nil, err
		}
		return m.buildPreparedEnvironment(ctx, d, g, actor)
	}
	if d.Status != "draft" {
		return nil, storage.ErrConflict
	}
	if err := m.checkDraftBudget(ctx, d); err != nil {
		return nil, err
	}
	review, err := m.Review(ctx, id)
	if err != nil {
		return nil, err
	}
	if !review.CanPrepareEnvironment {
		return nil, fmt.Errorf("%w: review environment requirements first", taskconfig.ErrInvalid)
	}
	source, err := m.Store.GetGraph(ctx, d.GraphID, d.GraphVersion)
	if err != nil {
		return nil, err
	}
	g, blocks, err := m.prepare(ctx, d, source)
	if err != nil {
		return nil, err
	}
	if len(blocks) > 0 {
		return nil, taskconfig.ErrInvalid
	}
	d.ExecutionGraphID = g.Metadata.ID
	d, err = tm.Transition(ctx, d, "preparing", actor)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*taskconfig.Draft, error) {
		d.LaunchError = "Environment preparation did not complete. Retained resources require inspection; preparation will not be replayed automatically."
		_, saveErr := tm.Transition(context.WithoutCancel(ctx), d, "interrupted", actor)
		return nil, errors.Join(cause, saveErr)
	}
	if _, err = m.Store.GetGraph(ctx, g.Metadata.ID, 0); !errors.Is(err, storage.ErrNotFound) {
		if err == nil {
			err = storage.ErrConflict
		}
		return fail(err)
	}
	if d.ProjectID != "" {
		project, err := (projectconfig.Manager{Store: m.Store}).Get(ctx, d.ProjectID)
		if err != nil {
			return fail(err)
		}
		if m.Workspaces == nil {
			return fail(errors.New("task workspaces unavailable"))
		}
		if _, err = projectconfig.CreateWorktree(ctx, project, d.BaseCommit, m.Workspaces.Root, g.Metadata.ID); err != nil {
			return fail(err)
		}
	}
	if _, _, err = m.Engine.SaveGraph(ctx, g, 0, actor); err != nil {
		return fail(err)
	}
	return m.buildPreparedEnvironment(ctx, d, g, actor)
}

func (m Manager) startPreparedEnvironment(ctx context.Context, d *taskconfig.Draft, revision int64, actor string) (*taskconfig.Draft, error) {
	if d.Revision != revision {
		return nil, storage.ErrConflict
	}
	if err := m.checkDraftBudget(ctx, d); err != nil {
		return nil, err
	}
	review, err := m.Review(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	if !review.CanStart {
		return nil, fmt.Errorf("%w: environment must be ready and requirements reviewed", taskconfig.ErrInvalid)
	}
	g, err := m.preparedEnvironmentGraph(ctx, d)
	if err != nil {
		return nil, err
	}
	if err := (taskauth.Manager{Store: m.Store}).Ensure(ctx, d.ID, actor); err != nil {
		return nil, err
	}
	tm := taskconfig.Manager{Store: m.Store}
	d, err = tm.Transition(ctx, d, "starting", actor)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*taskconfig.Draft, error) {
		d.LaunchError = "Task start did not complete. No automatic input replay will occur."
		_, saveErr := tm.Transition(context.WithoutCancel(ctx), d, "interrupted", actor)
		return nil, errors.Join(cause, saveErr)
	}
	if err = (taskauth.Manager{Store: m.Store}).Bind(ctx, d.ID, g); err != nil {
		return fail(err)
	}
	if d.ChannelID != "" {
		if _, err = m.Engine.Inject(ctx, g.Metadata.ID, d.ChannelID, d.MessageType, json.RawMessage(d.PayloadJSON), d.NodeID, nil, actor); err != nil {
			return fail(err)
		}
	} else {
		input := &domain.OperatorMessage{ID: "task-input-" + d.ID, GraphID: g.Metadata.ID, NodeID: d.NodeID, Replica: 0, Text: d.Request, Author: actor, CreatedAt: time.Now().UTC()}
		if err = m.Store.PutOperatorMessageOnce(ctx, input); err != nil {
			return fail(err)
		}
	}
	if err = m.Engine.StartAdmittedTask(ctx, g.Metadata.ID, actor); err != nil {
		return fail(err)
	}
	return tm.Transition(ctx, d, "running", actor)
}

func (m Manager) buildPreparedEnvironment(ctx context.Context, d *taskconfig.Draft, g *domain.Graph, actor string) (*taskconfig.Draft, error) {
	tm := taskconfig.Manager{Store: m.Store}
	d, err := tm.Transition(ctx, d, "environment_build_requested", actor)
	if err != nil {
		return nil, err
	}
	_, buildErr := m.Environments.Build(ctx, g, false, actor)
	d.LaunchError = ""
	if buildErr != nil {
		d.LaunchError = "Environment build could not be started. Review its definition and diagnostics before an explicit retry."
	}
	d, saveErr := tm.Transition(context.WithoutCancel(ctx), d, "environment_pending", actor)
	return d, errors.Join(buildErr, saveErr)
}
