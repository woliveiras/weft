// Package engine is the heart of the platform: it turns stored graph
// documents into running agent loops and keeps reality converging on the
// desired state.
//
// # Shape
//
// The engine is a reconciler. Desired state (graph running/paused/stopped,
// per-node overrides, replica counts) lives in storage. Every instance runs
// the same reconcile loop: for each replica that should exist it tries to
// take a lease; the instance holding a replica's lease runs it, renews the
// lease while it runs, and lets it go when the replica should no longer
// exist. Several instances therefore share a fleet without coordination
// beyond the store, and when one dies its leases expire and the survivors
// pick its replicas up. Unacknowledged messages of the dead instance are
// redelivered by the broker, and its in-flight executions are marked
// interrupted by whoever acquires the replica next.
//
// # Replica loop
//
// A replica (one agent instance) runs a loop: decide whether a turn should
// start (stop policies, pause, schedule), claim input messages, run the
// turn through its Runtime, publish what it emitted, then ack the inputs.
// Ack-after-publish gives at-least-once delivery; consumers deduplicate by
// message id, which makes processing effectively-once. See replica.go.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/woliveiras/weft/server/internal/delegation"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// Models is the engine's view of the LLM gateway.
type Models interface {
	Resolve(ref domain.ModelRef) (llm.Provider, string, error)
	Chat(ctx context.Context, provider string, req *llm.ChatRequest) (*llm.ChatResponse, error)
}

// StreamingModels is implemented by gateways that can stream an answer's
// text (llm.Registry). ChatStream must account for the call exactly as
// Chat does; the engine streams only native turns' calls through it.
type StreamingModels interface {
	ChatStream(ctx context.Context, provider string, req *llm.ChatRequest, onText func(string)) (*llm.ChatResponse, error)
}

// ToolGateway is the engine's view of the tool gateway.
type ToolGateway interface {
	Invoke(ctx context.Context, p *tools.Principal, name string, args json.RawMessage) (*tools.Result, error)
	Schemas(p *tools.Principal) []llm.ToolSchema
	// NeedsApproval reports whether invoking name as p will wait for a human
	// (same merge rules the gateway applies).
	NeedsApproval(p *tools.Principal, name string) bool
}

// SandboxSpec describes the sandbox a replica needs.
type SandboxSpec struct {
	Name           string
	Command        []string
	ExtraVolumes   []string
	ReadOnlyCaches bool
	Graph          *domain.Graph
	Node           *domain.Node
	Replica        int
	Workdir        string // platform path
	HostWorkdir    string // daemon path (docker bind mounts)
}

// SandboxFactory builds the command sandbox of a replica.
type SandboxFactory func(ctx context.Context, s SandboxSpec) (tools.Sandbox, error)

// Options wires the engine to its adapters.
type Options struct {
	ValidateToolGrants func(context.Context, *domain.Graph) error
	Reviews            interface {
		Pending(context.Context, string, string, int) (bool, error)
		AssertApproved(context.Context, string, string, int) error
		Terminal(context.Context, string, string, int) (string, error)
		Cancel(context.Context, string, string, string) error
	}
	SandboxIdleTimeout time.Duration
	Instance           string
	Store              storage.Store
	Broker             queue.Broker
	Bus                *events.Bus
	Approvals          *hitl.Service
	Models             Models
	Tools              ToolGateway
	Runtimes           map[domain.RuntimeKind]runtime.Runtime
	Workspaces         *workspace.Manager
	Sandboxes          SandboxFactory
	// Environments, when set, is consulted before starting nodes that run
	// commands in the graph's environment (see UsesEnvironment).
	Environments         EnvironmentChecker
	EnvironmentProposals EnvironmentChecker
	CallbackURL          string // base internal URL, e.g. http://weftd:8080
	Log                  *slog.Logger

	LeaseTTL           time.Duration
	ReconcileInterval  time.Duration
	RetryBackoff       time.Duration
	MaxConcurrentTurns int
	// LiveOutputInterval is the least time between two live output events
	// of one streamed model call (default 1s): it bounds how many events a
	// long answer adds to the event log.
	LiveOutputInterval time.Duration
}

