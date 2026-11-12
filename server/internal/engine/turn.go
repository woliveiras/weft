package engine

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

type turnOutcome struct {
	err       error
	completed bool
	reason    string
}

// turn runs one turn end to end: execution record, runtime call, routing
// of outputs, acknowledgement of inputs, stop handshake, ring broadcast.
func (r *replica) turn(loopCtx context.Context, n *domain.Node, dels []queue.Delivery, from []*sub, ops []*domain.OperatorMessage) turnOutcome {
	gr := r.run_l()
	e := gr.e
	gid := gr.g.Metadata.ID

	release, err := e.acquireTurn(loopCtx)
	if err != nil {
		nackAll(dels, 0)
		return turnOutcome{}
	}
	defer release()

	// A turn survives a graceful stop (loopCtx) but not a kill (turnCtx).
	base := context.WithoutCancel(loopCtx)
	turnCtx, cancel := context.WithTimeout(base, n.Limits.TurnTimeout.D())
	defer cancel()
	r.mu.Lock()
	r.turnCancel = cancel
	r.iteration++
	r.runIter++
	iter := r.iteration
	pendingStop := r.pendStop
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.turnCancel = nil
		r.execID = ""
		r.mu.Unlock()
	}()

	inputs := make([]*domain.Message, len(dels))
	for i, d := range dels {
		inputs[i] = d.Message()
	}

	// Trace: the turn continues the trace of its first input; further inputs
	// are linked, so fan-in shows up as one span with several parents.
	var links []trace.Link
	parent := base
	for i, m := range inputs {
		c := observability.Extract(base, m.Metadata)
		if i == 0 {
			parent = c
		} else {
			links = append(links, trace.Link{SpanContext: trace.SpanContextFromContext(c)})
		}
	}
	turnCtx, span := observability.Tracer().Start(trace.ContextWithSpanContext(turnCtx, trace.SpanContextFromContext(parent)), "agent.turn",
		trace.WithLinks(links...),
		trace.WithAttributes(
			attribute.String("weft.graph", gid), attribute.String("weft.node", n.ID),
			attribute.Int("weft.replica", r.index), attribute.Int("weft.iteration", iter),
			attribute.Int("weft.inputs", len(inputs)),
		))
	defer span.End()

	now := time.Now().UTC()
	x := &domain.Execution{
		ID: domain.NewID("exe"), GraphID: gid, GraphVer: gr.version, RunID: gr.runID,
		NodeID: n.ID, Replica: r.index, Iteration: iter, Status: domain.ExecRunning,
		TraceID: observability.TraceID(turnCtx), Instance: e.opt.Instance, StartedAt: now,
	}
	for _, m := range inputs {
		x.InputIDs = append(x.InputIDs, m.ID)
	}
	span.SetAttributes(attribute.String("weft.execution", x.ID))
	if err := e.store.PutExecution(turnCtx, x); err != nil {
		nackAll(dels, e.opt.RetryBackoff)
		return turnOutcome{err: err}
	}
	turnCtx, stopControl := (executioncontrol.Manager{Store: e.store}).Watch(turnCtx, x.ID)
	defer stopControl()
	r.mu.Lock()
	r.execID = x.ID
	r.mu.Unlock()
	r.setState(domain.ReplicaRunning, "")
	e.bus.Publish(turnCtx, domain.NewEvent(domain.EvExecStarted, gid, map[string]any{"iteration": iter, "inputs": x.InputIDs}).
		WithNode(n.ID, r.index).WithExecution(x.ID).WithTrace(x.TraceID))

	for i, d := range dels {
		m := d.Message()
		if t := m.CreatedAt(); !t.IsZero() {
			observability.MessageLatency.WithLabelValues(gid, n.ID).Observe(time.Since(t).Seconds())
		}
		e.recordMessage(turnCtx, gid, from[i].group.Channel, from[i].group.Group, m, domain.MsgDelivered, d.Attempt(), x.ID, "")
	}

	// Keep leases alive while the turn runs.
	stopExtend := r.extendLeases(turnCtx, dels, from)
	defer stopExtend()

	finish := func(st domain.ExecutionStatus, errMsg string) {
		done := time.Now().UTC()
		x.Status, x.Error, x.CompletedAt = st, errMsg, &done
		_ = e.store.PutExecution(context.WithoutCancel(turnCtx), x)
		observability.Executions.WithLabelValues(gid, n.ID, string(st)).Inc()
		observability.ExecutionDuration.WithLabelValues(gid, n.ID).Observe(done.Sub(x.StartedAt).Seconds())
		ev := domain.EvExecCompleted
		if st != domain.ExecSucceeded {
			ev = domain.EvExecFailed
		}
		e.bus.Publish(context.WithoutCancel(turnCtx), domain.NewEvent(ev, gid, x).WithNode(n.ID, r.index).WithExecution(x.ID).WithTrace(x.TraceID))
	}

	// An explicit execution stop consumes this attempt; requeuing its inputs
	// would immediately restart the very work the operator interrupted.
	stopRequested := func() bool {
		if !errors.Is(context.Cause(turnCtx), executioncontrol.ErrRequested) {
			checkCtx, done := context.WithTimeout(context.WithoutCancel(turnCtx), 2*time.Second)
			err := (executioncontrol.Manager{Store: e.store}).Check(checkCtx, x.ID)
			done()
			if !errors.Is(err, executioncontrol.ErrRequested) {
				return false
			}
		}
		for i, d := range dels {
			if err := d.Term(context.WithoutCancel(turnCtx), executioncontrol.ErrRequested.Error()); err != nil {
				e.log.Error("cancelled input termination failed", "execution", x.ID, "err", err)
			}
			e.recordMessage(context.WithoutCancel(turnCtx), gid, from[i].group.Channel, from[i].group.Group, d.Message(), domain.MsgDeadLettered, d.Attempt(), x.ID, executioncontrol.ErrRequested.Error())
		}
		finish(domain.ExecCancelled, executioncontrol.ErrRequested.Error())
		return true
	}

	// Human gate before the turn.
	if n.Approval.BeforeTurn {
		r.setState(domain.ReplicaAwaitingApproval, "waiting for approval to start the turn")
		a, err := e.opt.Approvals.RequestApproval(turnCtx, &domain.Approval{
			GraphID: gid, NodeID: n.ID, Replica: r.index, ExecutionID: x.ID, Kind: domain.ApprovalTurn,
			Title:     fmt.Sprintf("%s wants to start turn %d", n.DisplayName(), iter),
			Detail:    mustJSON(map[string]any{"inputs": inputs, "onTimeout": n.Approval.OnTimeout}),
			ExpiresAt: expiry(n.Approval.Timeout),
		})
		if err != nil || a.Status != domain.ApprovalApproved {
			if stopRequested() {
				return turnOutcome{}
			}
			reason := "turn rejected by operator"
			if err != nil {
				reason = err.Error()
				nackAll(dels, e.opt.RetryBackoff)
				finish(domain.ExecCancelled, reason)
				return turnOutcome{}
			}
			for i, d := range dels {
				_ = d.Term(context.WithoutCancel(turnCtx), reason)
				e.recordMessage(turnCtx, gid, from[i].group.Channel, from[i].group.Group, d.Message(), domain.MsgDeadLettered, d.Attempt(), x.ID, reason)
			}
			finish(domain.ExecCancelled, reason)
			return turnOutcome{}
		}
		r.setState(domain.ReplicaRunning, "")
	}

	// Admission on resume is cancellable by Stop as well as the turn timeout.
	resumeCtx, resumeCancel := context.WithCancel(turnCtx)
	unregister := context.AfterFunc(loopCtx, resumeCancel)
	if loopCtx.Err() != nil {
		resumeCancel()
	}
	resumeErr := r.resumeSandbox(resumeCtx)
	unregister()
	resumeCancel()
	if resumeErr != nil {
		if stopRequested() {
			return turnOutcome{}
		}
		nackAll(dels, e.opt.RetryBackoff)
		for _, op := range ops {
			copy := *op
			copy.Consumed = nil
			if err := e.store.PutOperatorMessage(context.WithoutCancel(turnCtx), &copy); err != nil {
				resumeErr = errors.Join(resumeErr, fmt.Errorf("restore operator message: %w", err))
			}
		}
		finish(domain.ExecFailed, "resume sandbox: "+resumeErr.Error())
		return turnOutcome{err: resumeErr}
	}

	env := newTurnEnv(r, gr, n, x)
	env.executionCtx = turnCtx
	env.span = trace.SpanContextFromContext(turnCtx)
	token := e.execs.register(x.ID, env)
	defer e.execs.unregister(x.ID)

	in := r.buildInput(turnCtx, gr, n, x, inputs, ops, pendingStop, env.principal)
	instructionStart := time.Now()
	manager := instructions.Manager{Store: e.store}
	rt := e.opt.Runtimes[n.Runtime.Kind]
	checkout, rootErr := guidanceCheckoutRoot(e.opt.Workspaces, gr.g, n, in.Workdir)
	snapshot, guidance, instructionErr := manager.Prepare(turnCtx, gr.g, n, checkout, in.Workdir, in.Prompt, in.OperatorMessages)
	instructionErr = errors.Join(instructionErr, rootErr)
	if guidance != nil {
		if progressive, ok := rt.(runtime.ProgressiveRuntime); !ok || !progressive.SupportsProgressiveGuidance() {
			instructionErr = instructions.ErrIncompatibleRuntime
			for i := range snapshot.Entries {
				entry := &snapshot.Entries[i]
				if entry.Included {
					entry.Included, entry.Delivery, entry.Reason = false, "", "incompatible_runtime"
				}
			}
		}
	}
	if snapshot != nil {
		snapshot.ExecutionID = x.ID
		instructionErr = errors.Join(instructionErr, manager.SaveSnapshot(turnCtx, snapshot))
	}
	observability.InstructionLoadSeconds.Observe(time.Since(instructionStart).Seconds())
	if instructionErr != nil {
		if stopRequested() {
			return turnOutcome{}
		}
		observability.InstructionLoads.WithLabelValues("failed").Inc()
		nackAll(dels, e.opt.RetryBackoff)
		// Restore operator requests: a failed context load must not consume them.
		for _, op := range ops {
			copy := *op
			copy.Consumed = nil
			instructionErr = errors.Join(instructionErr, e.store.PutOperatorMessage(context.WithoutCancel(turnCtx), &copy))
		}
		finish(domain.ExecFailed, "instruction context unavailable: "+instructionErr.Error())
		return turnOutcome{err: instructionErr}
	}
	observability.InstructionLoads.WithLabelValues("loaded").Inc()
	omitted := 0
	for _, entry := range snapshot.Entries {
		if !entry.Included {
			omitted++
		}
	}
	span.SetAttributes(attribute.Int("weft.instructions.omitted", omitted), attribute.Int("weft.instructions.entries", len(snapshot.Entries)))
	in.Instructions = snapshot
	if guidance != nil {
		guidance.ReadReference = func(ctx context.Context, path string, read func() (string, string)) (string, string, error) {
			return env.readGuidanceReference(ctx, checkout, path, read)
		}
		in.Guidance = &turnGuidance{Session: guidance, env: env}
	} else if instructions.Enabled(gr.g) {
		in.Prompt = instructions.Context(*snapshot) + "\n" + in.Prompt
	}
	e.bus.Publish(turnCtx, domain.NewEvent("execution.instructions.loaded", gid, map[string]any{"entries": len(snapshot.Entries), "remaining_budget": snapshot.Budget, "omitted": omitted}).WithNode(n.ID, r.index).WithExecution(x.ID).WithTrace(x.TraceID))
	if e.opt.CallbackURL != "" {
		in.Callback = &runtime.Callback{URL: strings.TrimRight(e.opt.CallbackURL, "/") + "/internal/v1/executions/" + x.ID, Token: token}
	}

	res, err := rt.Turn(turnCtx, in, env)
	if guidance != nil {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(turnCtx), 2*time.Second)
		err = errors.Join(err, guidance.Finish(auditCtx))
		cancel()
	}
	// End callback admission before persisting or publishing a terminal outcome.
	// Deferred cleanup remains necessary for earlier exits, but is too late here.
	e.execs.unregister(x.ID)
	killed := errors.Is(turnCtx.Err(), context.Canceled) && !errors.Is(context.Cause(turnCtx), executioncontrol.ErrUnavailable)
	env.mu.Lock()
	yielded := env.yielded
	env.mu.Unlock()
	if yielded {
		// A persisted review owns the continuation. Discard every output from
		// this turn, including callbacks made before requesting review.
		env.drainEmits()
		res = &runtime.TurnResult{Summary: "Awaiting durable human review"}
		err = nil
	}
	if turnCtx.Err() != nil {
		err = context.Cause(turnCtx)
	} else if controlErr := (executioncontrol.Manager{Store: e.store}).Check(turnCtx, x.ID); controlErr != nil {
		err = controlErr
	}
	operatorCancelled := errors.Is(err, executioncontrol.ErrRequested)
	killed = killed || operatorCancelled
	if err == nil && res == nil {
		err = errors.New("runtime returned no result")
	}
	if err == nil && res.Retry {
		err = errors.New("agent asked to retry later")
	}

	if err == nil && !yielded && e.opt.Reviews != nil {
		err = e.opt.Reviews.AssertApproved(turnCtx, gid, n.ID, r.index)
	}

	// Publish before acknowledging any input. A partial publication cannot be
	// rolled back: retrying the turn may emit duplicates with new message IDs.
	if err == nil && !yielded {
		emits := append(env.drainEmits(), res.Emits...)
		var parentMsg *domain.Message
		if len(inputs) > 0 {
			parentMsg = inputs[0]
		}
		for _, em := range emits {
			ids, publishErr := e.emit(context.WithoutCancel(turnCtx), gr, n, r.index, x, parentMsg, em)
			x.OutputIDs = append(x.OutputIDs, ids...)
			if publishErr != nil {
				err = fmt.Errorf("publish turn outputs: %w", publishErr)
				break
			}
		}
	}

	if err != nil {
		observability.EndSpan(span, err)
		st := domain.ExecFailed
		delay := time.Duration(0)
		switch {
		case killed:
			st = domain.ExecCancelled
		case errors.Is(turnCtx.Err(), context.DeadlineExceeded):
			err = fmt.Errorf("turn timed out after %s: %w", n.Limits.TurnTimeout.D(), err)
		}
		for i, d := range dels {
			if st != domain.ExecCancelled {
				delay = backoffDelay(e.opt.RetryBackoff, d.Attempt())
			}
			ch := from[i].cfg
			if operatorCancelled || (st == domain.ExecFailed && ch != nil && d.Attempt() >= ch.MaxDeliver) {
				_ = d.Term(context.WithoutCancel(turnCtx), err.Error())
				e.recordMessage(turnCtx, gid, from[i].group.Channel, from[i].group.Group, d.Message(), domain.MsgDeadLettered, d.Attempt(), x.ID, err.Error())
				observability.MessagesConsumed.WithLabelValues(gid, n.ID, "dead").Inc()
				continue
			}
			_ = d.Nack(context.WithoutCancel(turnCtx), delay)
			e.recordMessage(turnCtx, gid, from[i].group.Channel, from[i].group.Group, d.Message(), domain.MsgNacked, d.Attempt(), x.ID, err.Error())
			observability.MessagesConsumed.WithLabelValues(gid, n.ID, "nacked").Inc()
		}
		// Only retryable failures restore operator messages.
		for _, op := range ops {
			if operatorCancelled {
				break
			}
			cp := *op
			cp.ID, cp.Consumed = domain.NewID("opm"), nil
			_ = e.store.PutOperatorMessage(context.WithoutCancel(turnCtx), &cp)
		}
		x.Usage = env.usage()
		x.ToolCalls = env.toolCalls()
		if res != nil {
			x.Usage.Add(domainUsage(res.Usage))
			e.reportExternalUsage(turnCtx, gid, n.ID, x.ID, res.Usage)
			x.ToolCalls += res.ToolCalls
		}
		finish(st, err.Error())
		if killed {
			return turnOutcome{}
		}
		return turnOutcome{err: err}
	}

	// Every output was published or durably held for approval.
	pubCtx := context.WithoutCancel(turnCtx)
	for i, d := range dels {
		if err := d.Ack(pubCtx); err != nil {
			e.log.Warn("ack failed", "err", err)
		}
		_, _ = e.store.MarkProcessed(pubCtx, from[i].group.Group, d.Message().ID)
		e.recordMessage(pubCtx, gid, from[i].group.Channel, from[i].group.Group, d.Message(), domain.MsgAcked, d.Attempt(), x.ID, "")
		observability.MessagesConsumed.WithLabelValues(gid, n.ID, "acked").Inc()
	}

	if n.Cluster.Comms == domain.CommsRing {
		line := strings.TrimSpace(res.Broadcast)
		if line == "" {
			line = "(no broadcast; " + truncate(res.Summary, 120) + ")"
		}
		e.putBroadcast(pubCtx, gid, n.ID, r.index, iter, line)
	}

	x.Summary = truncate(res.Summary, 4000)
	x.Signal = string(res.Signal)
	x.Usage = env.usage()
	x.Usage.Add(domainUsage(res.Usage))
	e.reportExternalUsage(turnCtx, gid, n.ID, x.ID, res.Usage)
	x.ToolCalls = env.toolCalls() + res.ToolCalls
	finish(domain.ExecSucceeded, "")
	span.SetAttributes(attribute.Int("weft.outputs", len(x.OutputIDs)))
	if yielded {
		r.mu.Lock()
		r.runIter-- // the suspended operation still needs its continuation turn
		r.pendStop = false
		r.mu.Unlock()
		return turnOutcome{}
	}

	// Auto-stop handshake: declaration, then confirmation on the next turn.
	if n.Loop.Stop.Policy == domain.StopAuto {
		r.mu.Lock()
		defer r.mu.Unlock()
		switch {
		case pendingStop && res.Signal == runtime.SignalConfirmStop:
			return turnOutcome{completed: true, reason: "agent confirmed there is no further work"}
		case res.Signal == runtime.SignalNoFurtherWork || res.Signal == runtime.SignalConfirmStop:
			r.pendStop = len(ops) == 0 // an operator leaning in withdraws it
		default:
			r.pendStop = false
		}
	}
	return turnOutcome{}
}

