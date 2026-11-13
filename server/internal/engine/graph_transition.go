package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

// WithGraphTransition serializes lifecycle changes across controllers. Bound
// the work before lease expiry so a slow request cannot overlap its successor.
func (e *Engine) WithGraphTransition(ctx context.Context, id string, run func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	key := "graph-transition/" + id
	holder := domain.NewID("graph-transition")
	ok, err := e.store.AcquireLease(ctx, key, holder, 2*time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: graph %s has a lifecycle operation in progress", ErrConflict, id)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if err := e.store.ReleaseLease(cleanup, key, holder); err != nil {
			e.log.Warn("graph transition lease release failed", "graph", id, "err", err)
		}
	}()
	return run(ctx)
}

func (e *Engine) CheckGraphStopped(ctx context.Context, id string) error {
	_, rec, err := e.GetGraph(ctx, id, 0)
	if err != nil {
		return err
	}
	if rec.DesiredState != domain.GraphStopped || (rec.State != domain.GraphStopped && rec.State != "") {
		return fmt.Errorf("%w: stop the graph before deleting it", ErrConflict)
	}
	return ctx.Err()
}
