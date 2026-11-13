package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

var ErrGraphTransitionBusy = fmt.Errorf("%w: graph lifecycle operation in progress", ErrConflict)

// WithGraphTransition serializes lifecycle changes across controllers. Bound
// the work at two minutes, below the three-minute crash expiry.
func (e *Engine) WithGraphTransition(ctx context.Context, id string, run func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	key := "graph-transition/" + id
	holder := domain.NewID("graph-transition")
	ok, err := e.store.AcquireLease(ctx, key, holder, 3*time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w for graph %s", ErrGraphTransitionBusy, id)
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

func (e *Engine) withGraphActionTransition(ctx context.Context, id, action string, run func(context.Context) error) error {
	if action == "stop" || action == "kill" {
		return e.waitGraphTransition(ctx, id, run)
	}
	return e.WithGraphTransition(ctx, id, run)
}

func (e *Engine) waitGraphTransition(ctx context.Context, id string, run func(context.Context) error) error {
	waitUntil := time.Now().Add(3 * time.Second)
	for {
		err := e.WithGraphTransition(ctx, id, run)
		if !errors.Is(err, ErrGraphTransitionBusy) || time.Now().After(waitUntil) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (e *Engine) checkDeferredDeletion(ctx context.Context, id string) error {
	_, err := e.store.KVGet(ctx, domain.DeferredEnvironmentCleanup, id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: resolve deferred environment deletion for graph %s", ErrConflict, id)
}