// EnvironmentChecker reports whether a graph's environment can serve agents.
type EnvironmentChecker interface {
	Check(ctx context.Context, g *domain.Graph) error
}

type Engine struct {
	opt   Options
	log   *slog.Logger
	store storage.Store
	bus   *events.Bus

	mu     sync.Mutex
	graphs map[string]*graphRun // graphs with at least one local replica or deployed topology

	execs *execRegistry
	turns chan struct{} // global turn semaphore (nil = unlimited)

	// Only the graph reconcile loop writes sweep state.
	budgetSweepAt        time.Time
	brokerCleanupSweepAt time.Time
	brokerCleanupRetries map[string]brokerCleanupRetry
	wake                 chan struct{}
	stop                 context.CancelFunc
	done                 chan struct{}

	invalidLedgerWarningAt  atomic.Int64
	budgetConflictWarningAt atomic.Int64
	invalidControlWarnings  graphWarningLimiter
}

func New(opt Options) (*Engine, error) {
	if opt.Store == nil || opt.Broker == nil || opt.Bus == nil || opt.Approvals == nil {
		return nil, errors.New("engine: store, broker, bus and approvals are required")
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.LeaseTTL == 0 {
		opt.LeaseTTL = 20 * time.Second
	}
	if opt.ReconcileInterval == 0 {
		opt.ReconcileInterval = opt.LeaseTTL / 4
	}
	if opt.LiveOutputInterval <= 0 {
		opt.LiveOutputInterval = time.Second
	}
	if opt.RetryBackoff == 0 {
		opt.RetryBackoff = 2 * time.Second
	}
	e := &Engine{
		opt:    opt,
		log:    opt.Log.With("component", "engine"),
		store:  opt.Store,
		bus:    opt.Bus,
		graphs: map[string]*graphRun{},
		execs:  newExecRegistry(),
		wake:   make(chan struct{}, 1),
	}
	if opt.MaxConcurrentTurns > 0 {
		e.turns = make(chan struct{}, opt.MaxConcurrentTurns)
	}
	opt.Approvals.OnDecision(e.onApprovalDecided)
	return e, nil
}

// KnownRuntimes is used by validation.
func (e *Engine) KnownRuntimes() map[domain.RuntimeKind]bool {
	m := map[domain.RuntimeKind]bool{}
	for k := range e.opt.Runtimes {
		m[k] = true
	}
	return m
}

// Start runs recovery and the reconcile loop in the background.
func (e *Engine) Start(ctx context.Context) error {
	if err := e.recover(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	e.stop = cancel
	e.done = make(chan struct{})
	go e.loop(ctx)
	return nil
}

// Shutdown stops local replicas gracefully (in-flight turns finish, bounded
// by ctx) and releases their leases so another instance can take over at once.
func (e *Engine) Shutdown(ctx context.Context) {
	if e.stop != nil {
		e.stop()
		<-e.done
	}
	e.mu.Lock()
	runs := make([]*graphRun, 0, len(e.graphs))
	for _, r := range e.graphs {
		runs = append(runs, r)
	}
	e.mu.Unlock()
	var wg sync.WaitGroup
	for _, r := range runs {
		wg.Add(1)
		go func(r *graphRun) { defer wg.Done(); r.stopAll(ctx, false) }(r)
	}
	wg.Wait()
}

// Nudge triggers an immediate reconcile.
func (e *Engine) Nudge() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) loop(ctx context.Context) {
	defer close(e.done)
	budgetDone := make(chan struct{})
	go func() {
		defer close(budgetDone)
		e.budgetGraphLoop(ctx)
	}()
	defer func() { <-budgetDone }()
	t := time.NewTicker(e.opt.ReconcileInterval)
	defer t.Stop()
	for {
		e.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.wake:
		}
	}
}

