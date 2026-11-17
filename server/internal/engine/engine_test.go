package engine_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/llm"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/runtime/echo"
	"github.com/woliveiras/weft/server/internal/runtime/native"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

type rig struct {
	t      *testing.T
	store  storage.Store
	broker queue.Broker
	bus    *events.Bus
	apr    *hitl.Service
	eng    *engine.Engine
	dir    string
	ctx    context.Context
}

func newRig(t *testing.T, store storage.Store, broker queue.Broker, dir, instance string, configure ...func(*engine.Options)) *rig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(store, log)
	apr := hitl.New(store, bus, log)
	models, err := llm.NewRegistry([]llm.ProviderConfig{{Name: "mock", Type: "mock", DefaultModel: "mock-1"}}, "mock")
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.Builtin(tools.BuiltinConfig{})
	gw := tools.NewGateway(reg, apr, bus)
	ws, err := workspace.NewManager(filepath.Join(dir, "ws"), "", false)
	if err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Instance: instance, Store: store, Broker: broker, Bus: bus, Approvals: apr, Models: models,
		Tools: gwAdapter{reg, gw}, Workspaces: ws,
		Runtimes: map[domain.RuntimeKind]runtime.Runtime{domain.RuntimeEcho: echo.New(), domain.RuntimeNative: native.New()},
		Log:      log, LeaseTTL: 3 * time.Second, ReconcileInterval: 150 * time.Millisecond, RetryBackoff: 50 * time.Millisecond,
	}
	for _, fn := range configure {
		fn(&opt)
	}
	eng, err := engine.New(opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, store: store, broker: broker, bus: bus, apr: apr, eng: eng, dir: dir, ctx: ctx}
	t.Cleanup(func() {
		sctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		eng.Shutdown(sctx)
	})
	return r
}

type gwAdapter struct {
	reg *tools.Registry
	gw  *tools.Gateway
}

func (g gwAdapter) Invoke(ctx context.Context, p *tools.Principal, name string, args json.RawMessage) (*tools.Result, error) {
	return g.gw.Invoke(ctx, p, name, args)
}
func (g gwAdapter) Schemas(p *tools.Principal) []llm.ToolSchema { return g.reg.Schemas(p) }
func (g gwAdapter) ReadGuidanceReference(ctx context.Context, p *tools.Principal, path string, read func() string) (*tools.Result, error) {
	return g.gw.ReadGuidanceReference(ctx, p, path, read)
}
func (g gwAdapter) NeedsApproval(p *tools.Principal, name string) bool {
	t, ok := g.reg.Get(name)
	if !ok {
		return false
	}
	gr, err := tools.Authorize(p, t)
	return err == nil && gr.RequiresApproval
}

