package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// replica is one running agent instance: one loop, one session per turn.
//
// Stop is graceful: the turn in flight finishes (it has claimed messages
// and not yet published what it did with them), then the loop exits. Kill
// cancels the turn; its inputs are nacked and redelivered.
type replica struct {
	nodeID string
	index  int

	mu               sync.Mutex
	run_             *graphRun
	state            domain.ReplicaState
	nodePaused       bool
	iteration        int
	runIter          int
	runStart         time.Time
	idleSince        time.Time
	sandboxSuspended bool
	suspendRetry     time.Time
	pendStop         bool // auto-stop declaration pending confirmation
	execID           string
	workdir          string
	branch           string
	exited           bool

	stopping   bool
	loopCtx    context.Context
	loopCancel context.CancelFunc // cancels waits (graceful stop)
	turnCancel context.CancelFunc // cancels the in-flight turn (kill)
	pokeCh     chan struct{}
	doneCh     chan struct{}

	subs              []sub
	sandbox           tools.Sandbox
	sandboxDefinition string
}

type sub struct {
	group graph.Group
	s     queue.Subscription
	cfg   *domain.Channel
}

func newReplica(r *graphRun, node string, idx int) *replica {
	ctx, cancel := context.WithCancel(context.Background())
	return &replica{
		nodeID: node, index: idx, run_: r,
		state:   domain.ReplicaStopped,
		loopCtx: ctx, loopCancel: cancel,
		pokeCh: make(chan struct{}, 1),
		doneCh: make(chan struct{}),
	}
}

func (r *replica) key() string { return r.nodeID + "#" + itoa(r.index) }

func (r *replica) run_l() *graphRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run_
}

func (r *replica) setRun(gr *graphRun) {
	r.mu.Lock()
	r.run_ = gr
	r.mu.Unlock()
}

func (r *replica) poke() {
	select {
	case r.pokeCh <- struct{}{}:
	default:
	}
}

func (r *replica) setNodePaused(p bool) {
	r.mu.Lock()
	changed := r.nodePaused != p
	r.nodePaused = p
	r.mu.Unlock()
	if changed {
		r.poke()
	}
}

func (r *replica) paused(ctx context.Context) (bool, error) {
	r.mu.Lock()
	np := r.nodePaused
	gr := r.run_
	r.mu.Unlock()
	// Reconciliation is asynchronous. A message can wake this replica before
	// the in-memory graph has observed a successful pause request.
	rec, err := gr.e.store.GetGraphRecord(ctx, gr.g.Metadata.ID)
	if err != nil {
		return true, err
	}
	return np || rec.DesiredState != domain.GraphRunning, nil
}

func (r *replica) finished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.exited
}

func (r *replica) idleFor(d time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return (r.state == domain.ReplicaIdle || r.state == domain.ReplicaPaused) && !r.idleSince.IsZero() && time.Since(r.idleSince) >= d
}

// stop requests the loop to end and waits for it.
func (r *replica) stop(kill bool) {
	r.mu.Lock()
	r.stopping = true
	if kill && r.turnCancel != nil {
		r.turnCancel()
	}
	if r.state.Live() && r.state != domain.ReplicaStopping {
		r.state = domain.ReplicaStopping
	}
	r.mu.Unlock()
	r.loopCancel()
	r.poke()
	<-r.doneCh
}

func (r *replica) isStopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}

func (r *replica) node() (*graphRun, *domain.Node) {
	gr := r.run_l()
	return gr, gr.g.Node(r.nodeID)
}