func nackAll(dels []queue.Delivery, delay time.Duration) {
	for _, d := range dels {
		_ = d.Nack(context.Background(), delay)
	}
}

func expiry(d domain.Duration) *time.Time {
	if d <= 0 {
		return nil
	}
	t := time.Now().Add(d.D())
	return &t
}

func (r *replica) extendLeases(ctx context.Context, dels []queue.Delivery, from []*sub) func() {
	if len(dels) == 0 {
		return func() {}
	}
	every := 5 * time.Minute
	for _, s := range from {
		if s.cfg != nil && s.cfg.AckWait.D()/3 < every {
			every = s.cfg.AckWait.D() / 3
		}
	}
	if every < time.Second {
		every = time.Second
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, d := range dels {
					_ = d.Extend(ctx)
				}
			}
		}
	}()
	return cancel
}

func (r *replica) buildInput(ctx context.Context, gr *graphRun, n *domain.Node, x *domain.Execution, inputs []*domain.Message, ops []*domain.OperatorMessage, pendingStop bool, p *tools.Principal) *runtime.TurnInput {
	e := gr.e
	r.mu.Lock()
	workdir, branch := r.workdir, r.branch
	r.mu.Unlock()
	in := &runtime.TurnInput{
		ExecutionID: x.ID, GraphID: gr.g.Metadata.ID, GraphName: gr.g.Metadata.Name,
		NodeID: n.ID, NodeName: n.DisplayName(), Replica: r.index, Replicas: gr.replicaCount(n.ID),
		Iteration: x.Iteration, Agent: n.Runtime.Agent,
		Prompt: graph.Substitute(n.Prompt, gr.params), Inputs: inputs,
		Model: n.Model, PendingStop: pendingStop, Workdir: workdir, Branch: branch,
		Parameters: gr.params, Config: n.Config, MaxToolCalls: n.Limits.MaxToolCalls,
	}
	if in.Inputs == nil {
		in.Inputs = []*domain.Message{}
	}
	for _, op := range ops {
		in.OperatorMessages = append(in.OperatorMessages, op.Text)
	}
	for _, c := range gr.g.Outgoing(n.ID) {
		in.Outputs = append(in.Outputs, runtime.OutputInfo{Channel: c.ID, Type: c.Type, Targets: gr.g.Consumers(c), Types: c.Types, When: c.When})
	}
	if in.Outputs == nil {
		in.Outputs = []runtime.OutputInfo{}
	}
	if g, ok := e.opt.Models.(interface{ ModelGuidance(provider string) string }); ok {
		in.ModelGuidance = g.ModelGuidance(n.Model.Provider)
	}
	if e.opt.Tools != nil {
		in.Tools = e.opt.Tools.Schemas(p)
	}
	if in.Tools == nil {
		in.Tools = []llm.ToolSchema{}
	}
	if n.Cluster.Comms == domain.CommsRing && in.Replicas > 1 {
		in.Neighbors = e.neighbours(ctx, gr.g.Metadata.ID, n.ID, r.index, in.Replicas)
	}
	in.System = buildSystem(gr.g, n, in, workspace.Scratch(workdir, n.ID, r.index))
	return in
}

