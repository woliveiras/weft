package engine_test

import (
	"context"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
	"testing"
	"time"
)

type stoppedRuntime struct {
	instructionRuntime
	started chan string
	results chan []error
}

func (r stoppedRuntime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	r.started <- in.ExecutionID
	<-ctx.Done()
	// A buggy external caller can return success and use independent contexts.
	_, toolErr := env.InvokeTool(context.Background(), "filesystem.write", []byte(`{}`))
	_, chatErr := env.Chat(context.Background(), &llm.ChatRequest{})
	_, readErr := env.StateGet(context.Background(), "late")
	_, askErr := env.AskHuman(context.Background(), "late approval")
	r.results <- []error{toolErr, chatErr, askErr, readErr, env.StatePut(context.Background(), "late", []byte("write")), env.Emit(context.Background(), runtime.Emit{})}
	return &runtime.TurnResult{Summary: "must not be successful"}, nil
}
func TestCancelledExecutionRejectsLateCallbacksAndSuccess(t *testing.T) {
	dir := t.TempDir()
	rt := stoppedRuntime{started: make(chan string, 1), results: make(chan []error, 1)}
	r := newRig(t, openStore(t, dir), memory.New(), dir, "cancel", func(o *engine.Options) { o.Runtimes[domain.RuntimeEcho] = rt })
	r.deploy("metadata: {name: stopped}\nspec:\n  nodes:\n    - {id: worker, runtime: {kind: echo}, loop: {mode: once}}\n")
	var id string
	select {
	case id = <-rt.started:
	case <-time.After(5 * time.Second):
		t.Fatal("not started")
	}
	if _, err := (executioncontrol.Manager{Store: r.store}).Cancel(r.ctx, id, "tree", "operator"); err != nil {
		t.Fatal(err)
	}
	select {
	case results := <-rt.results:
		for _, err := range results {
			if !errors.Is(err, executioncontrol.ErrRequested) {
				t.Fatal("late callback not cancelled", err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not cancelled")
	}
	eventually(t, time.Second, "cancelled outcome", func() bool {
		x, e := r.store.GetExecution(r.ctx, id)
		return e == nil && x.Status == domain.ExecCancelled && x.Summary == ""
	})
	if _, e := r.store.KVGet(r.ctx, "stopped/worker", "late"); e == nil {
		t.Fatal("late state write")
	}
}