// recover repairs state left by a previous incarnation of this instance:
// executions it was running are marked interrupted (their inputs are
// redelivered by the broker when the leases on them expire).
func (e *Engine) recover(ctx context.Context) error {
	execs, err := e.store.ListExecutions(ctx, storage.ExecutionFilter{Status: domain.ExecRunning, Limit: 10000})
	if err != nil {
		return fmt.Errorf("recover: %w", err)
	}
	pending, err := e.store.ListExecutions(ctx, storage.ExecutionFilter{Status: domain.ExecAwaitingApproval, Limit: 10000})
	if err != nil {
		return fmt.Errorf("recover delegated approvals: %w", err)
	}
	for _, x := range pending {
		if x.ParentExecutionID != "" {
			execs = append(execs, x)
		}
	}
	for _, x := range execs {
		if x.Instance == e.opt.Instance {
			e.interrupt(ctx, x, "platform restarted during the turn")
		}
	}
	if err := (delegation.Manager{Store: e.store}).ReconcileBudgets(ctx, time.Now().UTC()); err != nil {
		e.log.Error("recover tree token budgets", "err", err)
	}
	return nil
}

func (e *Engine) interrupt(ctx context.Context, x *domain.Execution, why string) {
	if x.ParentExecutionID != "" && x.ApprovalID != "" {
		a, err := e.store.GetApproval(ctx, x.ApprovalID)
		if err == nil && a.Status == domain.ApprovalPending {
			a, err = e.opt.Approvals.Expire(ctx, a)
		}
		if err != nil {
			e.log.Error("interrupt delegation approval", "execution", x.ID, "err", err)
		} else {
			snapshotDelegationDecision(x, a)
		}
	}

	now := time.Now().UTC()
	x.Status = domain.ExecInterrupted
	x.Error = why
	x.CompletedAt = &now
	if err := e.store.PutExecution(ctx, x); err == nil {
		e.bus.Publish(ctx, domain.NewEvent(domain.EvExecFailed, x.GraphID, map[string]any{"status": x.Status, "error": why}).
			WithNode(x.NodeID, x.Replica).WithExecution(x.ID))
	}
}

// reconcile drives every graph toward its desired state.
func (e *Engine) reconcile(ctx context.Context) {
	if time.Since(e.budgetSweepAt) >= time.Minute {
		e.budgetSweepAt = time.Now()
		if err := (delegation.Manager{Store: e.store}).ReconcileBudgets(ctx, e.budgetSweepAt); err != nil {
			e.log.Error("reconcile tree token budgets", "err", err)
		}
	}
	recs, err := e.store.ListGraphRecords(ctx)
	if err != nil {
		e.log.Warn("reconcile: list graphs", "err", err)
		return
	}
	seen := map[string]bool{}
	for _, rec := range recs {
		seen[rec.ID] = true
		if err := e.reconcileGraph(ctx, rec); err != nil && ctx.Err() == nil {
			e.log.Warn("reconcile graph", "graph", rec.ID, "err", err)
			e.markFailed(ctx, rec, err)
		}
	}
	// Graphs deleted from the store: stop anything still running locally.
	e.mu.Lock()
	var gone []*graphRun
	for id, r := range e.graphs {
		if !seen[id] {
			gone = append(gone, r)
			delete(e.graphs, id)
		}
	}
	e.mu.Unlock()
	for _, r := range gone {
		r.stopAll(ctx, true)
	}
	e.opt.Approvals.Sweep(ctx)
	e.reconcileBrokerCleanup(ctx, seen)
}

