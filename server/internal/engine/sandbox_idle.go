package engine

import (
	"context"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
)

type resumableSandbox interface {
	Suspend(context.Context) error
	Start(context.Context) error
}

// Durable review holds no active turn. Its application lives in a separate
// preview sandbox, so the agent's container can release its capacity now.
func (r *replica) suspendForReview(ctx context.Context) {
	sb, ok := r.sandbox.(resumableSandbox)
	if !ok {
		return
	}
	r.mu.Lock()
	skip := r.sandboxSuspended || time.Now().Before(r.suspendRetry)
	r.mu.Unlock()
	if skip {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := r.sandboxTransition(bounded, "suspend", sb.Suspend)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.sandboxSuspended = true
	} else {
		r.suspendRetry = time.Now().Add(30 * time.Second)
	}
}

// Only the replica loop invokes lifecycle transitions, never a timer racing an
// active runtime. Waiting for approvals/tool calls remains part of the turn.
func (r *replica) sandboxIdleDelay() time.Duration {
	if _, ok := r.sandbox.(resumableSandbox); !ok {
		return -1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	timeout := r.run_.e.opt.SandboxIdleTimeout
	if timeout <= 0 || r.sandboxSuspended || r.idleSince.IsZero() ||
		(r.state != domain.ReplicaIdle && r.state != domain.ReplicaPaused) {
		return -1
	}
	deadline := r.idleSince.Add(timeout)
	if r.suspendRetry.After(deadline) {
		deadline = r.suspendRetry
	}
	delay := time.Until(deadline)
	if delay < 0 {
		return 0
	}
	return delay
}

func (r *replica) suspendIdle(ctx context.Context) {
	if r.sandboxIdleDelay() != 0 || ctx.Err() != nil {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := r.sandboxTransition(bounded, "suspend", r.sandbox.(resumableSandbox).Suspend)
	r.mu.Lock()
	if err == nil {
		r.sandboxSuspended = true
	} else {
		r.suspendRetry = time.Now().Add(30 * time.Second)
	}
	state := r.state
	r.mu.Unlock()
	if err == nil {
		r.setState(state, "sandbox suspended; workspace preserved")
	}
}

func (r *replica) resumeSandbox(ctx context.Context) error {
	r.mu.Lock()
	// A failed suspension can still have stopped the container (for example,
	// its final inspection or capacity release failed). Reconcile before work.
	suspended := r.sandboxSuspended || !r.suspendRetry.IsZero()
	r.mu.Unlock()
	if !suspended {
		return nil
	}
	r.setState(domain.ReplicaRunning, "waiting for sandbox capacity to resume")
	err := r.sandboxTransition(ctx, "resume", r.sandbox.(resumableSandbox).Start)
	if err == nil {
		r.mu.Lock()
		r.sandboxSuspended = false
		r.suspendRetry = time.Time{}
		r.mu.Unlock()
		r.setState(domain.ReplicaRunning, "sandbox resumed; workspace preserved")
	}
	return err
}

func (r *replica) sandboxTransition(ctx context.Context, action string, fn func(context.Context) error) error {
	ctx, span := observability.Start(ctx, "sandbox."+action)
	started := time.Now()
	err := fn(ctx)
	observability.EndSpan(span, err)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	observability.SandboxLifecycle.WithLabelValues(action, outcome).Inc()
	observability.SandboxLifecycleSeconds.WithLabelValues(action).Observe(time.Since(started).Seconds())
	gr := r.run_l()
	data := map[string]any{"action": action, "outcome": outcome, "workspacePreserved": true}
	if err != nil {
		data["error"] = err.Error()
		gr.e.log.Warn("sandbox lifecycle failed", "graph", gr.g.Metadata.ID, "node", r.nodeID, "replica", r.index, "action", action, "err", err)
	} else {
		gr.e.log.Info("sandbox lifecycle", "graph", gr.g.Metadata.ID, "node", r.nodeID, "replica", r.index, "action", action)
	}
	gr.e.bus.Publish(context.WithoutCancel(ctx), domain.NewEvent("sandbox.lifecycle", gr.g.Metadata.ID, data).WithNode(r.nodeID, r.index))
	return err
}
