package environment

import (
	"context"
	"sync"
)

// Build progress is separate from Status, whose immutable snapshot is read by
// the log flusher. Never infer lifecycle state from untrusted build output.
type progressKey struct{}
type buildProgress struct {
	mu       sync.Mutex
	phase    string
	decision string
}

func progressContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, progressKey{}, &buildProgress{phase: "preparing"})
}
func setProgress(ctx context.Context, phase, decision string) {
	if p, ok := ctx.Value(progressKey{}).(*buildProgress); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		if phase != "" {
			p.phase = phase
		}
		if decision != "" {
			p.decision = decision
		}
	}
}
func snapshotProgress(ctx context.Context, st *Status) {
	if p, ok := ctx.Value(progressKey{}).(*buildProgress); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		st.Phase, st.ReuseDecision = p.phase, p.decision
	}
}
func buildReasons(cur *View, force bool) []string {
	if force {
		return []string{"explicit_rebuild"}
	}
	if len(cur.ChangeReasons) > 0 {
		return append([]string(nil), cur.ChangeReasons...)
	}
	if cur.State == StateFailed {
		return []string{"retry_failed"}
	}
	return []string{"not_built"}
}
