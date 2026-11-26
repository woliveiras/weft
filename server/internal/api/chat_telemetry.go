package api

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/tools"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func chatTelemetryOutcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, chat.ErrModelUnavailable):
		return "model_unavailable"
	case errors.Is(err, cost.ErrUnpriced):
		return "price_required"
	case errors.Is(err, cost.ErrBudgetReached):
		return "budget_rejected"
	case errors.Is(err, tools.ErrRateLimited):
		return "busy"
	default:
		return "dependency_error"
	}
}

// operation is assigned by the caller, never taken from a model or request.
func observeChatOperation(ctx context.Context, operation string) (context.Context, func(error)) {
	start := time.Now()
	ctx, span := observability.Start(ctx, "chat."+operation)
	return ctx, func(err error) {
		outcome := chatTelemetryOutcome(err)
		observability.ChatOperationSeconds.WithLabelValues(operation, outcome).Observe(time.Since(start).Seconds())
		span.SetAttributes(attribute.String("weft.outcome", outcome))
		if err != nil {
			span.SetStatus(codes.Error, outcome)
		}
		span.End()
	}
}