// --- ring comms ---------------------------------------------------------

func ringNS(graphID, node string) string { return graphID + "/" + node + "/_ring" }

func (e *Engine) putBroadcast(ctx context.Context, graphID, node string, replica, iter int, line string) {
	b, _ := json.Marshal(runtime.NeighborNote{Replica: replica, Text: truncate(line, 500), At: time.Now().UTC().Format(time.RFC3339)})
	_ = e.store.KVPut(ctx, ringNS(graphID, node), fmt.Sprintf("r%d", replica), b)
	e.bus.Publish(ctx, domain.NewEvent(domain.EvAgentBroadcast, graphID, map[string]any{"text": line, "iteration": iter}).WithNode(node, replica))
}

// neighbours returns the latest lines of the two ring neighbours (radius 1,
// wrapping). Each replica writes only its own key, so there is nothing to
// lock and no read-modify-write to lose.
func (e *Engine) neighbours(ctx context.Context, graphID, node string, me, size int) []runtime.NeighborNote {
	all, err := e.store.KVList(ctx, ringNS(graphID, node), "r")
	if err != nil {
		return nil
	}
	want := map[string]bool{fmt.Sprintf("r%d", (me+size-1)%size): true, fmt.Sprintf("r%d", (me+1)%size): true}
	delete(want, fmt.Sprintf("r%d", me))
	var out []runtime.NeighborNote
	for k, v := range all {
		if !want[k] {
			continue
		}
		var nb runtime.NeighborNote
		if json.Unmarshal(v, &nb) == nil {
			out = append(out, nb)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Replica < out[j].Replica })
	return out
}