// setState records and publishes a state change.
func (r *replica) setState(st domain.ReplicaState, detail string) {
	r.mu.Lock()
	prev := r.state
	if prev == st && detail == "" {
		r.mu.Unlock()
		return
	}
	r.state = st
	if st == domain.ReplicaIdle || st == domain.ReplicaPaused {
		if r.idleSince.IsZero() {
			r.idleSince = time.Now()
		}
	} else {
		r.idleSince = time.Time{}
	}
	if r.sandboxSuspended && (st == domain.ReplicaIdle || st == domain.ReplicaPaused) {
		detail = "sandbox suspended; workspace preserved"
	}
	status := r.statusLocked(detail)
	gr := r.run_
	r.mu.Unlock()

	e := gr.e
	ctx := context.Background()
	_ = e.store.PutReplicaStatus(ctx, status)
	if prev != st {
		observability.Replicas.WithLabelValues(gr.g.Metadata.ID, r.nodeID, string(prev)).Dec()
		observability.Replicas.WithLabelValues(gr.g.Metadata.ID, r.nodeID, string(st)).Inc()
		e.bus.Publish(ctx, domain.NewEvent(domain.EvReplicaState, gr.g.Metadata.ID, status).WithNode(r.nodeID, r.index))
	}
}

func (r *replica) statusLocked(detail string) *domain.ReplicaStatus {
	s := &domain.ReplicaStatus{
		GraphID: r.run_.g.Metadata.ID, NodeID: r.nodeID, Replica: r.index,
		State: r.state, Iteration: r.iteration, RunIteration: r.runIter,
		Execution: r.execID, Instance: r.run_.e.opt.Instance, Detail: detail,
		Workdir: r.workdir, Branch: r.branch, UpdatedAt: time.Now().UTC(),
	}
	if n := r.run_.g.Node(r.nodeID); n != nil && n.Loop.Stop.Policy == domain.StopDuration && !r.runStart.IsZero() {
		d := r.runStart.Add(n.Loop.Stop.MaxDuration.D())
		s.Deadline = &d
	}
	return s
}

