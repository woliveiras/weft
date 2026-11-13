package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// DeferredCleanup records the graph and daemon used to rediscover resources by name and label.
// Shared generations remain in the existing generation inventory.
type DeferredCleanup struct {
	Graph             string    `json:"graph"`
	Actor             string    `json:"actor"`
	Since             time.Time `json:"since"`
	Daemon            string    `json:"daemon"`
	Reason            string    `json:"reason,omitempty"`
	GraphExists       bool      `json:"graphExists"`
	DaemonMatches     bool      `json:"daemonMatches"`
	PossibleResources []string  `json:"possibleResources,omitempty"`
}

func (m *Manager) recordDeferredCleanup(ctx context.Context, id, actor, reason string) (bool, error) {
	if _, err := m.opt.Store.KVGet(ctx, domain.DeferredEnvironmentCleanup, id); err == nil {
		return false, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return false, err
	}
	if reason == "" {
		reason = "Administrator requested deletion with environment cleanup deferred"
	}
	data, err := json.Marshal(DeferredCleanup{Graph: id, Actor: actor, Since: time.Now().UTC(), Daemon: m.cleanupDaemon(), Reason: reason})
	if err != nil {
		return false, err
	}
	err = m.opt.Store.KVPut(ctx, domain.DeferredEnvironmentCleanup, id, data)
	return err == nil, err
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
		if _, err := m.opt.Store.GetGraph(ctx, id, 0); err == nil {
			item.GraphExists = true
		} else if !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		item.DaemonMatches = item.Daemon == m.cleanupDaemon()
		item.PossibleResources = deferredResourceSelectors(id)
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

// These are selectors, not a live inventory. They are safe to record even when
// the daemon that owns the resources is unavailable.
func deferredResourceSelectors(id string) []string {
	return []string{
		"Image repository: " + Repository(id),
		"Volumes labeled weft.graph=" + id,
		"Buildx builder: " + builderName(id),
		"Buildx state volume: " + builderStateVolume(id),
		"Docker context: " + builderEndpointName(id),
		"Containers: " + strings.Join([]string{builderContainer(id), buildContainer(id), buildContainer(id) + "-copy", preparationProxy(id)}, ", "),
		"Network: " + preparationNetworkName(id),
	}
}

var ErrInvalidDeferredReason = errors.New("a cleanup reason of 1 to 500 characters is required")

// AbandonDeferredCleanup releases the ID without contacting Docker. When the
// graph still exists, this cancels an interrupted deletion instead.
func (m *Manager) AbandonDeferredCleanup(ctx context.Context, id, actor, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return ErrInvalidDeferredReason
	}
	work, release, err := m.holdResetLease(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	row, err := m.opt.Store.KVGet(work, domain.DeferredEnvironmentCleanup, id)
	if err != nil {
		return err
	}
	item, err := decodeDeferredCleanup(id, row)
	if err != nil {
		return err
	}
	kind := domain.EventType("environment.deferred_cleanup_abandon_requested")
	exists := false
	if _, err := m.opt.Store.GetGraph(work, id, 0); err == nil {
		exists = true
		kind = "environment.deferred_cleanup_cancel_requested"
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	event := domain.NewEvent(kind, id, map[string]any{
		"reason": reason, "daemon": item.Daemon, "possibleResources": deferredResourceSelectors(id),
	}).WithActor(actor)
	// Persist the audit before releasing the reservation. A store failure
	// leaves the reservation available for retry.
	if err := m.opt.Store.AppendEvent(work, event); err != nil {
		return fmt.Errorf("record deferred cleanup decision: %w", err)
	}
	if err := work.Err(); err != nil {
		return context.Cause(work)
	}
	if !exists {
		for _, namespace := range []string{kvNamespace, proxyHistoryNamespace} {
			if err := m.opt.Store.KVDelete(work, namespace, id); err != nil && !errors.Is(err, storage.ErrNotFound) {
				return err
			}
		}
	}
	if err := m.opt.Store.KVDelete(work, domain.DeferredEnvironmentCleanup, id); err != nil {
		return err
	}
	m.publish(context.WithoutCancel(work), "environment.deferred_cleanup_reservation_released", id, actor, map[string]any{"decision": event.ID})
	return nil
}
