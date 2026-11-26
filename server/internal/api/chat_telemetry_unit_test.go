package api

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestChatTelemetryUsesBoundedOutcomes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "ok"}, {context.Canceled, "cancelled"}, {context.DeadlineExceeded, "timeout"}, {tools.ErrRateLimited, "busy"},
		{errors.Join(errors.New("private account detail"), cost.ErrBudgetReached), "budget_rejected"},
		{chat.ErrModelUnavailable, "model_unavailable"}, {errors.New("private provider diagnostic"), "dependency_error"},
	} {
		if got := chatTelemetryOutcome(tc.err); got != tc.want {
			t.Errorf("outcome %q want %q", got, tc.want)
		}
	}
}
