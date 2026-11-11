package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/runtime/echo"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

type reviewRuntime struct {
	*echo.Runtime
	calls atomic.Int32
}

func (r *reviewRuntime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	if in.NodeID != "worker" {
		return r.Runtime.Turn(ctx, in, env)
	}
	if r.calls.Add(1) == 1 {
		result, e := env.InvokeTool(ctx, "review.request", json.RawMessage(`{"title":"Review code","instructions":"Inspect the candidate"}`))
		if e != nil {
			return nil, e
		}
		if result.IsError || !result.Yield {
			return nil, fmt.Errorf("review tool did not yield: %s", result.Content)
		}
		if e = env.Emit(ctx, runtime.Emit{Type: "done"}); e == nil {
			return nil, fmt.Errorf("emission accepted after review gate")
		}
		// Even a runtime which ignores the yield cannot publish or complete.
		return &runtime.TurnResult{Emits: []runtime.Emit{{Type: "done", Payload: json.RawMessage(`{}`)}}, Signal: runtime.SignalConfirmStop}, nil
	}
	return &runtime.TurnResult{Summary: "continued after review", Emits: []runtime.Emit{{Type: "done", Payload: json.RawMessage(`{}`)}}}, nil
}
func TestDurableReviewBlocksAndResumesEngine(t *testing.T) {
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			testReviewDecision(t, decision)
		})
	}
}
func testReviewDecision(t *testing.T, decision string) {
	dir := t.TempDir()
	store := openStore(t, dir)
	m := review.New(review.Options{Store: store, Root: filepath.Join(dir, "reviews"), Instance: "reviews"})
	rt := &reviewRuntime{Runtime: echo.New()}
	rig := newRig(t, store, memory.New(), dir, "review-engine", func(opt *engine.Options) {
		opt.Reviews = m
		reg := tools.Builtin(tools.BuiltinConfig{})
		reg.MustRegister(m.Tool())
		opt.Tools = gwAdapter{reg, tools.NewGateway(reg, opt.Approvals, opt.Bus)}
		opt.Runtimes[domain.RuntimeEcho] = rt
		opt.Sandboxes = func(ctx context.Context, s engine.SandboxSpec) (tools.Sandbox, error) {
			return &tools.ProcessSandbox{Root: s.Workdir}, nil
		}
	})
	go m.Run(rig.ctx)
	rig.deploy(`apiVersion: weft.io/v1
kind: Graph
metadata: {name: review}
spec:
  nodes:
    - id: worker
      runtime: {kind: echo}
      isolation: {mode: docker}
      tools: [review.request]
      loop: {mode: event}
    - id: sink
      runtime: {kind: echo}
      loop: {mode: event}
  channels:
    - {id: input, to: [worker]}
    - {id: output, from: worker, to: [sink]}
`)
	rig.inject("review", "input", "job", map[string]string{"task": "update"})
	var pending *review.Record
	eventually(t, 5*time.Second, "durable gate", func() bool {
		rs, e := m.List(rig.ctx)
		if e == nil && len(rs) == 1 {
			pending = rs[0]
			return true
		}
		return false
	})
	eventually(t, 5*time.Second, "input ack without waiting for reviewer", func() bool { return rig.acked("review", "input") == 1 })
	if rt.calls.Load() != 1 {
		t.Fatal("agent continued before decision")
	}
	msgs, e := store.ListMessages(rig.ctx, storage.MessageFilter{GraphID: "review", Channel: "output"})
	if e != nil || len(msgs) != 0 {
		t.Fatal("runtime bypassed gate", msgs, e)
	}
	if _, e = m.Decide(rig.ctx, pending.ID, decision, "without_execution", "operator", "diff reviewed", int64(pending.Generation)); e != nil {
		t.Fatal(e)
	}
	if decision == "rejected" {
		eventually(t, 5*time.Second, "terminal decision delivered", func() bool { r, err := m.Get(rig.ctx, pending.ID); return err == nil && r.Notified })
		time.Sleep(250 * time.Millisecond)
		if rt.calls.Load() != 1 {
			t.Fatal("rejected candidate continued")
		}
		msgs, err := store.ListMessages(rig.ctx, storage.MessageFilter{GraphID: "review", Channel: "output"})
		if err != nil || len(msgs) != 0 {
			t.Fatal("rejected candidate emitted", msgs, err)
		}
		return
	}
	eventually(t, 5*time.Second, "continuation consumed", func() bool { return rt.calls.Load() == 2 })
	eventually(t, 5*time.Second, "downstream output", func() bool {
		ms, e := store.ListMessages(rig.ctx, storage.MessageFilter{GraphID: "review", Channel: "output"})
		return e == nil && len(ms) > 0
	})
}
