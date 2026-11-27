package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

func (s *Server) chatReadTools(actor Identity) []chat.ReadTool {
	if !actor.Role.Allows(config.RoleViewer) {
		return nil
	}
	return []chat.ReadTool{
		{Schema: chat.TaskQuerySchema(), Read: func(ctx context.Context, args json.RawMessage) (chat.ToolObservation, error) {
			ctx, finish := observeChatOperation(ctx, "query_tasks")
			result, err := s.queryChatTasks(ctx, actor, args)
			finish(err)
			return result, err
		}},
		{Schema: chat.SpendQuerySchema(), Read: func(ctx context.Context, args json.RawMessage) (chat.ToolObservation, error) {
			ctx, finish := observeChatOperation(ctx, "query_spend")
			result, err := s.queryChatSpend(ctx, actor, args)
			finish(err)
			return result, err
		}},
		{Schema: chat.CheckpointQuerySchema(), Read: func(ctx context.Context, args json.RawMessage) (chat.ToolObservation, error) {
			ctx, finish := observeChatOperation(ctx, "list_checkpoints")
			result, err := s.queryChatCheckpoints(ctx, actor, args)
			finish(err)
			return result, err
		}},
	}
}

func (s *Server) queryChatSpend(ctx context.Context, actor Identity, args json.RawMessage) (chat.ToolObservation, error) {
	if !actor.Role.Allows(config.RoleViewer) {
		return chat.ToolObservation{}, tools.ErrDenied
	}
	q, err := chat.ParseSpendQuery(args, time.Now())
	if err != nil {
		return chat.ToolObservation{}, err
	}
	report, err := cost.Spend(ctx, s.Store, q.Period, "")
	if err != nil {
		return chat.ToolObservation{}, errors.Join(chat.ErrSpendUnavailable, err)
	}
	result, err := q.Observe(report)
	if err != nil {
		return chat.ToolObservation{}, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return chat.ToolObservation{}, err
	}
	return chat.ToolObservation{Data: data, Destination: "usage", Title: q.Title}, nil
}

func (s *Server) queryChatTasks(ctx context.Context, actor Identity, args json.RawMessage) (chat.ToolObservation, error) {
	if !actor.Role.Allows(config.RoleViewer) {
		return chat.ToolObservation{}, tools.ErrDenied
	}
	query, err := chat.ParseTaskQuery(args)
	if err != nil {
		return chat.ToolObservation{}, err
	}
	rows, err := (taskconfig.Manager{Store: s.Store}).RecentTasks(ctx)
	if err != nil {
		return chat.ToolObservation{}, errors.Join(chat.ErrReadUnavailable, err)
	}
	result := chat.TaskQueryResult{Mode: query.Mode, Status: query.Status}
	for _, d := range rows {
		if err := ctx.Err(); err != nil {
			return chat.ToolObservation{}, errors.Join(chat.ErrReadUnavailable, err)
		}
		state := d.Status
		// Terminal receipts and unbound drafts are already authoritative.
		if d.ExecutionGraphID != "" && !(chat.TaskSummary{State: state}).Terminal() {
			v, err := s.taskRunner().View(ctx, d.ID)
			if err != nil {
				return chat.ToolObservation{}, errors.Join(chat.ErrReadUnavailable, err)
			}
			state = v.State
		}
		if err := result.Add(chat.TaskQueryItem{ID: d.ID, Title: d.Title, Status: state, UpdatedAt: d.UpdatedAt}, query.Limit); err != nil {
			return chat.ToolObservation{}, err
		}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return chat.ToolObservation{}, err
	}
	// The model can summarize or reorder a list. Only the deterministic
	// inventory view can establish ordinal references for a later action.
	inventory := &chat.Inventory{Area: "tasks", TaskMode: query.Mode, TaskStatus: query.Status, Total: &result.Total, Items: []chat.InventoryReference{}}
	return chat.ToolObservation{Data: data, Inventory: inventory, Destination: "tasks", Title: query.Title}, nil
}
