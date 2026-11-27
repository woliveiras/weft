package api

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

func (s *Server) queryChatCheckpoints(ctx context.Context, actor Identity, raw json.RawMessage) (chat.ToolObservation, error) {
	if !actor.Role.Allows(config.RoleViewer) {
		return chat.ToolObservation{}, tools.ErrDenied
	}
	q, err := chat.ParseCheckpointQuery(raw)
	if err != nil {
		return chat.ToolObservation{}, err
	}
	dest := "workflows"
	reply := func(data any) (chat.ToolObservation, error) {
		b, err := json.Marshal(data)
		return chat.ToolObservation{Data: b, Title: q.Title, Destination: dest}, err
	}
	graphID := q.Target
	if strings.HasPrefix(q.Target, "tsk_") {
		dest = "tasks"
		task, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, q.Target)
		if errors.Is(err, storage.ErrNotFound) {
			return reply(map[string]string{"status": "task_not_found", "target": q.Target})
		}
		if err != nil || task.ID != q.Target {
			return chat.ToolObservation{}, errors.Join(chat.ErrCheckpointsUnavailable, err)
		}
		graphID = task.ExecutionGraphID
		if graphID == "" {
			return reply(map[string]string{"status": "task_has_no_execution", "target": q.Target})
		}
	}
	g, _, err := s.Engine.GetGraph(ctx, graphID, 0)
	if errors.Is(err, engine.ErrNotFound) && graphID == q.Target {
		return reply(map[string]string{"status": "workflow_not_found", "target": q.Target})
	}
	if err != nil {
		return chat.ToolObservation{}, errors.Join(chat.ErrCheckpointsUnavailable, err)
	}
	var aliases []string
	for _, r := range g.Spec.Workspace.Repositories {
		if !domain.ValidRepositoryAlias(r.Alias) {
			return chat.ToolObservation{}, chat.ErrCheckpointsUnavailable
		}
		aliases = append(aliases, r.Alias)
	}
	if q.Repository == "" && len(aliases) == 1 {
		q.Repository = aliases[0]
	}
	if q.Repository != "" && !slices.Contains(aliases, q.Repository) {
		return reply(map[string]any{"status": "unknown_repository_alias", "graphId": graphID, "availableAliases": aliases})
	}
	if q.Repository == "" && len(aliases) > 1 {
		return reply(map[string]any{"status": "repository_selection_required", "graphId": graphID, "availableAliases": aliases})
	}
	items, err := s.Engine.ListCheckpoints(ctx, graphID, q.Repository)
	if err != nil {
		return chat.ToolObservation{}, errors.Join(chat.ErrCheckpointsUnavailable, err)
	}
	rows, err := chat.RecentCheckpointRows(items)
	if err != nil {
		return chat.ToolObservation{}, err
	}
	return reply(map[string]any{"status": "ok", "graphId": graphID, "repository": q.Repository, "total": len(items), "latest": rows})
}
