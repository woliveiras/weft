package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// DeferredCleanup identifies the exact graph-owned resources selected by Reset.
// Shared generations remain in the existing generation inventory.
type DeferredCleanup struct {
	Graph  string    `json:"graph"`
	Actor  string    `json:"actor"`
	Since  time.Time `json:"since"`
	Daemon string    `json:"daemon"`
}

func (m *Manager) recordDeferredCleanup(ctx context.Context, id, actor string) error {
	if _, err := m.opt.Store.KVGet(ctx, domain.DeferredEnvironmentCleanup, id); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	data, err := json.Marshal(DeferredCleanup{Graph: id, Actor: actor, Since: time.Now().UTC(), Daemon: m.cleanupDaemon()})
	if err != nil {
		return err
	}
	return m.opt.Store.KVPut(ctx, domain.DeferredEnvironmentCleanup, id, data)
}

func (m *Manager) ListDeferredCleanup(ctx context.Context) ([]DeferredCleanup, error) {
	rows, err := m.opt.Store.KVList(ctx, domain.DeferredEnvironmentCleanup, "")
	if err != nil {
		return nil, err
	}
	result := make([]DeferredCleanup, 0, len(rows))
	for id, row := range rows {
		item, err := decodeDeferredCleanup(id, row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Graph < result[j].Graph })
	return result, nil
}

func decodeDeferredCleanup(id string, data []byte) (DeferredCleanup, error) {
	var item DeferredCleanup
	if err := json.Unmarshal(data, &item); err != nil {
		return item, err
	}
	if item.Graph != id || item.Daemon == "" {
		return item, fmt.Errorf("invalid deferred environment cleanup record for graph %s", id)
	}
	return item, nil
}

// RetryDeferredCleanup runs only for a deleted graph on its original daemon.
func (m *Manager) RetryDeferredCleanup(ctx context.Context, id, actor string) error {
	return m.resetWithLease(ctx, id, actor, func(ctx context.Context) error {
		if _, err := m.opt.Store.GetGraph(ctx, id, 0); err == nil {
			return fmt.Errorf("%w: graph still exists", storage.ErrConflict)
		} else if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		row, err := m.opt.Store.KVGet(ctx, domain.DeferredEnvironmentCleanup, id)
		if err != nil {
			return err
		}
		item, err := decodeDeferredCleanup(id, row)
		if err != nil {
			return err
		}
		if item.Daemon != m.cleanupDaemon() {
			return fmt.Errorf("%w: deferred cleanup belongs to a different sandbox daemon", storage.ErrConflict)
		}
		if err := m.resetResources(ctx, id, actor); err != nil {
			return err
		}
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		if err := m.opt.Store.KVDelete(ctx, domain.DeferredEnvironmentCleanup, id); err != nil {
			return err
		}
		m.publish(context.WithoutCancel(ctx), "environment.deferred_cleanup_completed", id, actor, nil)
		return nil
	})
}
