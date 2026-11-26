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
	case errors.Is(callErr, cost.ErrBudgetReached):
		result.Text = "The current budget prevented Chat from calling its model. Review the budget before retrying this saved request."
		result.Destination = "budgets"
	case errors.Is(callErr, tools.ErrRateLimited):
		result.Text = "Chat is handling its maximum number of model calls. Retry this saved request when a slot is available."
	case errors.Is(callErr, context.DeadlineExceeded) || errors.Is(callErr, context.Canceled):
		result.Text = "The model call ended before Chat received an answer. Retry this saved request."
	case errors.Is(modelErr, chat.ErrInvalidPlan):
		result.Text = "The Chat model returned an unsupported operation. Rephrase the request or inspect the available console pages."
	default:
		result.Text = "Chat could not use the selected model. Check the provider and retry this saved request."
		result.Destination = "chat-model"
	}
	return result
}
