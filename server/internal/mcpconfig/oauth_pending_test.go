package mcpconfig

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

type pendingFailureStore struct {
	storage.Store
	readErr, writeErr error
	writes            int
}

func (s *pendingFailureStore) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.Store.GetSetting(ctx, key)
}
func (s *pendingFailureStore) PutSetting(ctx context.Context, key string, value []byte, rev int64, by string) (int64, error) {
	s.writes++
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return s.Store.PutSetting(ctx, key, value, rev, by)
}
func TestPendingStorageFailuresAreClosedAndSanitized(t *testing.T) {
	for _, mode := range []string{"read", "write", "conflict", "no-key", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			m := testManager(t)
			store := &pendingFailureStore{Store: m.Store}
			m.Store = store
			switch mode {
			case "read":
				store.readErr = errors.New("synthetic-secret")
			case "write":
				store.writeErr = errors.New("synthetic-secret")
			case "conflict":
				store.writeErr = storage.ErrConflict
			case "no-key":
				m.Box = nil
			case "corrupt":
				if _, err := store.PutSetting(t.Context(), oauthPendingKey, []byte("synthetic-secret"), 0, "test"); err != nil {
					t.Fatal(err)
				}
			}
			err := m.updatePending(t.Context(), func(entries map[string]oauthPending) error {
				entries[nonce()] = oauthPending{Expires: time.Now().Add(time.Minute)}
				return nil
			})
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("unsafe failure: %v", err)
			}
			if mode == "conflict" && store.writes != 16 {
				t.Fatal("unbounded retries", store.writes)
			}
		})
	}
}

func TestPendingTransactionsAcrossControllersAndRestart(t *testing.T) {
	m, f := oauthSetup(t)
	restarted := func() *Manager {
		next := New(m.Store, m.Box, nil)
		next.OAuthClient = f.server.Client()
		next.Discover = m.Discover
		return next
	}
	start, err := m.StartOAuth(t.Context(), "team", "admin-session", "http://localhost/callback")
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := m.loadPending(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := m.Store.GetSetting(t.Context(), oauthPendingKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{start.State, start.Binding, entries[start.State].Verifier, "admin-session"} {
		if strings.Contains(string(saved.Value), sensitive) {
			t.Fatal("transaction secret persisted in plaintext")
		}
	}
	// An unrelated browser or session cannot consume the legitimate transaction.
	if restarted().CaptureOAuth(start.State, "wrong-binding", "test-code", "", "") == nil {
		t.Fatal("binding bypass")
	}
	if err = restarted().CaptureOAuth(start.State, start.Binding, "test-code", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted().CompleteOAuth(t.Context(), start.State, start.Binding, "wrong-session"); err == nil {
		t.Fatal("session bypass")
	}
	saved, err = m.Store.GetSetting(t.Context(), oauthPendingKey)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved.Value), "test-code") {
		t.Fatal("authorization code not encrypted")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := restarted().CompleteOAuth(t.Context(), start.State, start.Binding, "admin-session")
			if e == nil {
				if v.OAuth.Status != "authorized" {
					t.Error("authorization missing")
				}
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("transaction was not consumed exactly once", successes.Load())
	}
	entries, _, err = restarted().loadPending(t.Context())
	if err != nil || len(entries) != 0 {
		t.Fatal("consumed state retained", err)
	}
}

func TestPendingGlobalCapacityAndExpiry(t *testing.T) {
	m, _ := oauthSetup(t)
	if err := m.updatePending(t.Context(), func(entries map[string]oauthPending) error {
		for range maxOAuthPending {
			entries[nonce()] = oauthPending{Expires: time.Now().Add(time.Minute)}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	next := New(m.Store, m.Box, nil)
	next.OAuthClient = m.OAuthClient
	if _, err := next.StartOAuth(t.Context(), "team", "actor", "http://localhost/callback"); err == nil {
		t.Fatal("shared capacity exceeded")
	}
	if err := m.updatePending(t.Context(), func(entries map[string]oauthPending) error {
		for k, p := range entries {
			p.Expires = time.Now().Add(-time.Second)
			entries[k] = p
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := next.StartOAuth(t.Context(), "team", "actor", "http://localhost/callback"); err != nil {
		t.Fatal(err)
	}
	entries, _, err := m.loadPending(t.Context())
	if err != nil || len(entries) != 1 {
		t.Fatal("expired entries not reclaimed", err)
	}
}

func TestPendingIndependentSQLManagers(t *testing.T) {
	m, f := oauthSetup(t)
	path := filepath.Join(t.TempDir(), "oauth.db")
	first, err := sqlstore.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	connection, err := m.Store.GetSetting(t.Context(), prefix+"team")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.PutSetting(t.Context(), connection.Key, connection.Value, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	m.Store = first
	start, err := m.StartOAuth(t.Context(), "team", "session", "http://localhost/callback")
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := sqlstore.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	other, err := sqlstore.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	a := New(second, m.Box, nil)
	b := New(other, m.Box, nil)
	var captures atomic.Int32
	var wg sync.WaitGroup
	for _, manager := range []*Manager{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if manager.CaptureOAuth(start.State, start.Binding, "test-code", "", "") == nil {
				captures.Add(1)
			}
		}()
	}
	wg.Wait()
	if captures.Load() != 1 {
		t.Fatal("callback replay across storage connections", captures.Load())
	}
	b.OAuthClient = f.server.Client()
	b.Discover = m.Discover
	if _, err = b.CompleteOAuth(t.Context(), start.State, start.Binding, "session"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.CompleteOAuth(t.Context(), start.State, start.Binding, "session"); err == nil {
		t.Fatal("replayed consumed authorization")
	}
}
