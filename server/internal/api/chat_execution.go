package api

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Server) inspectChatExecution(ctx context.Context, plan chat.Plan) chat.Interpretation {
	out := chat.Interpretation{Plan: plan}
	tree, err := (executioncontrol.Manager{Store: s.Store}).Tree(ctx, plan.ExecutionID)
	if errors.Is(err, storage.ErrNotFound) {
		out.Text = "Execution not found. Check the exact execution ID in Activity."
		out.Destination = "activity"
		return out
	}
	if err == nil {
		if summary, ok := chat.SummarizeExecution(tree); ok {
			out.Execution = summary
			out.Text = "Current execution state"
			return out
		}
	}
	out.Text = "The execution tree is unavailable. Retry the saved request."
	out.Retryable = true
	return out
}
