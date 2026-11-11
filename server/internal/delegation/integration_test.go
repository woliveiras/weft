package delegation

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func manager(t *testing.T) Manager {
	t.Helper()
	s, e := sqlstore.OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "settings.db"))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return Manager{s}
}
func TestDurableConfigurationCASAndDisabledProfiles(t *testing.T) {
	m := manager(t)
	ctx := t.Context()
	p, e := m.Policy(ctx)
	if e != nil || p.Revision != 0 || p.Policy.ApprovalMode != "required" {
		t.Fatal(p, e)
	}
	p.Policy.ApprovalMode = "automatic"
	v, e := m.SavePolicy(ctx, p.Policy, 0, "admin")
	if e != nil || v.Revision != 1 {
		t.Fatal(v, e)
	}
	if _, e = m.SavePolicy(ctx, p.Policy, 0, "stale"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal(e)
	}
	loaded, e := (Manager{m.Store}).Policy(ctx)
	if e != nil || loaded != v {
		t.Fatal(loaded, e)
	}
	profile := DefaultProfile()
	profile.ID = "review"
	profile.Instructions = "Review supplied data"
	saved, e := m.SaveProfile(ctx, profile, 0, "admin")
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := m.Select(ctx, "review")
	if e != nil {
		t.Fatal(e)
	}
	profile.Enabled = false
	if _, e = m.SaveProfile(ctx, profile, saved.Revision, "admin"); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Select(ctx, "review"); e == nil {
		t.Fatal("disabled profile selected")
	}
	if !snapshot.Profile.Profile.Enabled || snapshot.Profile.Revision != 1 {
		t.Fatal("snapshot mutated")
	}
	if _, e = m.Select(ctx, "missing"); !errors.Is(e, storage.ErrNotFound) {
		t.Fatal(e)
	}
	all, e := m.Profiles(ctx)
	if e != nil || len(all) != 2 {
		t.Fatal(all, e)
	}
	p.Policy.Enabled = false
	if _, e = m.SavePolicy(ctx, p.Policy, 1, "admin"); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Select(ctx, "analysis"); e == nil {
		t.Fatal("disabled policy accepted")
	}
}
func TestCapacityReductionCountsExistingHighSlots(t *testing.T) {
	m := manager(t)
	ctx := t.Context()
	if ok, e := m.Store.AcquireLease(ctx, "delegation/slot/20", "old", time.Minute); e != nil || !ok {
		t.Fatal(e)
	}
	p := DefaultPolicy()
	p.MaxConcurrentChildren = 2
	if _, e := m.SavePolicy(ctx, p, 0, "admin"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	slots := []string{}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := m.Reserve(ctx, "new", 4, time.Minute)
			if e == nil {
				mu.Lock()
				slots = append(slots, s)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(slots) != 1 {
		t.Fatalf("admitted %d with one existing child under limit 2", len(slots))
	}
	for _, s := range slots {
		if e := m.Store.ReleaseLease(ctx, s, "new"); e != nil {
			t.Fatal(e)
		}
	}
	if e := m.Store.ReleaseLease(ctx, "delegation/slot/20", "old"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Reserve(ctx, "next", 4, time.Minute); e != nil {
		t.Fatal(e)
	}
}

type failingStore struct{ storage.Store }

func (failingStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, errors.New("storage unavailable")
}
func TestConfigurationFailuresDoNotUseDefaults(t *testing.T) {
	m := manager(t)
	broken := Manager{failingStore{m.Store}}
	if _, e := broken.Policy(t.Context()); e == nil {
		t.Fatal("policy failure ignored")
	}
	if _, e := broken.Select(t.Context(), "analysis"); e == nil {
		t.Fatal("selection failure ignored")
	}
	if _, e := broken.Reserve(t.Context(), "child", 4, time.Second); e == nil {
		t.Fatal("admission failure ignored")
	}
	rows, e := m.Store.ListLeases(t.Context(), "delegation/")
	if e != nil || len(rows) != 0 {
		t.Fatal("leaked lease", rows, e)
	}
	if _, e := m.Store.PutSetting(t.Context(), PolicyKey, []byte(`{"enabled":true}`), 0, "test"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Policy(t.Context()); e == nil {
		t.Fatal("corrupt policy ignored")
	}
}
