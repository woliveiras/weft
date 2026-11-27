package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestChatRecoveryShowsSafeNextAction(t *testing.T) {
	for _, tc := range []struct {
		model, call error
		dest, text  string
	}{
		{chat.ErrModelUnavailable, errors.Join(cost.ErrBudgetReached, errors.New("private-detail")), "budgets", "budget"},
		{chat.ErrModelUnavailable, tools.ErrRateLimited, "", "maximum number"},
		{chat.ErrInvalidPlan, nil, "", "unsupported"},
		{chat.ErrTruncated, nil, "", "output limit"},
		{errors.Join(chat.ErrSpendUnavailable, errors.New("private-detail")), nil, "usage", "verify the spend report"},
		{errors.Join(chat.ErrReadUnavailable, errors.New("private-detail")), nil, "", "verify the task records"},
		{chat.ErrModelUnavailable, errors.New("private-detail"), "chat-model", "selected model"},
	} {
		got := chatRecovery(tc.model, tc.call)
		if !got.Retryable || got.Destination != tc.dest || !strings.Contains(got.Text, tc.text) || strings.Contains(got.Text, "private-detail") {
			t.Fatalf("recovery %+v", got)
		}
	}
}