// run is the replica's goroutine.
func (r *replica) run() {
	defer close(r.doneCh)
	gr, n := r.node()
	e := gr.e
	log := e.log.With("graph", gr.g.Metadata.ID, "node", r.nodeID, "replica", r.index)
	ctx := r.loopCtx

	// Resume the lifetime iteration counter across restarts.
	if prev := r.previousStatus(ctx); prev != nil {
		r.iteration = prev.Iteration
	}
	r.runStart = time.Now()
	r.setState(domain.ReplicaStarting, "")

	completed := false
	// failedFinal: the replica ended because its work failed (a once-mode
	// node whose only turn failed). It is not restarted, like a completed
	// one, but reports failed so nobody mistakes it for success.
	failedFinal := ""
	defer func() {
		r.teardown(log)
		final, detail := domain.ReplicaStopped, ""
		switch {
		case failedFinal != "":
			final, detail = domain.ReplicaFailed, failedFinal
		case completed:
			final = domain.ReplicaCompleted
		}
		r.setState(final, detail)
		r.mu.Lock()
		r.exited = true
		r.mu.Unlock()
		e.Nudge()
	}()

	if err := r.setup(ctx); err != nil {
		log.Error("replica setup failed", "err", err)
		r.setState(domain.ReplicaFailed, err.Error())
		r.backoff(ctx, 10*time.Second)
		return
	}
	r.setState(domain.ReplicaIdle, "")

	var schedNext time.Time
	failures := 0
	var lastTurnErr error
	for {
		if r.isStopping() || ctx.Err() != nil {
			return
		}
		gr, n = r.node()
		if n == nil {
			return // node removed from the graph
		}

		r.suspendIdle(ctx)
		if e.opt.Reviews != nil {
			pending, err := e.opt.Reviews.Pending(ctx, gr.g.Metadata.ID, r.nodeID, r.index)
			if !pending && err == nil {
				terminal, checkErr := e.opt.Reviews.Terminal(ctx, gr.g.Metadata.ID, r.nodeID, r.index)
				if checkErr != nil {
					err = checkErr
				} else if terminal != "" {
					failedFinal = "human review " + terminal
					r.fail(ctx, failedFinal)
					return
				}
			}
			if pending || err != nil {
				waitStart := time.Now()
				if pending {
					r.suspendForReview(ctx)
				}
				detail := "waiting for durable human review"
				if err != nil {
					detail = "cannot check human review: " + err.Error()
				}
				r.setState(domain.ReplicaAwaitingApproval, detail)
				r.wait(ctx, time.Second)
				r.mu.Lock()
				r.runStart = r.runStart.Add(time.Since(waitStart))
				r.idleSince = time.Now()
				r.mu.Unlock()
				continue
			}
		}

		// --- should another turn start? -----------------------------------
		if done, why := r.stopPolicyMet(n); done {
			r.complete(ctx, why)
			completed = true
			return
		}
		paused, pauseErr := r.paused(ctx)
		if pauseErr != nil {
			log.Warn("cannot check graph execution state", "err", pauseErr)
			r.wait(ctx, time.Second)
			continue
		}
		if paused {
			r.setState(domain.ReplicaPaused, "")
			r.wait(ctx, time.Second)
			continue
		}

		// Check once-mode completion before claiming another operator request.
		if n.Loop.Mode == domain.LoopOnce && r.runIter >= 1 {
			if lastTurnErr != nil {
				failedFinal = "its only turn failed: " + truncate(lastTurnErr.Error(), 300)
				r.fail(ctx, failedFinal)
				return
			}
			r.complete(ctx, "ran once")
			completed = true
			return
		}

		ops, err := e.store.ConsumeOperatorMessages(ctx, gr.g.Metadata.ID, r.nodeID, r.index)
		if err != nil && ctx.Err() == nil {
			log.Warn("operator messages", "err", err)
		}

		var dels []queue.Delivery
		var delSubs []*sub
		switch n.Loop.Mode {
		case domain.LoopOnce:
			dels, delSubs = r.fetch(ctx, n.Loop.BatchSize, 0)
		case domain.LoopContinuous:
			dels, delSubs = r.fetch(ctx, n.Loop.BatchSize, 0)
		case domain.LoopScheduled:
			sch, _ := graph.ParseSchedule(n.Loop.Schedule)
			if schedNext.IsZero() {
				schedNext = sch.Next(time.Now())
				if r.runIter == 0 {
					schedNext = time.Now() // first turn on start, then on schedule
				}
			}
			if len(ops) == 0 && time.Now().Before(schedNext) {
				r.setState(domain.ReplicaIdle, "next run "+schedNext.Format(time.RFC3339))
				r.wait(ctx, time.Until(schedNext))
				continue
			}
			schedNext = sch.Next(time.Now())
			dels, delSubs = r.fetch(ctx, n.Loop.BatchSize, 0)
		case domain.LoopEvent:
			if len(ops) == 0 {
				dels, delSubs = r.fetch(ctx, n.Loop.BatchSize, 2*time.Second)
				if len(dels) == 0 {
					if len(r.subs) == 0 {
						// A source with nothing wired in has nothing to wait for;
						// event mode on it means "on operator message" only.
						r.setState(domain.ReplicaIdle, "waiting for an operator message")
						r.wait(ctx, 5*time.Second)
					} else {
						r.setState(domain.ReplicaIdle, "")
					}
					if n.Loop.Stop.Policy == domain.StopIdle && r.idleFor(n.Loop.Stop.IdleTimeout.D()) {
						r.complete(ctx, "idle timeout")
						completed = true
						return
					}
					continue
				}
			}
		}
		if ctx.Err() != nil {
			for _, d := range dels {
				_ = d.Nack(context.Background(), 0)
			}
			return
		}

		// A pause may arrive while fetching queue deliveries. Keep claimed inputs
		// in this turn until resume; cancellation below returns deliveries.
		for ctx.Err() == nil && !r.isStopping() {
			paused, err := r.paused(ctx)
			if err == nil && !paused {
				break
			}
			if err != nil {
				log.Warn("cannot check graph execution state", "err", err)
			} else {
				r.setState(domain.ReplicaPaused, "")
			}
			r.wait(ctx, time.Second)
		}
		if ctx.Err() != nil || r.isStopping() {
			for _, d := range dels {
				if err := d.Nack(context.Background(), 0); err != nil {
					log.Warn("return paused delivery", "err", err)
				}
			}
			return
		}

		// --- turn ------------------------------------------------------------
		out := r.turn(ctx, n, dels, delSubs, ops)
		lastTurnErr = out.err
		switch {
		case out.err != nil:
			failures++
			r.setState(domain.ReplicaIdle, "last turn failed: "+truncate(out.err.Error(), 200))
			// Back off on repeated failures so a broken dependency is not hammered.
			r.backoff(ctx, backoffDelay(e.opt.RetryBackoff, failures))
		default:
			failures = 0
		}
		if out.completed {
			r.complete(ctx, out.reason)
			completed = true
			return
		}
		if d := n.Loop.Interval.D(); d > 0 {
			r.setState(domain.ReplicaIdle, "pacing")
			r.wait(ctx, d)
		}
	}
}