func openStore(t *testing.T, dir string) storage.Store {
	t.Helper()
	s, err := sqlstore.OpenSQLite(context.Background(), filepath.Join(dir, "weft.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func (r *rig) deploy(doc string) *domain.Graph {
	r.t.Helper()
	var g domain.Graph
	if err := yaml.Unmarshal([]byte(doc), &g); err != nil {
		r.t.Fatal(err)
	}
	out, _, err := r.eng.SaveGraph(r.ctx, &g, 0, "test")
	if err != nil {
		r.t.Fatalf("save: %v", err)
	}
	if err := r.eng.GraphAction(r.ctx, out.Metadata.ID, "start", "test"); err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *rig) inject(graph, channel, typ string, payload any) *domain.Message {
	r.t.Helper()
	b, _ := json.Marshal(payload)
	m, err := r.eng.Inject(r.ctx, graph, channel, typ, b, "", nil, "test")
	if err != nil {
		r.t.Fatal(err)
	}
	return m
}

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func (r *rig) acked(graph, channel string) int {
	recs, err := r.store.ListMessages(r.ctx, storage.MessageFilter{GraphID: graph, Channel: channel, Status: domain.MsgAcked, Limit: 1000})
	if err != nil {
		r.t.Fatal(err)
	}
	return len(recs)
}

const pipeline = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: pipe}
spec:
  defaults: {runtime: {kind: echo}}
  nodes:
    - {id: a}
    - {id: b, cluster: {replicas: 3}}
    - {id: c}
    - {id: d}
  channels:
    - {id: in, to: [a]}
    - {id: ab, from: a, to: [b]}
    - {id: bc, from: b, type: fanout, to: [c, d]}
`

func TestPipelineCompetingAndFanout(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")
	r.deploy(pipeline)
	const n = 9
	for i := 0; i < n; i++ {
		r.inject("pipe", "in", "job", map[string]int{"i": i})
	}
	eventually(t, 20*time.Second, "all messages through the fan-out", func() bool {
		return r.acked("pipe", "bc") == 2*n // one copy per fan-out target
	})
	if got := r.acked("pipe", "ab"); got != n {
		t.Fatalf("queue ab processed %d times, want exactly %d (competing consumers)", got, n)
	}
	// Work on the clustered node was spread over replicas.
	xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "pipe", NodeID: "b", Limit: 100})
	seen := map[int]bool{}
	for _, x := range xs {
		if x.Status == domain.ExecSucceeded && len(x.InputIDs) > 0 {
			seen[x.Replica] = true
		}
	}
	if len(seen) < 2 {
		t.Logf("note: only %d replica(s) of b did work (scheduling dependent)", len(seen))
	}
	st, err := r.eng.Status(r.ctx, "pipe")
	if err != nil {
		t.Fatal(err)
	}
	if st.DesiredState != domain.GraphRunning || len(st.Nodes) != 4 {
		t.Fatalf("status: %+v", st)
	}
	// Causation chain: a message on bc descends from an injected message.
	recs, _ := r.store.ListMessages(r.ctx, storage.MessageFilter{GraphID: "pipe", Channel: "bc", Limit: 1})
	if recs[0].Message.MetaString(domain.MetaCausationID) == "" || recs[0].Message.Hops() != 2 {
		t.Fatalf("lineage not tracked: %+v", recs[0].Message.Metadata)
	}
}

const gated = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: gated}
spec:
  defaults: {runtime: {kind: echo}}
  nodes:
    - {id: maker}
    - {id: shipper}
  channels:
    - {id: in, to: [maker]}
    - {id: release, from: maker, to: [shipper], approval: true}
`

func TestApprovalGate(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")
	r.deploy(gated)
	r.inject("gated", "in", "build", map[string]string{"v": "1.0"})
	var pending []*domain.Approval
	eventually(t, 10*time.Second, "approval requested", func() bool {
		pending, _ = r.store.ListApprovals(r.ctx, storage.ApprovalFilter{GraphID: "gated", Status: domain.ApprovalPending})
		return len(pending) == 1
	})
	if r.acked("gated", "release") != 0 {
		t.Fatal("message crossed the gate before approval")
	}
	if _, err := r.apr.Decide(r.ctx, pending[0].ID, true, "alice", "ship it", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "approved message delivered", func() bool { return r.acked("gated", "release") == 1 })
	if _, err := r.apr.Decide(r.ctx, pending[0].ID, false, "bob", "", ""); err == nil {
		t.Fatal("second decision should conflict")
	}
}

const failing = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: failing}
spec:
  nodes:
    - {id: broken, runtime: {kind: echo}, config: {fail: true}}
  channels:
    - {id: in, to: [broken], maxDeliver: 3}
`

func TestRetryThenDeadLetter(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")
	r.deploy(failing)
	m := r.inject("failing", "in", "job", map[string]int{"x": 1})
	eventually(t, 20*time.Second, "message dead-lettered", func() bool {
		recs, _ := r.store.GetMessage(r.ctx, m.ID)
		for _, rec := range recs {
			if rec.Status == domain.MsgDeadLettered && rec.Attempt == 3 {
				return true
			}
		}
		return false
	})
	xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "failing", Status: domain.ExecFailed})
	if len(xs) != 3 {
		t.Fatalf("want 3 failed executions, got %d", len(xs))
	}
}

const autostop = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: flock}
spec:
  nodes:
    - id: researcher
      runtime: {kind: echo}
      config: {stopAfter: 2}
      loop: {mode: continuous, stop: {policy: auto}}
      cluster: {replicas: 3, comms: ring}
`

func TestRingClusterAutoStop(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")
	r.deploy(autostop)
	eventually(t, 20*time.Second, "all replicas completed via the stop handshake", func() bool {
		st, err := r.eng.Status(r.ctx, "flock")
		if err != nil || len(st.Nodes[0].Replicas) != 3 {
			return false
		}
		for _, rep := range st.Nodes[0].Replicas {
			if rep.State != domain.ReplicaCompleted {
				return false
			}
		}
		return true
	})
	// Each replica took exactly: declaration turn (iteration 2) + confirmation turn.
	xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "flock", Limit: 100})
	if len(xs) != 9 {
		t.Fatalf("want 9 turns (3 per replica), got %d", len(xs))
	}
	// Ring neighbours saw each other's broadcasts.
	evs, _ := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "flock", Types: []domain.EventType{domain.EvAgentBroadcast}, Limit: 100})
	if len(evs) != 9 {
		t.Fatalf("want 9 broadcasts, got %d", len(evs))
	}
}