// --- turn environment ---------------------------------------------------

// turnEnv implements runtime.TurnEnv for one execution. It is the only
// door out of an agent: tools go through the gateway with this replica's
// grants, model calls through the LLM registry with this node's model.
type turnEnv struct {
	budgetRootID string
	executionCtx context.Context
	toolMu       sync.Mutex
	r            *replica
	gr           *graphRun
	n            *domain.Node
	x            *domain.Execution
	principal    *tools.Principal
	span         trace.SpanContext

	mu       sync.Mutex
	yielded  bool
	emits    []runtime.Emit
	use      domain.Usage
	calls    int
	children int
	// checkpointDue: the round's first workspace change records a checkpoint.
	checkpointDue bool
	// streamed is the last live model call whose text was shown, until the
	// next Output (guarded by mu).
	streamed *streamedText
}

func newTurnEnv(r *replica, gr *graphRun, n *domain.Node, x *domain.Execution) *turnEnv {
	r.mu.Lock()
	wd := r.workdir
	r.mu.Unlock()
	env := &turnEnv{budgetRootID: x.ID, r: r, gr: gr, n: n, x: x, principal: &tools.Principal{
		GraphVersion: gr.g.Metadata.Version, GraphID: gr.g.Metadata.ID, NodeID: n.ID, Replica: r.index, ExecutionID: x.ID,
		WorkspaceReadOnly: n.Workspace.ReadOnly, Grants: n.Tools, Checks: gr.g.Spec.Checks, BaseCommit: gr.g.Metadata.Labels[domain.LabelTaskBase], Workdir: wd, Sandbox: r.sandbox, Actor: fmt.Sprintf("agent:%s#%d", n.ID, r.index),
	}}
	if delegationRuntime(n.Runtime.Kind) {
		env.principal.Delegate = env.delegate
	}
	env.checkpointDue = true
	env.principal.BeforeMutation = env.beforeMutation
	env.principal.PrepareTool = func(ctx context.Context, name string) error {
		if name == "environment.status" || name == "environment.propose" || name == "delegation.request" {
			return nil
		}
		return env.ensureEnvironment(ctx)
	}
	return env
}

