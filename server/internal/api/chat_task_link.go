package api

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func (s *Server) linkChatTask(ctx context.Context, actor Identity, plan chat.Plan) chat.Interpretation {
	reply := chat.Interpretation{Plan: plan}
	if !actor.Role.Allows(config.RoleViewer) {
		reply.Text = "Viewer access is required to open a task."
		return reply
	}
	// Links are exact reads like the authenticated Task Details route. A
	// missing ID never falls through to a similarly named task.
	task, err := (taskconfig.Manager{Store: s.Store}).Get(ctx, plan.Task)
	if errors.Is(err, storage.ErrNotFound) {
		reply.Text = "No task was found with that ID. Check the task ID and try again."
		return reply
	}
	if err != nil || task.ID != plan.Task {
		reply.Text, reply.Retryable = "The task link is temporarily unavailable. Retry to check again.", true
		return reply
	}
	reply.Text = "Open the details for task " + task.ID + "."
	reply.Task = &chat.TaskSummary{ID: task.ID, LinkOnly: true}
	return reply
}
