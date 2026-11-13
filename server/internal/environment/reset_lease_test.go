package environment

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type renewalFailureStore struct {
	storage.Store
	remaining int
}

func (s *renewalFailureStore) AcquireLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	if strings.HasPrefix(holder, resetLeaseHolderPrefix) {
		leases, err := s.Store.ListLeases(ctx, key)
		if err != nil {
			return false, err
		}
		for _, lease := range leases {
			if lease.Key == key && lease.Holder == holder {
				if s.remaining != 0 {
					s.remaining--
					return false, errors.New("test store unavailable")
				}
			}
		}
	}
	return s.Store.AcquireLease(ctx, key, holder, ttl)
}

func TestResetLeaseRenewalAndLossIntegration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
	}{
		{"renew", 0},
		{"transient", 1},
		{"lose", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := capacityHarness(t)
			var log bytes.Buffer
			h.m.opt.Log = slog.New(slog.NewTextHandler(&log, nil))
			h.m.resetLeaseTTL = 450 * time.Millisecond
			h.m.opt.Store = &renewalFailureStore{Store: h.store, remaining: tc.failures}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			g := graphWith(goNodeEnv())
			id := g.Metadata.ID
			if err := h.m.save(ctx, &Status{Graph: id, State: StateReady, Image: "existing"}); err != nil {
				t.Fatal(err)
			}
			h.docker.set(t, "hold-ps", "")
			done := make(chan struct{})
			var resetErr error
			go func() {
				defer close(done)
				resetErr = h.m.Reset(ctx, id, "test")
			}()
			assertRenewalWarning := func() {
				t.Helper()
				want := 0
				if tc.failures != 0 {
					want = 1
				}
				if count := strings.Count(log.String(), "environment reset lease renewal failed"); count != want ||
					(want == 1 && !strings.Contains(log.String(), "graph="+id)) {
					t.Fatalf("expected %d renewal warnings for %s, got %s", want, id, log.String())
				}
			}
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
				}
			}()
			for {
				if _, err := os.Stat(filepath.Join(h.docker.dir, "held-ps")); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("reset did not reach Docker")
				case <-time.After(10 * time.Millisecond):
				}
			}
			if tc.failures < 0 {
				<-done
				assertRenewalWarning()
				err := resetErr
				if !errors.Is(err, ErrResetLeaseLost) {
					t.Fatalf("reset after renewal error: %v", err)
				}
				status, err := h.m.load(ctx, id)
				if err != nil || status.State != StateReady {
					t.Fatalf("reset changed status after losing its lease: %+v, %v", status, err)
				}
				for _, call := range h.docker.calls(t) {
					if strings.HasPrefix(call, "rm ") || strings.HasPrefix(call, "rmi ") {
						t.Fatalf("reset mutated resources after losing its lease: %s", call)
					}
				}
				return
			}
			time.Sleep(2 * h.m.resetLeaseTTL)
			if _, err := h.m.Build(ctx, g, true, "test"); !errors.Is(err, ErrResetRunning) {
				t.Fatalf("build after initial lease expiry: %v", err)
			}
			if err := h.m.Reset(ctx, id, "test"); !errors.Is(err, ErrResetRunning) {
				t.Fatalf("concurrent reset: %v", err)
			}
			if err := os.Remove(filepath.Join(h.docker.dir, "hold-ps")); err != nil {
				t.Fatal(err)
			}
			<-done
			assertRenewalWarning()
			if resetErr != nil {
				t.Fatal(resetErr)
			}
			if _, err := h.m.Build(ctx, g, true, "test"); err != nil {
				t.Fatalf("build after reset: %v", err)
			}
			if v := h.wait(t, g); v.State != StateReady {
				t.Fatalf("build after reset: %+v", v)
			}
		})
	}
}

func TestBuildSavedRejectsDeletedGraph(t *testing.T) {
	h := capacityHarness(t)
	g := graphWith(goNodeEnv())
	if _, err := h.m.BuildSaved(t.Context(), g, true, "test"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("stale graph build: %v", err)
	}
	if calls := h.docker.calls(t); len(calls) != 0 {
		t.Fatalf("stale graph build touched Docker: %v", calls)
	}
}

