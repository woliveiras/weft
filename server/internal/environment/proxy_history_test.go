//go:build !windows

package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/storage"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRetainBlocked(t *testing.T) {
	now := time.Now().UTC()
	row := BlockedRequest{ProxyRequest: ProxyRequest{Time: now, Outcome: "denied", Host: "example.org"}, Proxy: "one", Phase: "runtime"}
	rows := []BlockedRequest{row, row}
	for _, delta := range []time.Duration{-blockedHistoryAge, -blockedHistoryAge - time.Second, time.Second} {
		r := row
		r.Time = now.Add(delta)
		rows = append(rows, r)
	}
	r := row
	r.Outcome = "allowed"
	rows = append(rows, r)
	if got := retainBlocked(rows, now, RetentionPolicy{7, 1000}); len(got) != 1 {
		t.Fatal(got)
	}
	for i := 0; i < 1200; i++ {
		r := row
		r.Time = now.Add(-time.Duration(i) * time.Second)
		r.EventID = fmt.Sprint(i)
		rows = append(rows, r)
	}
	got := retainBlocked(rows, now, RetentionPolicy{7, 1000})
	if len(got) != 1000 || got[0].Time.Before(got[999].Time) {
		t.Fatal(len(got))
	}
}

func TestBlockedHistoryPersistenceIntegration(t *testing.T) {
	h, dir := proxyFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Second)
	logs := fmt.Sprintf(`{"time":%q,"msg":"egress request","host":"example.org","outcome":"denied","reason":"host_not_allowed"}`, now.Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(dir, "logs"), []byte(logs), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := h.m.CollectBlockedHistory(ctx, "bookaneer"); err != nil {
			t.Fatal(err)
		}
	}
	restarted := &Manager{opt: h.m.opt}
	if err := os.WriteFile(filepath.Join(dir, "fail-list"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := restarted.BlockedHistory(ctx, "bookaneer")
	if err != nil || len(v.Requests) != 1 || v.CollectedAt == nil {
		t.Fatal(v, err)
	}
	if err := restarted.CollectBlockedHistory(ctx, "bookaneer"); err == nil {
		t.Fatal("inventory failure hidden")
	}
	other, err := restarted.BlockedHistory(ctx, "other")
	if err != nil || len(other.Requests) != 0 {
		t.Fatal(other, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := v.Requests[0]
			r.EventID = fmt.Sprint(i)
			if _, err := restarted.updateBlocked(ctx, "bookaneer", []BlockedRequest{r}, true); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	v, err = restarted.BlockedHistory(ctx, "bookaneer")
	if err != nil || len(v.Requests) != 5 {
		t.Fatal(v, err)
	}
	// Expiration is persisted, including when no live proxy remains.
	for i := range v.Requests {
		v.Requests[i].Time = now.Add(-blockedHistoryAge)
	}
	_, err = restarted.updateBlocked(ctx, "expired", v.Requests, true)
	if err != nil {
		t.Fatal(err)
	}
	v, err = restarted.BlockedHistory(ctx, "expired")
	if err != nil || len(v.Requests) != 0 {
		t.Fatal(v, err)
	}
	if _, err := h.store.PutSetting(ctx, blockedHistoryPrefix+"corrupt", []byte("invalid"), 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.BlockedHistory(ctx, "corrupt"); err == nil {
		t.Fatal("corruption hidden")
	}
}

func TestHistoryCollectorRunsWithoutUI(t *testing.T) {
	h, _ := proxyFixture(t)
	if _, err := h.store.SaveGraph(context.Background(), graphWith(goNodeEnv()), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); h.m.RunProxyHistory(ctx) }()
	deadline := time.After(3 * time.Second)
	for {
		v, err := h.m.BlockedHistory(ctx, "bookaneer")
		if err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		if v.CollectedAt != nil {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("collector did not run")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
}

type historyFailureStore struct {
	storage.Store
	failRead bool
	conflict bool
}

func (s historyFailureStore) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if s.failRead {
		return nil, errors.New("read unavailable")
	}
	return s.Store.GetSetting(ctx, key)
}
func (s historyFailureStore) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	if s.conflict {
		return 0, storage.ErrConflict
	}
	return 0, errors.New("write unavailable")
}
func TestHistoryStorageFailures(t *testing.T) {
	for _, mode := range []string{"read", "write", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			h := setup(t)
			h.m.opt.Store = historyFailureStore{Store: h.store, failRead: mode == "read", conflict: mode == "conflict"}
			if _, err := h.m.updateBlocked(context.Background(), "g", nil, true); err == nil {
				t.Fatal("storage failure hidden")
			}
		})
	}
}

func TestHistoryPrunesPersistedExpiredData(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	old := time.Now().Add(-8 * 24 * time.Hour)
	data, err := json.Marshal(BlockedHistory{Requests: []BlockedRequest{{ProxyRequest: ProxyRequest{Time: old, Outcome: "denied"}}}, CollectedAt: &old})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.PutSetting(ctx, blockedHistoryPrefix+"deleted-graph", data, 0, ""); err != nil {
		t.Fatal(err)
	}
	v, err := h.m.BlockedHistory(ctx, "deleted-graph")
	if err != nil || len(v.Requests) != 0 {
		t.Fatal(v, err)
	}
	if _, err := h.store.GetSetting(ctx, blockedHistoryPrefix+"deleted-graph"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("expired document remains", err)
	}
}
