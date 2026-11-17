package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
)

type callbackFinishingRuntime struct {
	instructionRuntime
	token string
	fail  bool
}

func (r *callbackFinishingRuntime) Turn(_ context.Context, in *runtime.TurnInput, _ runtime.TurnEnv) (*runtime.TurnResult, error) {
	r.token = in.Callback.Token
	if r.fail {
		return nil, errors.New("fixture failure")
	}
	return &runtime.TurnResult{Summary: "done"}, nil
}

type callbackObservingStore struct {
	storage.Store
	observe func(*domain.Execution)
}

func (s callbackObservingStore) PutExecution(ctx context.Context, x *domain.Execution) error {
	if x.Status.Terminal() {
		s.observe(x)
	}
	return s.Store.PutExecution(ctx, x)
}

func TestCallbackRevokedBeforeTerminalStorageIntegration(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			rt := &callbackFinishingRuntime{fail: fail}
			observed := make(chan error, 1)
			var eng *engine.Engine
			store := callbackObservingStore{Store: openStore(t, dir), observe: func(x *domain.Execution) {
				_, _, err := eng.Callback(x.ID, rt.token)
				observed <- err
			}}
			r := newRig(t, store, memory.New(), dir, "callback-lifecycle", func(o *engine.Options) {
				o.CallbackURL = "http://fixture.invalid"
				o.Runtimes[domain.RuntimeEcho] = rt
			})
			eng = r.eng
			r.deploy("metadata: {name: callback-lifecycle}\nspec:\n  nodes:\n    - {id: worker, runtime: {kind: echo}, loop: {mode: once}}\n")
			select {
			case err := <-observed:
				if !errors.Is(err, engine.ErrBadCallback) {
					t.Fatalf("callback still accepted at terminal write: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no terminal write")
			}
		})
	}
}