const native1 = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: native}
spec:
  nodes:
    - id: writer
      prompt: Summarise what you get and hand it on.
      tools: [filesystem.read]
    - {id: sink, runtime: {kind: echo}}
  channels:
    - {id: in, to: [writer]}
    - {id: out, from: writer, to: [sink]}
`

func TestNativeRuntimeWithMockModel(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")
	r.deploy(native1)
	r.inject("native", "in", "doc", map[string]string{"text": "hello world"})
	eventually(t, 15*time.Second, "native agent emitted via the emit tool", func() bool { return r.acked("native", "out") == 1 })
	recs, _ := r.store.ListMessages(r.ctx, storage.MessageFilter{GraphID: "native", Channel: "out", Limit: 1})
	if recs[0].Message.Type != "mock.output" {
		t.Fatalf("unexpected type %q", recs[0].Message.Type)
	}
	xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "native", NodeID: "writer", Limit: 1})
	if xs[0].Usage.InputTokens == 0 {
		t.Fatal("token usage not recorded")
	}
}

// TestFailover: instance 1 runs a graph and dies without releasing its
// leases; instance 2 (same store and broker) takes the replicas over after
// the lease TTL and finishes the work.
func TestFailover(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, dir)
	broker := memory.New()
	r1 := newRig(t, store, broker, dir, "i1")
	r1.deploy(pipeline)
	eventually(t, 10*time.Second, "i1 owns replicas", func() bool {
		ls, _ := store.ListLeases(r1.ctx, "pipe/")
		return len(ls) == 6
	})
	// Simulate a crash: stop i1's loops but put its leases back, as a dead
	// process would leave them (a graceful shutdown releases them).
	held := mustLeases(t, store)
	r1.eng.Shutdown(context.Background())
	crashed := time.Now()
	for _, l := range held {
		if ok, err := store.AcquireLease(r1.ctx, l.Key, "i1", 3*time.Second); !ok || err != nil {
			t.Fatalf("re-take %s: %v %v", l.Key, ok, err)
		}
	}
	r2 := newRig(t, store, broker, dir, "i2")
	for i := 0; i < 4; i++ {
		r2.inject("pipe", "in", "job", map[string]int{"i": i})
	}
	eventually(t, 30*time.Second, "i2 took over and processed the work", func() bool { return r2.acked("pipe", "bc") == 8 })
	if waited := time.Since(crashed); waited < 2500*time.Millisecond {
		t.Fatalf("i2 ran replicas after %s, before i1's leases expired", waited)
	}
	for _, l := range mustLeases(t, store) {
		if l.Holder != "i2" {
			t.Fatalf("lease %s still held by %s", l.Key, l.Holder)
		}
	}
}

func mustLeases(t *testing.T, s storage.Store) []domain.Lease {
	ls, err := s.ListLeases(context.Background(), "pipe/")
	if err != nil {
		t.Fatal(err)
	}
	return ls
}

const onceTwoWays = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: once}
spec:
  defaults: {runtime: {kind: echo}}
  nodes:
    - {id: ok, loop: {mode: once}}
    - {id: broken, loop: {mode: once}, config: {fail: true}}
`

// A once-mode node whose only turn failed ends failed (not completed) and
// is not retried in the same run; a successful one ends completed.
func TestOnceModeReportsFailedTurns(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1")
	r.deploy(onceTwoWays)
	state := func(node string) (domain.ReplicaState, string) {
		list, _ := r.store.ListReplicaStatus(r.ctx, "once")
		for _, s := range list {
			if s.NodeID == node {
				return s.State, s.Detail
			}
		}
		return "", ""
	}
	eventually(t, 15*time.Second, "both replicas finished", func() bool {
		a, _ := state("ok")
		b, _ := state("broken")
		return a == domain.ReplicaCompleted && b == domain.ReplicaFailed
	})
	if _, detail := state("broken"); !strings.Contains(detail, "its only turn failed") {
		t.Fatalf("detail: %q", detail)
	}
	// Not retried: one failed execution after a few reconcile intervals.
	time.Sleep(500 * time.Millisecond)
	xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "once", NodeID: "broken"})
	if len(xs) != 1 || xs[0].Status != domain.ExecFailed {
		t.Fatalf("executions of the failed once node: %d", len(xs))
	}
	evs, _ := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "once", NodeID: "broken", Types: []domain.EventType{domain.EvLog}})
	if len(evs) == 0 || !strings.Contains(string(evs[0].Data), "replica failed: its only turn failed") || evs[0].Level != "error" {
		t.Fatalf("log event: %+v", evs)
	}
}
