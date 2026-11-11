package executioncontrol

import (
	"context"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"testing"
	"time"
)

type brokenStore struct{ storage.Store }

func (brokenStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	return nil, errors.New("unavailable")
}
func TestWatchFailsClosed(t *testing.T) {
	m := Manager{Store: brokenStore{}}
	if !errors.Is(m.Check(context.Background(), "x"), ErrUnavailable) {
		t.Fatal("failure bypass")
	}
	ctx, stop := m.Watch(context.Background(), "x")
	defer stop()
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrUnavailable) {
			t.Fatal(context.Cause(ctx))
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not cancel")
	}
	if _, err := m.Cancel(context.Background(), "x", "invalid", "a"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestFamily(t *testing.T) {
	a := &domain.Execution{GraphID: "g", RunID: "r"}
	for _, b := range []*domain.Execution{{GraphID: "other", RunID: "r"}, {GraphID: "g", RunID: "other"}} {
		if family(a, b) {
			t.Fatal("cross-family accepted")
		}
	}
	if !family(a, a) {
		t.Fatal("same family rejected")
	}
}

// A retry after acknowledgement must not advertise pending cancellation.
type terminalStore struct{ storage.Store }

func (terminalStore) GetExecution(context.Context, string) (*domain.Execution, error) {
	return &domain.Execution{ID: "root", GraphID: "g", RunID: "r", Status: domain.ExecCancelled}, nil
}
func (terminalStore) ListExecutions(context.Context, storage.ExecutionFilter) ([]*domain.Execution, error) {
	return nil, nil
}
func (terminalStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	return &storage.Setting{Value: []byte(`{"executionId":"root","rootExecutionId":"root","scope":"tree","actor":"first"}`)}, nil
}
func TestCancellationRetryReportsTerminalWithoutWriting(t *testing.T) {
	result, err := (Manager{Store: terminalStore{}}).Cancel(context.Background(), "root", "tree", "second")
	if err != nil || !result.AlreadyTerminal || result.Created || result.Request.Actor != "first" {
		t.Fatal(result, err)
	}
}
