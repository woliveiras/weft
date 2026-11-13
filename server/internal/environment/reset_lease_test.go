package environment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
