package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const oauthPendingKey = "mcp/oauth-pending"
const maxOAuthPending = 128

// The bounded document is encrypted as a whole, including state, browser/session
// bindings, PKCE verifiers and returned authorization codes. CAS makes capture and
// consumption single-winner across controllers sharing storage and encryption keys.
func (m *Manager) loadPending(ctx context.Context) (map[string]oauthPending, int64, error) {
	entries := map[string]oauthPending{}
	saved, err := m.Store.GetSetting(ctx, oauthPendingKey)
	if errors.Is(err, storage.ErrNotFound) {
		return entries, 0, nil
	}
	if err != nil {
		return nil, 0, oauthError("OAuth transaction storage unavailable")
	}
	raw, err := m.Box.Open(string(saved.Value), oauthPendingKey)
	if err != nil {
		return nil, 0, oauthError("OAuth transaction encryption key unavailable")
	}
	if json.Unmarshal(raw, &entries) != nil || entries == nil || len(entries) > maxOAuthPending {
		return nil, 0, oauthError("OAuth transaction storage invalid")
	}
	return entries, saved.Revision, nil
}

func (m *Manager) updatePending(ctx context.Context, change func(map[string]oauthPending) error) error {
	for attempt := 0; attempt < 16; attempt++ {
		entries, revision, err := m.loadPending(ctx)
		if err != nil {
			return err
		}
		for state, entry := range entries {
			if !time.Now().Before(entry.Expires) {
				delete(entries, state)
			}
		}
		if err = change(entries); err != nil {
			return err
		}
		if revision == 0 && len(entries) == 0 {
			return nil
		}
		if len(entries) > maxOAuthPending {
			return oauthError("Too many pending OAuth authorizations")
		}
		raw, err := json.Marshal(entries)
		if err != nil {
			return oauthError("OAuth transaction serialization failed")
		}
		sealed, err := m.Box.Seal(raw, oauthPendingKey)
		if err != nil {
			return oauthError("OAuth transaction encryption unavailable")
		}
		_, err = m.Store.PutSetting(ctx, oauthPendingKey, []byte(sealed), revision, "system")
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		if err != nil {
			return oauthError("OAuth transaction storage unavailable")
		}
		return nil
	}
	return oauthError("OAuth transactions are busy; retry authorization")
}
