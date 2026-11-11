package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

type quotaWindow struct {
	Minute int64 `json:"minute"`
	Used   int   `json:"used"`
}

// Each connection has one fixed UTC-minute window in shared storage. CAS makes
// admission aggregate across agents and controller instances, including restart.
// Admission is charged even when the subsequent remote call fails or is cancelled.
func (m *Manager) admitCall(ctx context.Context, r record) error {
	now := time.Now()
	if m.QuotaNow != nil {
		now = m.QuotaNow()
	}
	return m.admitCallAt(ctx, r, now)
}
func (m *Manager) admitCallAt(ctx context.Context, r record, now time.Time) error {
	if r.CallsPerMinute == 0 {
		return nil
	}
	minute := now.Unix() / 60
	key := "mcp/quotas/" + r.ID
	for attempts := 0; attempts < 64; attempts++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var window quotaWindow
		revision := int64(0)
		setting, err := m.Store.GetSetting(ctx, key)
		if err == nil {
			revision = setting.Revision
			if json.Unmarshal(setting.Value, &window) != nil || window.Used < 0 {
				return fmt.Errorf("MCP connection quota state unavailable")
			}
		} else if !errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("MCP connection quota storage unavailable")
		}
		// A backwards clock must not grant an extra window.
		if minute > window.Minute {
			window = quotaWindow{Minute: minute}
		}
		if window.Used >= r.CallsPerMinute {
			return fmt.Errorf("%w: MCP connection minute quota exhausted", tools.ErrRateLimited)
		}
		window.Used++
		raw, err := json.Marshal(window)
		if err != nil {
			return err
		}
		_, err = m.Store.PutSetting(ctx, key, raw, revision, "mcp-quota")
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("MCP connection quota storage unavailable")
		}
		return nil
	}
	return fmt.Errorf("%w: MCP connection quota busy; no remote call dispatched", tools.ErrRateLimited)
}