func (r *replica) previousStatus(ctx context.Context) *domain.ReplicaStatus {
	gr := r.run_l()
	list, err := gr.e.store.ListReplicaStatus(ctx, gr.g.Metadata.ID)
	if err != nil {
		return nil
	}
	for _, s := range list {
		if s.NodeID == r.nodeID && s.Replica == r.index {
			return s
		}
	}
	return nil
}

func (r *replica) stopPolicyMet(n *domain.Node) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch n.Loop.Stop.Policy {
	case domain.StopIterations:
		if r.runIter >= n.Loop.Stop.MaxIterations {
			return true, fmt.Sprintf("reached %d iterations", n.Loop.Stop.MaxIterations)
		}
	case domain.StopDuration:
		if time.Since(r.runStart) >= n.Loop.Stop.MaxDuration.D() {
			return true, "time limit reached"
		}
	}
	return false, ""
}

func (r *replica) complete(ctx context.Context, why string) {
	gr := r.run_l()
	gr.e.markCompleted(context.Background(), gr.g.Metadata.ID, r.nodeID, r.index, gr.runID)
	gr.e.bus.Publish(context.Background(), domain.NewEvent(domain.EvLog, gr.g.Metadata.ID, map[string]any{"message": "replica completed: " + why}).
		WithNode(r.nodeID, r.index))
}

// fail ends a replica whose work failed: like complete it is not restarted
// in this run (a new start runs it again), but it reports failed.
func (r *replica) fail(ctx context.Context, why string) {
	gr := r.run_l()
	gr.e.markCompleted(context.Background(), gr.g.Metadata.ID, r.nodeID, r.index, gr.runID)
	ev := domain.NewEvent(domain.EvLog, gr.g.Metadata.ID, map[string]any{"message": "replica failed: " + why}).WithNode(r.nodeID, r.index)
	ev.Level = "error"
	gr.e.bus.Publish(context.Background(), ev)
}

// wait sleeps until d passes, the replica is poked, or the loop is cancelled.
func (r *replica) wait(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	deadline := time.Now().Add(d)
	for {
		delay := time.Until(deadline)
		idle := r.sandboxIdleDelay()
		if idle >= 0 && idle < delay {
			delay = idle
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-r.pokeCh:
			t.Stop()
			return
		case <-t.C:
			r.suspendIdle(ctx)
			if !time.Now().Before(deadline) {
				return
			}
		}
	}
}

func (r *replica) backoff(ctx context.Context, d time.Duration) { r.wait(ctx, d) }

