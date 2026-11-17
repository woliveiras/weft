package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

// WorkspaceTarget identifies an authorized directory, not the process that
// writes it. Shared directories deliberately carry no node/execution identity.
type WorkspaceTarget struct {
	GraphID, NodeID, Dir string
	Replica              int
}

type monitoredWorkspace struct {
	snapshot workspaceSnapshot
	time     time.Time
}

// WorkspaceMonitor observes net changes independently of agent/tool lifetimes.
// Scan must be called serially. Targets are rediscovered without creating dirs.
type WorkspaceMonitor struct {
	Targets  func(context.Context) ([]WorkspaceTarget, error)
	Auditor  Auditor
	Interval time.Duration
	previous map[WorkspaceTarget]monitoredWorkspace
	gap      bool
}

func (m *WorkspaceMonitor) Scan(ctx context.Context) error {
	targets, err := m.Targets(ctx)
	if err != nil {
		if !m.gap {
			for target := range m.previous {
				m.emit(target, map[string]any{"source": "monitor", "attribution": "unknown", "status": "partial", "gap": true, "reason": "discovery_failed", "scope": "workspace_except_git_metadata", "observedAt": time.Now().UTC(), "changes": []workspaceChange{}})
			}
		}
		m.gap = true
		return fmt.Errorf("discover workspace monitoring targets: %w", err)
	}
	if m.previous == nil {
		m.previous = map[WorkspaceTarget]monitoredWorkspace{}
	}
	next := make(map[WorkspaceTarget]monitoredWorkspace, len(targets))
	for _, target := range targets {
		if ctx.Err() != nil {
			m.gap = true
			return ctx.Err()
		}
		if _, duplicate := next[target]; duplicate {
			continue
		}
		previous, known := m.previous[target]
		current := snapshotWorkspace(ctx, target.Dir)
		now := time.Now().UTC()
		changes, complete := diffWorkspace(previous.snapshot, current)
		status := "complete"
		if !complete {
			status = "partial"
		}
		baseline := !known
		if baseline {
			status = "baseline"
			if !current.complete {
				status = "partial"
			}
		}
		gap := baseline || m.gap || !previous.snapshot.complete || !current.complete
		if baseline || m.gap || len(changes) > 0 || previous.snapshot.complete != current.complete {
			data := map[string]any{"source": "monitor", "attribution": "unknown", "scope": "workspace_except_git_metadata", "changes": changes, "status": status, "gap": gap, "observedAt": now}
			if known {
				data["since"] = previous.time
			}
			m.emit(target, data)
		}
		next[target] = monitoredWorkspace{current, now}
	}
	m.previous = next
	m.gap = false
	return nil
}

func (m *WorkspaceMonitor) Run(ctx context.Context, log *slog.Logger) {
	interval := m.Interval
	if interval <= 0 {
		interval = time.Second
	}
	for {
		if err := m.Scan(ctx); err != nil && ctx.Err() == nil {
			log.Warn("workspace monitor scan failed", "error", err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *WorkspaceMonitor) emit(target WorkspaceTarget, data map[string]any) {
	ev := domain.NewEvent("workspace.observed", target.GraphID, data).WithActor("workspace-monitor")
	// Scope does not identify the author of a change.
	if target.NodeID != "" {
		ev.WithNode(target.NodeID, target.Replica)
	}
	m.Auditor.Audit(context.Background(), ev)
}
