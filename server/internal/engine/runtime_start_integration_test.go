package engine_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/runtime/echo"
	"github.com/woliveiras/weft/server/internal/storage"
)

type runtimeStartObserver struct {
	runtime.Runtime
	store storage.Store
	calls atomic.Int64
	errs  chan error
}

func (r *runtimeStartObserver) Turn(ctx context.Context, in *runtime.TurnInput, _ runtime.TurnEnv) (*runtime.TurnResult, error) {
	r.calls.Add(1)
	stored, err := r.store.GetExecution(ctx, in.ExecutionID)
	if err == nil && (stored.RuntimeStartedAt == nil || stored.RuntimeStartedAt.Before(stored.StartedAt) || stored.Status != domain.ExecRunning) {
		err = errors.New("runtime invoked without its durable start marker")
	}
	r.errs <- err
	return &runtime.TurnResult{Summary: "finished immediately"}, nil
}

func TestRuntimeStartPersistedAfterBeforeTurnApproval(t *testing.T) {
	for _, decision := range []string{"approve", "reject", "ungated"} {
		t.Run(decision, func(t *testing.T) {
			dir := t.TempDir()
			store := openStore(t, dir)
			capture := &runtimeStartObserver{Runtime: echo.New(), store: store, errs: make(chan error, 1)}
			r := newRig(t, store, memory.New(), dir, "runtime-start", func(o *engine.Options) {
				o.Runtimes[domain.RuntimeEcho] = capture
			})
			gate := ""
			if decision != "ungated" {
				gate = ", approval: {beforeTurn: true}"
			}
			r.deploy(`metadata: {name: runtime-start}
spec:
  nodes:
    - {id: worker, runtime: {kind: echo}, loop: {mode: once}` + gate + `}
`)
			var executionID string
			if decision != "ungated" {
				var pending []*domain.Approval
				eventually(t, 5*time.Second, "pending before-turn approval", func() bool {
					pending, _ = store.ListApprovals(t.Context(), storage.ApprovalFilter{GraphID: "runtime-start", Status: domain.ApprovalPending})
					return len(pending) == 1
				})
				executionID = pending[0].ExecutionID
				stored, err := store.GetExecution(t.Context(), executionID)
				if err != nil || stored.RuntimeStartedAt != nil || stored.StartedAt.IsZero() || capture.calls.Load() != 0 {
					t.Fatalf("pending approval marked runtime started: %+v, calls=%d, err=%v", stored, capture.calls.Load(), err)
				}
				if _, err := r.apr.Decide(t.Context(), pending[0].ID, decision == "approve", "reviewer", "fixture", ""); err != nil {
					t.Fatal(err)
				}
			}
			if decision == "reject" {
				eventually(t, 5*time.Second, "rejected execution", func() bool {
					stored, _ := store.GetExecution(t.Context(), executionID)
					return stored != nil && stored.Status == domain.ExecCancelled
				})
				stored, err := store.GetExecution(t.Context(), executionID)
				if err != nil || stored.RuntimeStartedAt != nil || capture.calls.Load() != 0 {
					t.Fatalf("rejected approval marked runtime started: %+v, calls=%d, err=%v", stored, capture.calls.Load(), err)
				}
				return
			}
			select {
			case err := <-capture.errs:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("runtime was not called")
			}
			eventually(t, 5*time.Second, "completed fast runtime with persisted start", func() bool {
				executions, _ := store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: "runtime-start"})
				return len(executions) == 1 && executions[0].Status == domain.ExecSucceeded &&
					executions[0].RuntimeStartedAt != nil && executions[0].CompletedAt != nil &&
					!executions[0].CompletedAt.Before(*executions[0].RuntimeStartedAt)
			})
		})
	}
}

type runtimeStartFailureStore struct{ storage.Store }

func (s runtimeStartFailureStore) PutExecution(ctx context.Context, execution *domain.Execution) error {
	if execution.RuntimeStartedAt != nil && execution.Status == domain.ExecRunning {
		return errors.New("runtime admission storage unavailable")
	}
	return s.Store.PutExecution(ctx, execution)
}

func TestRuntimeStartStorageFailureDoesNotInvokeRuntime(t *testing.T) {
	dir := t.TempDir()
	store := runtimeStartFailureStore{Store: openStore(t, dir)}
	capture := &runtimeStartObserver{Runtime: echo.New(), store: store, errs: make(chan error, 1)}
	r := newRig(t, store, memory.New(), dir, "runtime-start-write-failure", func(o *engine.Options) {
		o.Runtimes[domain.RuntimeEcho] = capture
		o.RetryBackoff = 10 * time.Second
	})
	request := &domain.OperatorMessage{
		ID: "runtime-request", GraphID: "runtime-start-write-failure", NodeID: "worker",
		Replica: -1, Text: "Keep this request until the runtime can start", CreatedAt: time.Now().UTC(),
	}
	if err := store.PutOperatorMessage(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	r.deploy(`metadata: {name: runtime-start-write-failure}
spec:
  nodes:
    - {id: worker, runtime: {kind: echo}, loop: {mode: once}}
`)
	eventually(t, 5*time.Second, "execution failed before runtime", func() bool {
		executions, _ := store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: request.GraphID})
		return len(executions) > 0 && executions[0].Status == domain.ExecFailed
	})
	executions, err := store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: request.GraphID})
	if err != nil {
		t.Fatal(err)
	}
	for _, execution := range executions {
		if execution.RuntimeStartedAt != nil || !strings.Contains(execution.Error, "runtime admission unavailable") {
			t.Fatalf("failed runtime admission persisted as started: %+v", execution)
		}
	}
	if capture.calls.Load() != 0 {
		t.Fatal("runtime called after its start marker could not be persisted")
	}
	requests, err := store.ListOperatorMessages(t.Context(), request.GraphID, request.NodeID, 10)
	if err != nil || len(requests) != 1 || requests[0].Consumed != nil {
		t.Fatalf("failed admission consumed its operator request: %+v, %v", requests, err)
	}
}