func backoffDelay(base time.Duration, attempt int) time.Duration {
	d := time.Duration(float64(base) * math.Pow(2, float64(attempt-1)))
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

// setup prepares the replica's workspace, sandbox, runtime and subscriptions.
func (r *replica) setup(ctx context.Context) error {
	gr, n := r.node()
	e := gr.e
	dir, branch, err := e.opt.Workspaces.Replica(ctx, gr.g, n, r.index)
	if err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	if err := os.MkdirAll(workspace.Scratch(dir, n.ID, r.index), 0o755); err != nil {
		return err
	}
	r.mu.Lock()
	r.workdir, r.branch = dir, branch
	r.mu.Unlock()

	if e.opt.Sandboxes != nil {
		sb, err := e.opt.Sandboxes(ctx, SandboxSpec{Graph: gr.g, Node: n, Replica: r.index, Workdir: dir, HostWorkdir: e.opt.Workspaces.HostPath(dir)})
		if err != nil {
			return fmt.Errorf("sandbox: %w", err)
		}
		r.sandbox = sb
		definition, err := environmentDefinition(gr.g)
		if err != nil {
			return fmt.Errorf("environment identity: %w", err)
		}
		r.sandboxDefinition = definition
	}
	rt := e.opt.Runtimes[n.Runtime.Kind]
	if rt == nil {
		return fmt.Errorf("no runtime %q", n.Runtime.Kind)
	}
	if err := rt.Prepare(ctx, runtime.ReplicaContext{Graph: gr.g, Node: n, Replica: r.index, Workdir: dir}); err != nil {
		return fmt.Errorf("runtime prepare: %w", err)
	}
	consumer := fmt.Sprintf("%s#%d@%s", n.ID, r.index, e.opt.Instance)
	for _, g := range graph.ConsumerGroups(gr.g, n.ID, r.index) {
		s, err := e.opt.Broker.Subscribe(ctx, g.Subject, g.Group, consumer)
		if err != nil {
			return fmt.Errorf("subscribe %s/%s: %w", g.Subject, g.Group, err)
		}
		r.subs = append(r.subs, sub{group: g, s: s, cfg: gr.g.Channel(g.Channel)})
	}
	return nil
}

func (r *replica) teardown(log interface{ Warn(string, ...any) }) {
	gr, n := r.node()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, s := range r.subs {
		_ = s.s.Close()
	}
	r.subs = nil
	if n != nil {
		if rt := gr.e.opt.Runtimes[n.Runtime.Kind]; rt != nil {
			if err := rt.Release(ctx, runtime.ReplicaContext{Graph: gr.g, Node: n, Replica: r.index, Workdir: r.workdir}); err != nil {
				log.Warn("runtime release", "err", err)
			}
		}
	}
	if r.sandbox != nil {
		if err := r.sandbox.Close(); err != nil {
			log.Warn("sandbox removal or capacity release failed", "err", err)
		}
	}
}

// fetch claims up to max deliveries across the replica's subscriptions,
// waiting up to wait for the first. Duplicates (already processed by this
// group) are acked and skipped here.
func (r *replica) fetch(ctx context.Context, max int, wait time.Duration) ([]queue.Delivery, []*sub) {
	if len(r.subs) == 0 {
		return nil, nil
	}
	var out []queue.Delivery
	var from []*sub
	per := wait
	if len(r.subs) > 1 && wait > 0 {
		per = wait / time.Duration(len(r.subs))
		if per < 100*time.Millisecond {
			per = 100 * time.Millisecond
		}
	}
	// Rotate the starting subscription so one busy input cannot starve another.
	start := r.iteration % len(r.subs)
	for i := 0; i < len(r.subs) && len(out) < max; i++ {
		s := &r.subs[(start+i)%len(r.subs)]
		w := per
		if len(out) > 0 {
			w = 0
		}
		ds, err := s.s.Fetch(ctx, max-len(out), w)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, context.DeadlineExceeded) {
				r.run_l().e.log.Warn("fetch", "subject", s.group.Subject, "group", s.group.Group, "err", err)
			}
			continue
		}
		for _, d := range ds {
			if r.duplicate(ctx, s, d) {
				continue
			}
			out = append(out, d)
			from = append(from, s)
		}
	}
	return out, from
}

// duplicate drops a redelivery of a message this group already processed
// (the turn succeeded and published, but the ack was lost). The processed
// set is the authoritative record; message history is best-effort only.
func (r *replica) duplicate(ctx context.Context, s *sub, d queue.Delivery) bool {
	gr := r.run_l()
	seen, err := gr.e.store.IsProcessed(ctx, s.group.Group, d.Message().ID)
	if err != nil || !seen {
		return false
	}
	_ = d.Ack(ctx)
	observability.MessagesConsumed.WithLabelValues(gr.g.Metadata.ID, r.nodeID, "duplicate").Inc()
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = storage.ErrNotFound
