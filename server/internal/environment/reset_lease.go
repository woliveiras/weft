package environment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

const resetLeaseHolderPrefix = "environment-reset_"

func (m *Manager) environmentLeaseConflict(ctx context.Context, graphID string) error {
	key := leasePrefix + graphID
	leases, err := m.opt.Store.ListLeases(ctx, key)
	if err != nil {
		return fmt.Errorf("inspect environment lease for graph %s: %w", graphID, err)
	}
	for _, lease := range leases {
		if lease.Key == key && lease.ExpiresAt.After(time.Now()) && strings.HasPrefix(lease.Holder, resetLeaseHolderPrefix) {
			return fmt.Errorf("%w for graph %s", ErrResetRunning, graphID)
		}
	}
	return fmt.Errorf("%w for graph %s", ErrBuildRunning, graphID)
}

func (m *Manager) holdResetLease(ctx context.Context, graphID string) (context.Context, func(), error) {
	key := leasePrefix + graphID
	holder := domain.NewID("environment-reset")
	ttl := m.resetLeaseTTL
	started := time.Now()
	ok, err := m.opt.Store.AcquireLease(ctx, key, holder, ttl)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, m.environmentLeaseConflict(ctx, graphID)
	}

	work, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.renewResetLease(work, cancel, key, holder, started, ttl)
	}()
	return work, func() {
		cancel(nil)
		<-done
		release, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if err := m.opt.Store.ReleaseLease(release, key, holder); err != nil {
			m.opt.Log.Warn("environment reset lease release failed", "graph", graphID, "err", err)
		}
	}, nil
}

func (m *Manager) renewResetLease(ctx context.Context, cancel context.CancelCauseFunc, key, holder string, acquired time.Time, ttl time.Duration) {
	// Fence work before the last known expiry, even if a store call is slow.
	watchdog := time.AfterFunc(time.Until(acquired.Add(2*ttl/3)), func() { cancel(ErrResetLeaseLost) })
	defer watchdog.Stop()
	tick := time.NewTicker(ttl / 3)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			started := time.Now()
			ok, err := m.opt.Store.AcquireLease(ctx, key, holder, ttl)
			if err != nil {
				if ctx.Err() == nil {
					cancel(fmt.Errorf("%w: %v", ErrResetLeaseLost, err))
				}
				return
			}
			if !ok {
				cancel(ErrResetLeaseLost)
				return
			}
			watchdog.Reset(time.Until(started.Add(2 * ttl / 3)))
		}
	}
}
