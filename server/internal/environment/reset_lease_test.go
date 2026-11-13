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
}

func (s renewalFailureStore) AcquireLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	if strings.HasPrefix(holder, resetLeaseHolderPrefix) {
		leases, err := s.Store.ListLeases(ctx, key)
		if err != nil {
			return false, err
		}
		for _, lease := range leases {
			if lease.Key == key && lease.Holder == holder {
				return false, errors.New("test store unavailable")
			}
		}
	}
	return s.Store.AcquireLease(ctx, key, holder, ttl)
}

func TestResetLeaseRenewalAndLossIntegration(t *testing.T) {
	for _, lose := range []bool{false, true} {
		name := "renew"
		if lose {
			name = "lose"
		}
		t.Run(name, func(t *testing.T) {
			h := capacityHarness(t)
			h.m.resetLeaseTTL = 450 * time.Millisecond
			if lose {
				h.m.opt.Store = renewalFailureStore{h.store}
			}
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
			if lose {
				<-done
				err := resetErr
				if !errors.Is(err, ErrResetLeaseLost) || !strings.Contains(err.Error(), "test store unavailable") {
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
