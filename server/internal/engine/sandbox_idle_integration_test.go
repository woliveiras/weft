package engine_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	"github.com/woliveiras/weft/server/internal/tools"
)

type idleIntegrationSandbox struct {
	tools.ProcessSandbox
	suspended   atomic.Bool
	stops       atomic.Int32
	resumes     atomic.Int32
	fail        atomic.Bool
	stopFailure atomic.Bool
}

func (s *idleIntegrationSandbox) Suspend(context.Context) error {
	s.stops.Add(1)
	s.suspended.Store(true)
	if s.stopFailure.Load() {
		return errors.New("stop confirmation failed")
	}
	return nil
}
func (s *idleIntegrationSandbox) Start(context.Context) error {
	if s.fail.Load() {
		return errors.New("resume unavailable")
	}
	s.resumes.Add(1)
	s.suspended.Store(false)
	return nil
}

type idleRuntime struct {
	*echo.Runtime
	entered chan struct{}
	release chan struct{}
}

func (r *idleRuntime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.Runtime.Turn(ctx, in, env)
}

func TestEngineSuspendsIdleAndResumesMessages(t *testing.T) {
	dir := t.TempDir()
	sb := &idleIntegrationSandbox{}
	rt := &idleRuntime{Runtime: echo.New(), entered: make(chan struct{}, 10), release: make(chan struct{}, 10)}
	var workdir string
	r := newRig(t, openStore(t, dir), memory.New(), dir, "idle", func(opt *engine.Options) {
		opt.SandboxIdleTimeout = 30 * time.Millisecond
		opt.Runtimes[domain.RuntimeEcho] = rt
		opt.Sandboxes = func(ctx context.Context, s engine.SandboxSpec) (tools.Sandbox, error) {
			workdir = s.Workdir
			if err := os.WriteFile(filepath.Join(s.Workdir, "preserved"), []byte("task data"), 0600); err != nil {
				return nil, err
			}
			return sb, nil
		}
	})
	r.deploy(`apiVersion: weft.io/v1
kind: Graph
metadata: {name: idle}
spec:
  nodes:
    - id: worker
      runtime: {kind: echo}
      loop: {mode: event}
  channels:
    - {id: in, to: [worker]}
`)
	eventually(t, 5*time.Second, "sandbox suspended while awaiting input", func() bool { return sb.suspended.Load() })
	r.inject("idle", "in", "job", map[string]string{"job": "first"})
	select {
	case <-rt.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not resume")
	}
	if sb.suspended.Load() || sb.resumes.Load() != 1 {
		t.Fatal("runtime called before resume")
	}
	// A runtime blocked longer than the timeout must never be suspended.
	stops := sb.stops.Load()
	time.Sleep(100 * time.Millisecond)
	if sb.stops.Load() != stops {
		t.Fatal("suspended an active turn")
	}
	rt.release <- struct{}{}
	eventually(t, 5*time.Second, "input acknowledged", func() bool { return r.acked("idle", "in") == 1 })
	eventually(t, 5*time.Second, "sandbox idle again", func() bool { return sb.stops.Load() > stops })
	data, err := os.ReadFile(filepath.Join(workdir, "preserved"))
	if err != nil || string(data) != "task data" {
		t.Fatalf("workspace lost: %s %v", data, err)
	}
	// Resume failure restores operator input and does not invoke the runtime.
	sb.fail.Store(true)
	msg, err := r.eng.SendOperatorMessage(r.ctx, "idle", "worker", 0, "continue", "test")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "failed resume recorded", func() bool {
		xs, e := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "idle", Limit: 10})
		if e != nil {
			return false
		}
		for _, x := range xs {
			if strings.Contains(x.Error, "resume unavailable") {
				return true
			}
		}
		return false
	})
	select {
	case <-rt.entered:
		t.Fatal("runtime called after failed resume")
	default:
	}
	sb.fail.Store(false)
	select {
	case <-rt.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("operator message was lost")
	}
	rt.release <- struct{}{}
	eventually(t, 5*time.Second, "operator turn succeeded", func() bool {
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "idle", Limit: 20})
		count := 0
		for _, x := range xs {
			if x.Status == domain.ExecSucceeded {
				count++
			}
		}
		return count == 2
	})
	ops, err := r.store.ListOperatorMessages(r.ctx, "idle", "worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range ops {
		if op.ID == msg.ID && op.Consumed != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("operator input not consumed after successful resume")
	}
	evs, err := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "idle", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, ev := range evs {
		if ev.Type == "sandbox.lifecycle" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing lifecycle telemetry")
	}
}

