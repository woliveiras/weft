package environment

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestDecodeDeferredCleanupRequiresMatchingGraphAndDaemon(t *testing.T) {
	for _, tc := range []struct {
		name, record string
		valid        bool
	}{
		{"valid", `{"graph":"g","daemon":"fingerprint"}`, true},
		{"bad JSON", "{", false},
		{"missing daemon", `{"graph":"g"}`, false},
		{"different graph", `{"graph":"other","daemon":"fingerprint"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeDeferredCleanup("g", []byte(tc.record))
			if tc.valid {
				if err != nil || got.Graph != "g" || got.Daemon != "fingerprint" {
					t.Fatalf("valid record: %+v %v", got, err)
				}
			} else if err == nil {
				t.Fatalf("accepted invalid record: %+v", got)
			}
		})
	}
}

type rejectDeferredCleanupAudit struct {
	storage.Store
	err error
}

func (s rejectDeferredCleanupAudit) AppendEvent(context.Context, *domain.Event) error {
	return s.err
}

func TestInvalidDeferredCleanupReservationSurvivesAuditFailure(t *testing.T) {
	h := setup(t)
	ctx := t.Context()
	id := "invalid"
	row := []byte("{")
	if err := h.store.KVPut(ctx, domain.DeferredEnvironmentCleanup, id, row); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("audit store unavailable")
	h.m.opt.Store = rejectDeferredCleanupAudit{Store: h.store, err: failure}
	if err := h.m.AbandonDeferredCleanup(ctx, id, "admin", "verified resources"); !errors.Is(err, failure) {
		t.Fatalf("abandon with failed audit: %v", err)
	}
	if got, err := h.store.KVGet(ctx, domain.DeferredEnvironmentCleanup, id); err != nil || !bytes.Equal(got, row) {
		t.Fatalf("reservation was released or changed: %q %v", got, err)
	}
}