// SpanContext is the turn's span; callback requests from external runtimes
// are parented to it so their tool and model calls appear inside the turn.
func (t *turnEnv) SpanContext() trace.SpanContext { return t.span }
func (t *turnEnv) controlled(ctx context.Context) (context.Context, func(), error) {
	linked, cancel := context.WithCancelCause(ctx)
	stop := func() bool { return false }
	if t.executionCtx != nil {
		stop = context.AfterFunc(t.executionCtx, func() { cancel(context.Cause(t.executionCtx)) })
		if t.executionCtx.Err() != nil {
			cancel(context.Cause(t.executionCtx))
		}
	}
	cleanup := func() { stop(); cancel(context.Canceled) }
	if linked.Err() != nil {
		cleanup()
		return linked, func() {}, context.Cause(linked)
	}
	if err := (executioncontrol.Manager{Store: t.gr.e.store}).Check(linked, t.x.ID); err != nil {
		cleanup()
		return linked, func() {}, err
	}
	return linked, cleanup, nil
}

func (t *turnEnv) InvokeTool(ctx context.Context, name string, args json.RawMessage) (*tools.Result, error) {
	t.toolMu.Lock()
	defer t.toolMu.Unlock()
	ctx, done, controlErr := t.controlled(ctx)
	defer done()
	if controlErr != nil {
		return nil, controlErr
	}
	if err := t.countTool(); err != nil {
		return nil, err
	}
	if t.gr.e.opt.Tools == nil {
		return nil, tools.ErrUnknownTool
	}
	// A tool that needs approval parks the replica in awaiting_approval.
	if t.gr.e.opt.Tools.NeedsApproval(t.principal, name) {
		t.r.setState(domain.ReplicaAwaitingApproval, "waiting for approval: "+name)
		defer t.r.setState(domain.ReplicaRunning, "")
	}
	result, err := t.gr.e.opt.Tools.Invoke(ctx, t.principal, name, args)
	if err == nil && result != nil && result.Yield {
		t.mu.Lock()
		t.yielded = true
		t.mu.Unlock()
	}
	if err == nil && name == "environment.propose" && result != nil && !result.IsError {
		if err := t.ensureEnvironment(ctx); err != nil {
			return nil, err
		}
	}
	return result, err
}