func TestScheduledSandboxSuspendsAndPausedTaskWaitsForResume(t *testing.T) {
	dir := t.TempDir()
	sb := &idleIntegrationSandbox{}
	rt := &idleRuntime{Runtime: echo.New(), entered: make(chan struct{}, 10), release: make(chan struct{}, 10)}
	rt.release <- struct{}{}
	r := newRig(t, openStore(t, dir), memory.New(), dir, "scheduled-idle", func(opt *engine.Options) {
		opt.SandboxIdleTimeout = 30 * time.Millisecond
		opt.Runtimes[domain.RuntimeEcho] = rt
		opt.Sandboxes = func(context.Context, engine.SandboxSpec) (tools.Sandbox, error) { return sb, nil }
	})
	r.deploy(`apiVersion: weft.io/v1
kind: Graph
metadata: {name: scheduled-idle}
spec:
  nodes:
    - id: worker
      runtime: {kind: echo}
      loop: {mode: scheduled, schedule: "@every 1h"}
`)
	select {
	case <-rt.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first scheduled turn missing")
	}
	eventually(t, 5*time.Second, "scheduled wait suspends before next run", func() bool { return sb.suspended.Load() })
	select {
	case <-rt.entered:
		t.Fatal("idle timer advanced the schedule")
	default:
	}
	if err := r.eng.GraphAction(r.ctx, "scheduled-idle", "pause", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.SendOperatorMessage(r.ctx, "scheduled-idle", "worker", 0, "next task", "test"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if sb.resumes.Load() != 0 {
		t.Fatal("paused graph resumed on message")
	}
	queued, err := r.store.ListOperatorMessages(r.ctx, "scheduled-idle", "worker", 10)
	if err != nil || len(queued) != 1 || queued[0].Consumed != nil {
		t.Fatalf("paused input must remain available: %v %v", queued, err)
	}
	if err := r.eng.GraphAction(r.ctx, "scheduled-idle", "resume", "test"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("resume did not deliver queued operator message")
	}
	if sb.resumes.Load() != 1 {
		t.Fatal("sandbox not resumed")
	}
	rt.release <- struct{}{}
}

func TestEngineReconcilesUncertainSuspensionBeforeTurn(t *testing.T) {
	dir := t.TempDir()
	sb := &idleIntegrationSandbox{}
	sb.stopFailure.Store(true)
	r := newRig(t, openStore(t, dir), memory.New(), dir, "uncertain", func(opt *engine.Options) {
		opt.SandboxIdleTimeout = 30 * time.Millisecond
		opt.Sandboxes = func(context.Context, engine.SandboxSpec) (tools.Sandbox, error) { return sb, nil }
	})
	r.deploy(`apiVersion: weft.io/v1
kind: Graph
metadata: {name: uncertain}
spec:
  nodes:
    - {id: worker, runtime: {kind: echo}, loop: {mode: event}}
`)
	eventually(t, 5*time.Second, "suspension fails after stopping", func() bool { return sb.stops.Load() > 0 })
	if _, err := r.eng.SendOperatorMessage(r.ctx, "uncertain", "worker", 0, "continue", "test"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "uncertain sandbox reconciled before turn", func() bool { return sb.resumes.Load() == 1 })
	eventually(t, 5*time.Second, "turn succeeds after reconciliation", func() bool {
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "uncertain", Limit: 10})
		for _, x := range xs {
			if x.Status == domain.ExecSucceeded {
				return true
			}
		}
		return false
	})
}