func TestEnvironmentLeaseDistinguishesLegacyRecoveryAndBuildHolders(t *testing.T) {
	for _, tc := range []struct {
		holder string
		want   error
	}{
		{"environment-recovery_legacy", ErrEnvironmentRecovering},
		{"environment-reset_current", ErrResetRunning},
		{"weftd", ErrBuildRunning},
	} {
		t.Run(tc.holder, func(t *testing.T) {
			h := capacityHarness(t)
			key := leasePrefix + "bookaneer"
			if ok, err := h.store.AcquireLease(t.Context(), key, tc.holder, time.Minute); err != nil || !ok {
				t.Fatalf("seed lease: %v, %v", ok, err)
			}
			if _, err := h.m.Build(t.Context(), graphWith(goNodeEnv()), true, "test"); !errors.Is(err, tc.want) {
				t.Fatalf("conflict = %v, want %v", err, tc.want)
			}
			if err := h.m.Reset(t.Context(), "bookaneer", "test"); !errors.Is(err, tc.want) {
				t.Fatalf("reset conflict = %v, want %v", err, tc.want)
			}
		})
	}
}

type releaseFailureStore struct{ storage.Store }

func (s releaseFailureStore) KVPut(context.Context, string, string, []byte) error {
	return errors.New("capacity store unavailable")
}

func TestResetCapacityStoreFailureIsNotDockerCleanupConflict(t *testing.T) {
	h := capacityHarness(t)
	h.m.opt.Capacity.Store = releaseFailureStore{h.store}
	err := h.m.Reset(t.Context(), "bookaneer", "test")
	if err == nil || errors.Is(err, ErrResetIncomplete) || !strings.Contains(err.Error(), "capacity store unavailable") {
		t.Fatalf("capacity storage error = %v", err)
	}
}

func TestForcedDeletionFailureRollsBackReservation(t *testing.T) {
	h := capacityHarness(t)
	ctx := t.Context()
	g := graphWith(goNodeEnv())
	id := g.Metadata.ID
	if _, err := h.store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	if err := h.m.save(ctx, &Status{Graph: id, State: StateReady}); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("delete failed")
	for _, failure := range []error{injected, context.DeadlineExceeded} {
		err := h.m.ResetBeforeDelete(ctx, id, "admin", true, "Daemon unavailable", func(context.Context) error { return nil }, func(context.Context) error { return failure })
		if !errors.Is(err, failure) {
			t.Fatalf("delete error: %v", err)
		}
		if _, err := h.store.KVGet(ctx, domain.DeferredEnvironmentCleanup, id); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("reservation still present: %v", err)
		}
	}
	events, err := h.store.ListEvents(ctx, storage.EventFilter{GraphID: id, Types: []domain.EventType{"environment.graph_delete_force_started", "environment.graph_delete_forced"}})
	if err != nil || len(events) != 4 {
		t.Fatalf("attempt and result audit: %v, %v", events, err)
	}
	for _, event := range events {
		if event.Type == "environment.graph_delete_forced" && !strings.Contains(string(event.Data), `"outcome":"failed"`) {
			t.Fatalf("unexpected result: %s", event.Data)
		}
	}
}

func TestRecoveryPersistsLastObservedBuildTime(t *testing.T) {
	h := setup(t)
	ctx := t.Context()
	g := graphWith(goNodeEnv())
	id := g.Metadata.ID
	started, progress := time.Now().UTC().Add(-2*time.Minute), time.Now().UTC().Add(-time.Minute)
	if err := h.m.save(ctx, &Status{Graph: id, State: StateBuilding, StartedAt: &started, LastProgressAt: &progress}); err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Get(ctx, g)
	if err != nil || view.State != StateFailed || view.FinishedAt == nil || !view.FinishedAt.Equal(progress) {
		t.Fatalf("interrupted view: %+v, %v", view, err)
	}
	if st, err := h.m.load(ctx, id); err != nil || st.State != StateBuilding {
		t.Fatalf("GET changed state: %+v, %v", st, err)
	}
	if err := h.m.recoverBuilds(ctx); err != nil {
		t.Fatal(err)
	}
	if st, err := h.m.load(ctx, id); err != nil || st.State != StateFailed || st.FinishedAt == nil || !st.FinishedAt.Equal(progress) {
		t.Fatalf("persisted recovery: %+v, %v", st, err)
	}
}
