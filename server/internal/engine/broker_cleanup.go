package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	return e.store.KVDelete(ctx, brokerCleanupNamespace, id)
}