type toolCallBudgetError int

func (e toolCallBudgetError) Error() string {
	return fmt.Sprintf("tool call budget of %d per turn exhausted", int(e))
}

func (t *turnEnv) countTool() error {
	t.mu.Lock()
	if t.yielded {
		t.mu.Unlock()
		return fmt.Errorf("turn yielded to human review")
	}
	t.calls++
	calls := t.calls
	t.mu.Unlock()
	if t.n.Limits.MaxToolCalls > 0 && calls > t.n.Limits.MaxToolCalls {
		return toolCallBudgetError(t.n.Limits.MaxToolCalls)
	}
	return nil
}

// Chat implements runtime.TurnEnv.
func (t *turnEnv) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return t.chat(ctx, req, false)
}

// ChatLive implements runtime.LiveChatEnv: Chat, with the answer's text
// published as live agent.output events while the provider streams it.
func (t *turnEnv) ChatLive(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return t.chat(ctx, req, true)
}

func (t *turnEnv) chat(ctx context.Context, req *llm.ChatRequest, live bool) (response *llm.ChatResponse, retErr error) {
	ctx, done, controlErr := t.controlled(ctx)
	defer done()
	if controlErr != nil {
		return nil, controlErr
	}
	t.mu.Lock()
	yielded := t.yielded
	t.mu.Unlock()
	if yielded {
		return nil, fmt.Errorf("turn yielded to human review")
	}
	t.armCheckpoint() // a model call starts a round
	e := t.gr.e
	if e.opt.Models == nil {
		return nil, errors.New("no model providers configured")
	}
	ref := t.n.Model
	if req.Model != "" {
		ref.Name = req.Model
	}
	prov, model, err := e.opt.Models.Resolve(ref)
	if err != nil {
		return nil, err
	}
	req.Model = model
	if req.Temperature == nil {
		req.Temperature = t.n.Model.Temperature
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = t.n.Model.MaxTokens
	}
	if t.n.Limits.MaxTokens > 0 {
		u := t.usage()
		if u.InputTokens+u.OutputTokens >= t.n.Limits.MaxTokens {
			return nil, fmt.Errorf("token budget of %d per turn exhausted", t.n.Limits.MaxTokens)
		}
	}
	if req.Metadata == nil {
		req.Metadata = map[string]string{}
	}
	req.Metadata["graph"], req.Metadata["node"], req.Metadata["execution"] = t.gr.g.Metadata.ID, t.n.ID, t.x.ID
	// Money is reserved first, so a call its budget refuses never marks the
	// token reservation dispatched.
	money, err := t.reserveMoney(ctx, prov.Name(), req)
	if err != nil {
		return nil, err
	}
	var reservation *tokenReservation
	if t.hasTreeBudget() {
		reservation, err = t.reserveTokens(ctx, req)
		if err != nil {
			return nil, errors.Join(err, money.Release(ctx))
		}
		defer func() {
			if e := reservation.settle(ctx); e != nil {
				retErr = errors.Join(retErr, fmt.Errorf("settle parent token budget: %w", e))
				response = nil
			}
		}()
		if err = reservation.dispatch(ctx); err != nil {
			return nil, errors.Join(err, money.Release(ctx))
		}
	}
	var resp *llm.ChatResponse
	if sm, ok := e.opt.Models.(StreamingModels); ok && live {
		out := t.newLiveOutput(ctx)
		resp, err = sm.ChatStream(ctx, prov.Name(), req, out.add)
		out.done(resp, err)
	} else {
		resp, err = e.opt.Models.Chat(ctx, prov.Name(), req)
	}
	if reservation != nil {
		reservation.report(resp)
	}
	if serr := t.settleMoney(ctx, money, resp); serr != nil {
		return nil, errors.Join(err, fmt.Errorf("settle task run budget: %w", serr))
	}
	if resp != nil {
		t.mu.Lock()
		t.use.Add(domainUsage(resp.Usage))
		t.mu.Unlock()
	}

	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (t *turnEnv) Emit(ctx context.Context, em runtime.Emit) error {
	ctx, done, controlErr := t.controlled(ctx)
	defer done()
	if controlErr != nil {
		return controlErr
	}
	t.mu.Lock()
	yielded := t.yielded
	t.mu.Unlock()
	if yielded {
		return fmt.Errorf("turn yielded to human review")
	}
	if strings.TrimSpace(em.Type) == "" {
		return errors.New("emit: type is required")
	}
	if len(em.Payload) == 0 {
		em.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(em.Payload) {
		return errors.New("emit: payload must be valid JSON")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.yielded {
		return fmt.Errorf("turn yielded to human review")
	}
	if len(t.emits) >= 1000 {
		return errors.New("emit: at most 1000 messages per turn")
	}
	t.emits = append(t.emits, em)
	return nil
}

func (t *turnEnv) drainEmits() []runtime.Emit {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.emits
	t.emits = nil
	return out
}

func (t *turnEnv) stateNS() string { return t.gr.g.Metadata.ID + "/" + t.n.ID }

func (t *turnEnv) StateGet(ctx context.Context, key string) ([]byte, error) {
	ctx, done, err := t.controlled(ctx)
	defer done()
	if err != nil {
		return nil, err
	}
	return t.gr.e.store.KVGet(ctx, t.stateNS(), key)
}

func (t *turnEnv) StatePut(ctx context.Context, key string, value []byte) error {
	ctx, done, controlErr := t.controlled(ctx)
	defer done()
	if controlErr != nil {
		return controlErr
	}
	if len(value) > 1<<20 {
		return errors.New("state values are limited to 1 MiB")
	}
	return t.gr.e.store.KVPut(ctx, t.stateNS(), key, value)
}

func (t *turnEnv) Output(ctx context.Context, text string) {
	ctx, done, err := t.controlled(ctx)
	defer done()
	if err != nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	data := map[string]any{"text": truncate(text, maxOutputText)}
	t.mu.Lock()
	// The streamed answer's text completes its live line.
	if t.streamed != nil && t.streamed.text == text {
		data["stream"] = t.streamed.id
	}
	t.streamed = nil
	t.mu.Unlock()
	t.gr.e.bus.Publish(ctx, domain.NewEvent(domain.EvAgentOutput, t.gr.g.Metadata.ID, data).
		WithNode(t.n.ID, t.r.index).WithExecution(t.x.ID))
}

// ContextWindow implements runtime.ContextEnv: the window of the provider
// the node's model resolves to (0 when unknown or unresolvable; the model
// call itself reports a resolution error).
func (t *turnEnv) ContextWindow(context.Context) int {
	if t.gr.e.opt.Models == nil {
		return 0
	}
	prov, _, err := t.gr.e.opt.Models.Resolve(t.n.Model)
	if err != nil {
		return 0
	}
	return llm.ContextWindowOf(prov)
}

// RecordContext implements runtime.ContextEnv.
func (t *turnEnv) RecordContext(ctx context.Context, ev runtime.ContextEvent) {
	ctx, done, err := t.controlled(ctx)
	defer done()
	if err != nil {
		return
	}
	t.gr.e.bus.Publish(ctx, domain.NewEvent(domain.EvAgentContext, t.gr.g.Metadata.ID, ev).
		WithNode(t.n.ID, t.r.index).WithExecution(t.x.ID))
}

// RecordPlan implements runtime.PlanEnv: the plan is an event of the
// execution, so the task view shows the latest one live and after the run.
func (t *turnEnv) RecordPlan(ctx context.Context, items []runtime.PlanItem) {
	ctx, done, err := t.controlled(ctx)
	defer done()
	if err != nil {
		return
	}
	t.gr.e.bus.Publish(ctx, domain.NewEvent(domain.EvAgentPlan, t.gr.g.Metadata.ID, map[string]any{"items": items}).
		WithNode(t.n.ID, t.r.index).WithExecution(t.x.ID))
}

func (t *turnEnv) AskHuman(ctx context.Context, question string) (string, error) {
	ctx, done, controlErr := t.controlled(ctx)
	defer done()
	if controlErr != nil {
		return "", controlErr
	}
	t.r.setState(domain.ReplicaAwaitingApproval, "asked a human")
	defer t.r.setState(domain.ReplicaRunning, "")
	a, err := t.gr.e.opt.Approvals.RequestApproval(ctx, &domain.Approval{
		GraphID: t.gr.g.Metadata.ID, NodeID: t.n.ID, Replica: t.r.index, ExecutionID: t.x.ID,
		Kind: domain.ApprovalQuestion, Title: truncate(question, 200),
		Detail: mustJSON(map[string]any{"question": question}),
	})
	if err != nil {
		return "", err
	}
	if a.Status != domain.ApprovalApproved {
		return "", fmt.Errorf("question was %s", a.Status)
	}
	return a.Answer, nil
}

func (t *turnEnv) usage() domain.Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.use
}

func (t *turnEnv) toolCalls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// --- execution registry (callback authentication) -------------------------

type execRegistry struct {
	mu   sync.Mutex
	envs map[string]*execEntry
}

type execEntry struct {
	env   *turnEnv
	token string
}

func newExecRegistry() *execRegistry { return &execRegistry{envs: map[string]*execEntry{}} }

func (x *execRegistry) register(id string, env *turnEnv) string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	tok := hex.EncodeToString(b[:])
	x.mu.Lock()
	x.envs[id] = &execEntry{env: env, token: tok}
	x.mu.Unlock()
	return tok
}

func (x *execRegistry) unregister(id string) {
	x.mu.Lock()
	delete(x.envs, id)
	x.mu.Unlock()
}

// ErrBadCallback is returned for unknown executions or wrong tokens.
var ErrBadCallback = errors.New("unknown execution or invalid token")

// Callback authenticates an external runtime's callback and returns the
// turn environment it may act through.
func (e *Engine) Callback(execID, token string) (runtime.TurnEnv, *tools.Principal, error) {
	e.execs.mu.Lock()
	ent := e.execs.envs[execID]
	e.execs.mu.Unlock()
	if ent == nil || subtle.ConstantTimeCompare([]byte(ent.token), []byte(token)) != 1 || (ent.env.executionCtx != nil && ent.env.executionCtx.Err() != nil) {
		return nil, nil, ErrBadCallback
	}
	return ent.env, ent.env.principal, nil
}

// ToolSchemas lists the tools granted to a principal (callback GET /tools).
func (e *Engine) ToolSchemas(p *tools.Principal) []llm.ToolSchema {
	if e.opt.Tools == nil {
		return nil
	}
	return e.opt.Tools.Schemas(p)
}

// domainUsage records a model call's usage on an execution.
func domainUsage(u llm.Usage) domain.Usage {
	return domain.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens}
}

// reportExternalUsage passes inference an external runtime did outside the
// LLM gateway to cost accounting, as its own (unpriced) record.
func (e *Engine) reportExternalUsage(ctx context.Context, graph, node, execution string, u llm.Usage) {
	reporter, ok := e.opt.Models.(interface {
		ReportExternal(context.Context, llm.Call)
	})
	if !ok || (u.InputTokens == 0 && u.OutputTokens == 0) {
		return
	}
	reporter.ReportExternal(context.WithoutCancel(ctx), llm.Call{Usage: u,
		Metadata: map[string]string{"graph": graph, "node": node, "execution": execution}})
}