func (e *Engine) reconcileGraph(ctx context.Context, rec *domain.GraphRecord) error {
	e.mu.Lock()
	run := e.graphs[rec.ID]
	e.mu.Unlock()

	if rec.DesiredState == domain.GraphStopped || rec.DesiredState == "" {
		if run != nil {
			run.stopAll(ctx, false)
			e.mu.Lock()
			delete(e.graphs, rec.ID)
			e.mu.Unlock()
		}
		e.settleStopped(ctx, rec)
		return nil
	}

	// Load (or reload, after an edit) the graph version the record points at.
	if run == nil || run.version != rec.Version || run.runID != rec.RunID {
		g, err := e.store.GetGraph(ctx, rec.ID, rec.Version)
		if err != nil {
			return err
		}
		graph.Normalize(g)
		next, err := newGraphRun(e, g, rec.RunID)
		if err != nil {
			return err
		}
		if run != nil {
			// A new version keeps replicas whose node definition is unchanged
			// running; changed or removed nodes restart on their next turn
			// boundary (graceful).
			next.adopt(ctx, run)
		}
		if err := next.declareTopology(ctx); err != nil {
			return err
		}
		e.mu.Lock()
		e.graphs[rec.ID] = next
		e.mu.Unlock()
		run = next
	}
	run.setPaused(rec.DesiredState == domain.GraphPaused)
	return run.reconcile(ctx)
}

// markFailed records that a graph the operator wants running cannot be
// deployed (its version cannot be loaded, a condition no longer compiles,
// the broker refuses its topology). Reconciliation keeps retrying, and the
// state returns to running as soon as a deploy succeeds.
func (e *Engine) markFailed(ctx context.Context, rec *domain.GraphRecord, cause error) {
	if rec.DesiredState == domain.GraphStopped || rec.State == domain.GraphFailed {
		return
	}
	if e.updateObservedState(ctx, rec, domain.GraphFailed) {
		e.bus.Publish(ctx, domain.NewEvent(domain.EvGraphFailed, rec.ID, map[string]any{"error": cause.Error()}))
	}
}

// settleStopped makes the record reflect reality once no replica is live.
func (e *Engine) settleStopped(ctx context.Context, rec *domain.GraphRecord) {
	if rec.State == domain.GraphStopped {
		return
	}
	statuses, err := e.store.ListReplicaStatus(ctx, rec.ID)
	if err != nil {
		return
	}
	live := false
	for _, s := range statuses {
		if s.State.Live() {
			leases, _ := e.store.ListLeases(ctx, domain.ReplicaKey(rec.ID, s.NodeID, s.Replica))
			if len(leases) > 0 && leases[0].ExpiresAt.After(time.Now()) {
				live = true
				continue
			}
			// The instance running it is gone; record what we know.
			s.State = domain.ReplicaStopped
			s.UpdatedAt = time.Now().UTC()
			_ = e.store.PutReplicaStatus(ctx, s)
		}
	}
	if live {
		if rec.State != domain.GraphStopping {
			e.updateObservedState(ctx, rec, domain.GraphStopping)
		}
		return
	}
	if e.updateObservedState(ctx, rec, domain.GraphStopped) {
		e.bus.Publish(ctx, domain.NewEvent(domain.EvGraphStopped, rec.ID, map[string]any{"runId": rec.RunID}))
	}
}

func (e *Engine) graphRun(id string) *graphRun {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.graphs[id]
}

// acquireTurn bounds concurrent turns on this instance.
func (e *Engine) acquireTurn(ctx context.Context) (func(), error) {
	if e.turns == nil {
		return func() {}, nil
	}
	select {
	case e.turns <- struct{}{}:
		return func() { <-e.turns }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// An observation cannot replace a newer operator command or graph version.
// A conflict is expected: the next reconciliation reads the newer snapshot.
func (e *Engine) updateObservedState(ctx context.Context, rec *domain.GraphRecord, state domain.GraphState) bool {
	err := e.store.UpdateGraphState(ctx, rec, state)
	if err != nil && !errors.Is(err, storage.ErrConflict) && !errors.Is(err, storage.ErrNotFound) && ctx.Err() == nil {
		e.log.Warn("persist observed graph state", "graph", rec.ID, "err", err)
	}
	return err == nil
}
