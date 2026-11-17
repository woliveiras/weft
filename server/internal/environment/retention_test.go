package environment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

func TestRetentionPolicyValidation(t *testing.T) {
	for _, p := range []RetentionPolicy{{1, 1}, {90, 10000}, {7, 1000}} {
		if err := p.Validate(); err != nil {
			t.Fatal(p, err)
		}
	}
	for _, p := range []RetentionPolicy{{0, 100}, {91, 100}, {7, 0}, {7, 10001}, {-1, 3}} {
		if err := p.Validate(); err == nil {
			t.Fatal(p)
		}
	}
}
func TestRetentionSettingsIntegration(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	v, err := h.m.Retention(ctx)
	if err != nil || v.Revision != 0 || v.Policy != (RetentionPolicy{7, 1000}) {
		t.Fatal(v, err)
	}
	now := time.Now().UTC().Add(-time.Second)
	rows := []BlockedRequest{}
	for i := 0; i < 4; i++ {
		rows = append(rows, BlockedRequest{ProxyRequest: ProxyRequest{Time: now.Add(-time.Duration(i) * 24 * time.Hour), Outcome: "denied"}})
	}
	if _, err := h.m.updateBlocked(ctx, "g", rows, true); err != nil {
		t.Fatal(err)
	}
	v, err = h.m.SaveRetention(ctx, RetentionPolicy{2, 1}, 0, "admin")
	if err != nil || v.Revision != 1 || v.UpdatedBy != "admin" {
		t.Fatal(v, err)
	}
	restored := &Manager{opt: h.m.opt}
	loaded, err := restored.Retention(ctx)
	if err != nil || loaded.Policy != v.Policy || loaded.Revision != 1 {
		t.Fatal(loaded, err)
	}
	history, err := restored.BlockedHistory(ctx, "g")
	if err != nil || len(history.Requests) != 1 || history.RetentionDays != 2 || history.Limit != 1 || !history.Requests[0].Time.Equal(now) {
		t.Fatal(history, err)
	}
	if _, err := h.m.SaveRetention(ctx, RetentionPolicy{8, 2000}, 0, "other"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := h.m.SaveRetention(ctx, RetentionPolicy{0, 0}, 1, "admin"); err == nil {
		t.Fatal("invalid setting accepted")
	}
	if _, err := h.m.SaveRetention(ctx, RetentionPolicy{7, 1000}, -1, "admin"); err == nil {
		t.Fatal("negative revision accepted")
	}
	if _, err := h.m.SaveRetention(ctx, RetentionPolicy{90, 10000}, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	history, err = h.m.BlockedHistory(ctx, "g")
	if err != nil || len(history.Requests) != 1 {
		t.Fatal("discarded history restored", history, err)
	}
	// Existing records older than a new age limit are pruned, even below the count cap.
	rows[0].Time = now.Add(-4 * 24 * time.Hour)
	if _, err := h.m.updateBlocked(ctx, "old", rows[:1], true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.SaveRetention(ctx, RetentionPolicy{1, 100}, 2, "admin"); err != nil {
		t.Fatal(err)
	}
	history, err = h.m.BlockedHistory(ctx, "old")
	if err != nil || len(history.Requests) != 0 {
		t.Fatal(history, err)
	}
}
func TestRetentionStoredFailures(t *testing.T) {
	for _, raw := range []string{"broken", `{}`, `null`, `{"days":0,"maxEvents":20}`} {
		t.Run(raw, func(t *testing.T) {
			h := setup(t)
			ctx := context.Background()
			if _, err := h.store.PutSetting(ctx, RetentionSettingsKey, []byte(raw), 0, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := h.m.Retention(ctx); err == nil {
				t.Fatal("corruption hidden")
			}
			if _, err := h.m.BlockedHistory(ctx, "g"); err == nil {
				t.Fatal("silently applied defaults")
			}
		})
	}
	h := setup(t)
	ctx := context.Background()
	h.m.opt.Store = historyFailureStore{Store: h.store, failRead: true}
	if _, err := h.m.Retention(ctx); err == nil {
		t.Fatal("read failure hidden")
	}
	h.m.opt.Store = historyFailureStore{Store: h.store}
	if _, err := h.m.SaveRetention(ctx, RetentionPolicy{7, 1000}, 0, ""); err == nil {
		t.Fatal("write failure hidden")
	}
}
func TestCustomRetentionPruning(t *testing.T) {
	now := time.Now().UTC()
	rows := []BlockedRequest{}
	for i := 0; i < 10; i++ {
		rows = append(rows, BlockedRequest{ProxyRequest: ProxyRequest{Time: now.Add(-time.Duration(i) * 12 * time.Hour), Outcome: "denied"}})
	}
	if got := retainBlocked(rows, now, RetentionPolicy{1, 100}); len(got) != 2 {
		t.Fatal(got)
	}
	if got := retainBlocked(rows, now, RetentionPolicy{90, 3}); len(got) != 3 {
		t.Fatal(got)
	}
	// Invalid JSON must not overwrite the stored history during a failed read.
	h := setup(t)
	ctx := context.Background()
	data, _ := json.Marshal(BlockedHistory{Requests: rows})
	if _, err := h.store.PutSetting(ctx, blockedHistoryPrefix+"g", data, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.PutSetting(ctx, RetentionSettingsKey, []byte("broken"), 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.BlockedHistory(ctx, "g"); err == nil {
		t.Fatal("expected failure")
	}
	saved, err := h.store.GetSetting(ctx, blockedHistoryPrefix+"g")
	if err != nil || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
}
