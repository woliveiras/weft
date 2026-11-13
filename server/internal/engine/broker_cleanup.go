package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const brokerCleanupNamespace = "graph-broker-cleanup-v1"

type brokerGroup struct{ Subject, Group string }

// Call only after graph deletion or before creating a graph with this ID,
// while the caller holds the graph transition lease.
func (e *Engine) retryBrokerCleanup(ctx context.Context, id string) error {
	data, err := e.store.KVGet(ctx, brokerCleanupNamespace, id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var groups []brokerGroup
	if err := json.Unmarshal(data, &groups); err != nil {
		return fmt.Errorf("decode pending broker cleanup: %w", err)
	}
	for _, group := range groups {
		if err := e.opt.Broker.DeleteGroup(ctx, group.Subject, group.Group); err != nil {
			return fmt.Errorf("%w: pending broker cleanup for graph %s: %v", ErrConflict, id, err)
		}
	}
	if err := e.store.KVDelete(ctx, brokerCleanupNamespace, id); err != nil {
		return err
	}
	e.bus.Publish(ctx, domain.NewEvent(domain.EvGraphBrokerCleanupCompleted, id, map[string]int{
		"groups": len(groups),
	}).WithActor("system:broker"))
	return nil
}

// A deletion can fail after writing the cleanup record. Verify the graph is
// gone under its transition lease before removing any broker groups.
func (e *Engine) reconcileBrokerCleanup(ctx context.Context, liveGraphs map[string]bool) {
	now := time.Now()
	if now.Sub(e.brokerCleanupSweepAt) < time.Second {
		return
	}
	e.brokerCleanupSweepAt = now
	work, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := e.store.KVList(work, brokerCleanupNamespace, "")
	if err != nil {
		if ctx.Err() == nil {
			e.log.Warn("list pending broker cleanup", "err", err)
			e.brokerCleanupSweepAt = now.Add(4 * time.Second)
		}
		return
	}
	if e.brokerCleanupRetries == nil {
		e.brokerCleanupRetries = make(map[string]brokerCleanupRetry)
	}
	for id := range e.brokerCleanupRetries {
		if _, ok := rows[id]; !ok {
			delete(e.brokerCleanupRetries, id)
		}
	}

	var failures int
	var firstID string
	var firstErr error
	for id := range rows {
		if work.Err() != nil {
			break
		}
		retry := e.brokerCleanupRetries[id]
		if liveGraphs[id] || now.Before(retry.next) {
			continue
		}
		attempt, finish := context.WithTimeout(work, time.Second)
		err := e.WithGraphTransition(attempt, id, func(ctx context.Context) error {
			if _, err := e.store.GetGraphRecord(ctx, id); !errors.Is(err, storage.ErrNotFound) {
				return err
			}
			return e.retryBrokerCleanup(ctx, id)
		})
		finish()
		if err == nil {
			delete(e.brokerCleanupRetries, id)
			continue
		}
		if errors.Is(err, ErrGraphTransitionBusy) || ctx.Err() != nil {
			continue
		}
		e.brokerCleanupRetries[id] = retry.afterFailure(now)
		failures++
		if firstErr == nil {
			firstID, firstErr = id, err
		}
	}
	if failures > 0 {
		e.log.Warn("broker cleanup will retry", "failedGraphs", failures, "graph", firstID, "err", firstErr)
	}
}

type brokerCleanupRetry struct {
	next  time.Time
	delay time.Duration
}

func (r brokerCleanupRetry) afterFailure(now time.Time) brokerCleanupRetry {
	delay := min(max(2*r.delay, 2*time.Second), time.Minute)
	return brokerCleanupRetry{next: now.Add(delay), delay: delay}
}
