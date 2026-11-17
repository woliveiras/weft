package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const blockedHistoryPrefix = "environment/blocked-history/"
const blockedHistoryLimit = 1000
const blockedHistoryAge = 7 * 24 * time.Hour

type BlockedRequest struct {
	ProxyRequest
	Proxy string `json:"proxy"`
	Phase string `json:"phase"`
}
type BlockedHistory struct {
	Requests      []BlockedRequest `json:"requests"`
	RetentionDays int              `json:"retentionDays"`
	Limit         int              `json:"limit"`
	CollectedAt   *time.Time       `json:"collectedAt,omitempty"`
}

func retainBlocked(rows []BlockedRequest, now time.Time, policy RetentionPolicy) []BlockedRequest {
	result := make([]BlockedRequest, 0)
	seen := map[BlockedRequest]bool{}
	for _, row := range rows {
		if row.Outcome != "denied" || !row.Time.After(now.Add(-time.Duration(policy.Days)*24*time.Hour)) || row.Time.After(now) {
			continue
		}
		key := row
		key.Time = key.Time.UTC().Round(0)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, row)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Time.After(result[j].Time) })
	if len(result) > policy.MaxEvents {
		result = result[:policy.MaxEvents]
	}
	return result
}

// CAS preserves concurrent collectors' updates; storage remains bounded per graph.
func (m *Manager) updateBlocked(ctx context.Context, graph string, rows []BlockedRequest, collected bool) (BlockedHistory, error) {
	for attempt := 0; attempt < 8; attempt++ {
		settings, err := m.Retention(ctx)
		if err != nil {
			return BlockedHistory{}, err
		}
		policy := settings.Policy
		v := BlockedHistory{}
		var revision int64
		saved, err := m.opt.Store.GetSetting(ctx, blockedHistoryPrefix+graph)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return v, err
		}
		if err == nil {
			revision = saved.Revision
			if err := json.Unmarshal(saved.Value, &v); err != nil {
				return v, fmt.Errorf("blocked access history is invalid")
			}
		}
		v.RetentionDays, v.Limit = policy.Days, policy.MaxEvents
		now := time.Now().UTC()
		v.Requests = retainBlocked(append(v.Requests, rows...), now, policy)
		if collected {
			v.CollectedAt = &now
		}
		if revision != 0 && !collected && len(v.Requests) == 0 && (v.CollectedAt == nil || v.CollectedAt.Before(now.Add(-time.Duration(policy.Days)*24*time.Hour))) {
			err := m.opt.Store.DeleteSetting(ctx, blockedHistoryPrefix+graph, revision)
			if errors.Is(err, storage.ErrConflict) {
				continue
			}
			return v, err
		}
		data, err := json.Marshal(v)
		if err != nil {
			return v, err
		}
		if !collected && saved != nil && string(saved.Value) == string(data) {
			return v, nil
		}
		if revision == 0 && !collected && len(v.Requests) == 0 {
			return v, nil
		}
		_, err = m.opt.Store.PutSetting(ctx, blockedHistoryPrefix+graph, data, revision, "proxy-collector")
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		return v, err
	}
	return BlockedHistory{}, fmt.Errorf("blocked access history update conflicted")
}

func (m *Manager) BlockedHistory(ctx context.Context, graph string) (BlockedHistory, error) {
	return m.updateBlocked(ctx, graph, nil, false)
}
func (m *Manager) saveBlocked(ctx context.Context, graph string, proxies []ProxyStatus) error {
	rows := []BlockedRequest{}
	for _, proxy := range proxies {
		if proxy.Warning != "" {
			return fmt.Errorf("proxy history collection incomplete")
		}
		for _, request := range proxy.Requests {
			if request.Outcome == "denied" {
				rows = append(rows, BlockedRequest{request, proxy.Name, proxy.Phase})
			}
		}
	}
	_, err := m.updateBlocked(ctx, graph, rows, true)
	return err
}
func (m *Manager) CollectBlockedHistory(ctx context.Context, graph string) error {
	view, err := m.ProxyStatus(ctx, graph)
	if err != nil {
		return err
	}
	if view.Truncated {
		return fmt.Errorf("proxy history inventory truncated")
	}
	return m.saveBlocked(ctx, graph, view.Proxies)
}

// RunProxyHistory collects independently of console activity. Expiration also
// covers deleted graphs, whose remaining history is swept until empty.
func (m *Manager) RunProxyHistory(ctx context.Context) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		graphs, err := m.opt.Store.ListGraphs(ctx)
		if err == nil {
			for _, graph := range graphs {
				if ctx.Err() != nil {
					return
				}
				if err := m.CollectBlockedHistory(ctx, graph.Metadata.ID); err != nil {
					m.opt.Log.Warn("proxy history collection failed", "graph", graph.Metadata.ID, "error", err)
				}
			}
		} else if ctx.Err() == nil {
			m.opt.Log.Warn("proxy history inventory failed", "error", err)
		}
		saved, err := m.opt.Store.ListSettings(ctx, blockedHistoryPrefix)
		if err != nil && ctx.Err() == nil {
			m.opt.Log.Warn("proxy history retention failed", "error", err)
		}
		for _, item := range saved {
			if ctx.Err() != nil {
				return
			}
			if _, err := m.BlockedHistory(ctx, strings.TrimPrefix(item.Key, blockedHistoryPrefix)); err != nil {
				m.opt.Log.Warn("proxy history retention failed", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
