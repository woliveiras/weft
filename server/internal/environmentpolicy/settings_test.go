package environmentpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestNetworkSettingsValidation(t *testing.T) {
	for _, n := range []NetworkSettings{{}, {SetupNetworks: []string{"none"}}, {SetupNetworks: []string{"none", "none"}, RunNetworks: []string{"none"}}, {SetupNetworks: []string{"host"}, RunNetworks: []string{"none"}}, {SetupNetworks: []string{"none"}, RunNetworks: []string{"egress"}, EgressHosts: []string{"*.example.com"}}} {
		if n.Validate() == nil {
			t.Fatalf("accepted %+v", n)
		}
	}
	if err := (NetworkSettings{SetupNetworks: []string{"none", "egress"}, RunNetworks: []string{"none"}, EgressHosts: []string{"example.com"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestSettingsPersistenceAndConcurrency(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s := Settings{Store: store, Base: Policy{DisableSetup: true}}
	v, err := s.Get(ctx)
	if err != nil || v.Revision != 0 || v.Source != "server" {
		t.Fatal(v, err)
	}
	n := NetworkSettings{SetupNetworks: []string{"egress"}, RunNetworks: []string{"none"}, EgressHosts: []string{"example.com"}}
	saved, err := s.Save(ctx, n, 0, "admin")
	if err != nil || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
	if _, err = s.Save(ctx, n, 0, "other"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale write accepted", err)
	}
	// Another controller uses persisted network settings while preserving its
	// startup feature switches. No local cache or restart is needed.
	other := Settings{Store: store, Base: Policy{DisableSetup: true}}
	effective, err := other.Effective(ctx)
	if err != nil || !effective.DisableSetup || effective.SetupNetworks[0] != "egress" {
		t.Fatal(effective, err)
	}
	if _, err = other.Preview(ctx, n, 0); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale preview", err)
	}
	got, err := other.Get(ctx)
	if err != nil || got.UpdatedBy != "admin" || got.Source != "installation" {
		t.Fatal(got, err)
	}
}

type brokenSettingsStore struct{ storage.Store }

func (brokenSettingsStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, errors.New("storage offline")
}
func TestSettingsFailClosed(t *testing.T) {
	if _, err := (Settings{Store: brokenSettingsStore{}}).Effective(context.Background()); err == nil {
		t.Fatal("storage failure fell back to YAML")
	}
	for _, body := range []string{"{", `{}`, `{"setupNetworks":["bridge"],"runNetworks":["host"]}`} {
		store, err := fsstore.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PutSetting(context.Background(), NetworkSettingsKey, []byte(body), 0, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err = (Settings{Store: store}).Effective(context.Background()); err == nil {
			t.Fatal("corrupt setting fell back to YAML")
		}
	}
}

type failedWriteStore struct{ storage.Store }

func (failedWriteStore) PutSetting(context.Context, string, []byte, int64, string) (int64, error) {
	return 0, errors.New("write unavailable")
}
func TestSettingsWriteFailure(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := Settings{Store: failedWriteStore{store}}
	n := NetworkSettings{SetupNetworks: []string{"none"}, RunNetworks: []string{"none"}}
	if _, err := s.Save(context.Background(), n, 0, "admin"); err == nil {
		t.Fatal("write failure hidden")
	}
	v, err := s.Get(context.Background())
	if err != nil || v.Revision != 0 || v.Source != "server" {
		t.Fatal(v, err)
	}
}
