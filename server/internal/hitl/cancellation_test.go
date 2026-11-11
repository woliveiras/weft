package hitl

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

type failingDecision struct{ storage.Store }

func (failingDecision) DecideApproval(context.Context, string, domain.ApprovalStatus, string, string, string) (*domain.Approval, error) {
	return nil, errors.New("write failed")
}
func TestCancelPendingFailure(t *testing.T) {
	s := &Service{store: failingDecision{}}
	if _, e := s.CancelPending(context.Background(), "a"); e == nil || e.Error() != "write failed" {
		t.Fatal(e)
	}
}
func TestCancelledWaitExpiresWithoutTimeoutApproval(t *testing.T) {
	ctx := context.Background()
	store, e := sqlstore.OpenSQLite(ctx, filepath.Join(t.TempDir(), "a.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(store, events.NewBus(store, log), log)
	waiting, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, e := s.RequestApproval(waiting, &domain.Approval{ID: "pending", Detail: json.RawMessage(`{"onTimeout":"approve"}`)})
		done <- e
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, e := store.GetApproval(ctx, "pending"); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("approval not created")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("wait stuck")
	}
	a, e := store.GetApproval(ctx, "pending")
	if e != nil || a.Status != domain.ApprovalExpired || a.DecidedBy != "system:cancel" {
		t.Fatal(a, e)
	}
	if _, e = s.Decide(ctx, a.ID, true, "operator", "", ""); !errors.Is(e, ErrNotPending) {
		t.Fatal("late approval accepted", e)
	}
	if _, e = s.CancelPending(ctx, a.ID); e != nil {
		t.Fatal(e)
	}
}
