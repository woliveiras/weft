// Package events is the platform's event bus. Every fact (state change,
// message movement, tool call, approval, agent narration) is appended to the
// durable event log in storage first and only then fanned out to live
// subscribers.
//
// Live subscribers do not rely on in-memory delivery: a subscriber tails
// the log by sequence number and is merely *woken* by local publishes. That
// makes the stream correct across several platform instances (an event
// appended by instance B is seen by an SSE client connected to instance A
// within one poll interval) and lets a reconnecting client resume exactly
// where it left off (Last-Event-ID = seq), at the cost of a store read per
// wake-up.
package events

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type Bus struct {
	store storage.Store
	log   *slog.Logger

	mu      sync.Mutex
	waiters map[chan struct{}]struct{}
}

func NewBus(store storage.Store, log *slog.Logger) *Bus {
	return &Bus{store: store, log: log, waiters: map[chan struct{}]struct{}{}}
}

// Publish appends ev to the log and wakes local subscribers. Failures to
// persist are logged, not returned: losing an observability event must
// never fail the operation it describes.
func (b *Bus) Publish(ctx context.Context, ev *domain.Event) {
	if ev.ID == "" {
		ev.ID = domain.NewID("evt")
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	// Detach from request cancellation: the event describes something that happened.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := b.store.AppendEvent(pctx, ev); err != nil {
		b.log.Warn("event not persisted", "type", ev.Type, "err", err)
		return
	}
	b.wake()
}

// Audit implements tools.Auditor.
func (b *Bus) Audit(ctx context.Context, ev *domain.Event) { b.Publish(ctx, ev) }

// Emit is a convenience for building and publishing an event.
func (b *Bus) Emit(ctx context.Context, t domain.EventType, graph, node string, replica int, data any) {
	b.Publish(ctx, domain.NewEvent(t, graph, data).WithNode(node, replica))
}

func (b *Bus) wake() {
	b.mu.Lock()
	for w := range b.waiters {
		select {
		case w <- struct{}{}:
		default:
		}
	}
	b.mu.Unlock()
}

// Subscribe streams events matching f (AfterSeq is the resume point) to fn
// until ctx ends or fn returns an error. fn is called with nil as a
// heartbeat after 15s without events, so stream writers can keep idle
// connections alive from the same goroutine that writes events.
func (b *Bus) Subscribe(ctx context.Context, f storage.EventFilter, fn func(*domain.Event) error) error {
	w := make(chan struct{}, 1)
	b.mu.Lock()
	b.waiters[w] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.waiters, w)
		b.mu.Unlock()
	}()
	f.Ascending = true
	if f.Limit <= 0 {
		f.Limit = 500
	}
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	last := time.Now()
	for {
		evs, err := b.store.ListEvents(ctx, f)
		if err != nil {
			return err
		}
		for _, ev := range evs {
			if err := fn(ev); err != nil {
				return err
			}
			f.AfterSeq = ev.Seq
			last = time.Now()
		}
		if time.Since(last) > 15*time.Second {
			if err := fn(nil); err != nil {
				return err
			}
			last = time.Now()
		}
		if len(evs) == f.Limit {
			continue // more backlog to drain
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w:
		case <-poll.C:
		}
	}
}

// LatestSeq returns the newest sequence number (0 if the log is empty), so
// a fresh live subscriber can start at "now" rather than replaying history.
func (b *Bus) LatestSeq(ctx context.Context, graph string) int64 {
	evs, err := b.store.ListEvents(ctx, storage.EventFilter{GraphID: graph, Limit: 1})
	if err != nil || len(evs) == 0 {
		return 0
	}
	return evs[0].Seq
}
