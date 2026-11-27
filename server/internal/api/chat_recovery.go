package api

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Only known dispositions cross the transcript boundary.
func chatRecovery(modelErr, callErr error) chat.Interpretation {
	result := chat.Interpretation{Retryable: true}
	switch {
	case errors.Is(callErr, cost.ErrUnpriced):
		result.Text = "The Chat model needs a price under the current budget policy. Review model prices before retrying this saved request."
		result.Destination = "prices"
	case errors.Is(callErr, cost.ErrBudgetReached):
		result.Text = "The current budget prevented Chat from calling its model. Review the budget before retrying this saved request."
		result.Destination = "budgets"
	case errors.Is(callErr, tools.ErrRateLimited):
		result.Text = "Chat is handling its maximum number of model calls. Retry this saved request when a slot is available."
	case errors.Is(callErr, context.DeadlineExceeded) || errors.Is(callErr, context.Canceled), errors.Is(modelErr, context.Canceled), errors.Is(modelErr, context.DeadlineExceeded):
		result.Text = "The model call ended before Chat received an answer. Retry this saved request."
	case errors.Is(modelErr, chat.ErrTruncated):
		result.Text = "The model reached its output limit before completing this request."
	case errors.Is(modelErr, chat.ErrCheckpointsUnavailable):
		result.Text = "Chat could not verify the workspace checkpoints. Retry the query."
	case errors.Is(modelErr, chat.ErrSpendUnavailable):
		result.Text = "Chat could not verify the spend report. Retry the query."
		result.Destination = "usage"
	case errors.Is(modelErr, chat.ErrReadUnavailable):
		result.Text = "Chat could not verify the task records. Retry the query."
	case errors.Is(modelErr, chat.ErrInvalidPlan):
		result.Text = "The Chat model returned an unsupported operation. Rephrase the request or inspect the available console pages."
	default:
		result.Text = "Chat could not use the selected model. Check the provider and retry this saved request."
		result.Destination = "chat-model"
	}
	return result
}
