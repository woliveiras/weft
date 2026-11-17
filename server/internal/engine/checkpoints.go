package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// Worktree checkpoints (NAT-07). Every model call starts a round; the first
// workspace-changing tool call of the round records a checkpoint of the
// replica's directory (skipped when nothing changed since the newest one).
// The gateway calls beforeMutation after authorization, so tools need not
// know about checkpoints. Failed turns never restore anything: the workspace
// and its checkpoints stay as they are for inspection.

// armCheckpoint marks the start of a round.
func (t *turnEnv) armCheckpoint() {
	t.mu.Lock()
	t.checkpointDue = true
	t.mu.Unlock()
}

// beforeMutation implements tools.Principal.BeforeMutation.
func (t *turnEnv) beforeMutation(ctx context.Context, tool string) {
	t.mu.Lock()
	due := t.checkpointDue
	t.checkpointDue = false
	t.mu.Unlock()
	if !due {
		return
	}
	t.gr.e.recordCheckpoint(ctx, t.gr.g, t.n.ID, t.r.index, t.principal.Workdir, t.principal.Actor,
		workspace.CheckpointMeta{ExecutionID: t.x.ID, Reason: workspace.CheckpointBeforeEdit, Tool: tool})
}

func (e *Engine) checkpointScope(g *domain.Graph, node string, replica int, dir string) (workspace.CheckpointScope, error) {
	base, err := e.opt.Workspaces.GraphDir(g)
	return workspace.CheckpointScope{GraphID: g.Metadata.ID, NodeID: node, Replica: replica, Dir: dir, Base: base}, err
}

// recordCheckpoint records before an edit. A failure is reported (event,
// metric, log) and never blocks the edit: the round runs without a
// checkpoint rather than not at all.
func (e *Engine) recordCheckpoint(ctx context.Context, g *domain.Graph, node string, replica int, dir, actor string, meta workspace.CheckpointMeta) {
	if e.opt.Workspaces == nil || dir == "" {
		return
	}
	start := time.Now()
	ctx, span := observability.Start(ctx, "checkpoint.record",
		attribute.String("weft.graph", g.Metadata.ID), attribute.String("weft.node", node),
		attribute.Int("weft.replica", replica), attribute.String("weft.execution", meta.ExecutionID))
	scope, err := e.checkpointScope(g, node, replica, dir)
	var cp *workspace.Checkpoint
	created := false
	if err == nil {
		cp, created, err = e.opt.Workspaces.RecordCheckpoint(ctx, scope, meta)
	}
	outcome := "unchanged"
	switch {
	case errors.Is(err, workspace.ErrNotRepository):
		// Not a failure: a plain folder has nothing to record into.
		outcome, err = "unavailable", nil
	case err != nil:
		outcome = "failed"
		e.log.Warn("checkpoint before edit failed", "graph", g.Metadata.ID, "node", node, "replica", replica, "execution", meta.ExecutionID, "tool", meta.Tool, "err", err)
		e.publishCheckpoint(ctx, domain.EvCheckpointFailed, g.Metadata.ID, node, replica, meta.ExecutionID, actor, map[string]any{
			"operation": "record", "reason": meta.Reason, "tool": meta.Tool, "error": err.Error()})
	case created:
		outcome = "created"
		e.publishCheckpoint(ctx, domain.EvCheckpointCreated, g.Metadata.ID, node, replica, meta.ExecutionID, actor, map[string]any{
			"checkpoint": cp.ID, "ref": cp.Ref, "commit": cp.Commit, "reason": cp.Reason, "tool": cp.Tool})
	}
	observability.WorkspaceCheckpoints.WithLabelValues("record", outcome).Inc()
	observability.WorkspaceCheckpointSecs.WithLabelValues("record").Observe(time.Since(start).Seconds())
	span.SetAttributes(attribute.String("weft.checkpoint.outcome", outcome))
	observability.EndSpan(span, err)
}

func (e *Engine) publishCheckpoint(ctx context.Context, typ domain.EventType, graphID, node string, replica int, execution, actor string, data map[string]any) {
	ev := domain.NewEvent(typ, graphID, data).WithNode(node, replica).WithTrace(observability.TraceID(ctx))
	if execution != "" {
		ev.WithExecution(execution)
	}
	if actor != "" {
		ev.WithActor(actor)
	}
	e.bus.Publish(context.WithoutCancel(ctx), ev)
}

// restoreCheckpoint restores a replica scope and reports it. Unknown
// checkpoints and scopes without a repository are ErrNotFound.
func (e *Engine) restoreCheckpoint(ctx context.Context, g *domain.Graph, node string, replica int, dir, id, source, actor string, meta workspace.CheckpointMeta) (res *workspace.RestoreResult, err error) {
	start := time.Now()
	ctx, span := observability.Start(ctx, "checkpoint.restore",
		attribute.String("weft.graph", g.Metadata.ID), attribute.String("weft.node", node),
		attribute.Int("weft.replica", replica), attribute.String("weft.checkpoint.source", source))
	outcome := "restored"
	defer func() {
		observability.WorkspaceCheckpoints.WithLabelValues("restore", outcome).Inc()
		observability.WorkspaceCheckpointSecs.WithLabelValues("restore").Observe(time.Since(start).Seconds())
		span.SetAttributes(attribute.String("weft.checkpoint.outcome", outcome))
		observability.EndSpan(span, err)
	}()
	scope, err := e.checkpointScope(g, node, replica, dir)
	if err == nil {
		res, err = e.opt.Workspaces.RestoreCheckpoint(ctx, scope, id, meta)
	}
	if errors.Is(err, workspace.ErrCheckpointNotFound) || errors.Is(err, workspace.ErrNotRepository) {
		outcome = "rejected"
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if err != nil {
		outcome = "failed"
		data := map[string]any{"operation": "restore", "checkpoint": id, "source": source, "error": err.Error()}
		if res != nil {
			// Files may be partly restored; the saved checkpoint holds the state before.
			data["saved"], data["written"], data["removed"] = res.Saved.ID, res.Written, res.Removed
		}
		e.log.Warn("checkpoint restore failed", "graph", g.Metadata.ID, "node", node, "replica", replica, "checkpoint", id, "source", source, "err", err)
		e.publishCheckpoint(ctx, domain.EvCheckpointFailed, g.Metadata.ID, node, replica, meta.ExecutionID, actor, data)
		return nil, err
	}
	e.publishCheckpoint(ctx, domain.EvCheckpointRestored, g.Metadata.ID, node, replica, meta.ExecutionID, actor, map[string]any{
		"checkpoint": res.Restored.ID, "saved": res.Saved.ID, "written": res.Written, "removed": res.Removed, "source": source})
	return res, nil
}

// ListCheckpoints lists a graph's checkpoints, newest first.
func (e *Engine) ListCheckpoints(ctx context.Context, graphID string) ([]workspace.Checkpoint, error) {
	g, _, err := e.GetGraph(ctx, graphID, 0)
	if err != nil {
		return nil, err
	}
	base, err := e.opt.Workspaces.GraphDir(g)
	if err != nil {
		return nil, err
	}
	return e.opt.Workspaces.GraphCheckpoints(ctx, base, g.Metadata.ID)
}

// RestoreCheckpoint restores a replica's workspace for a person: checkpoint
// id, or the newest checkpoint recorded before an edit of node/replica. It
// is refused while that replica runs a turn on this instance.
func (e *Engine) RestoreCheckpoint(ctx context.Context, graphID, node string, replica int, id, actor string) (*workspace.RestoreResult, error) {
	if id != "" {
		n, rep, _, err := workspace.ParseCheckpointID(id)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if node != "" && (node != n || replica != rep) {
			return nil, fmt.Errorf("%w: checkpoint %s belongs to %s replica %d", ErrInvalid, id, n, rep)
		}
		node, replica = n, rep
	}
	if node == "" || replica < 0 {
		return nil, fmt.Errorf("%w: a checkpoint or a node and replica is required", ErrInvalid)
	}
	g, _, err := e.GetGraph(ctx, graphID, 0)
	if err != nil {
		return nil, err
	}
	n := g.Node(node)
	if n == nil {
		return nil, fmt.Errorf("%w: node %s", ErrNotFound, node)
	}
	if e.replicaInTurn(graphID, node, replica) {
		return nil, fmt.Errorf("%w: %s replica %d is running a turn; stop or wait for it before restoring", ErrConflict, node, replica)
	}
	dir, err := e.opt.Workspaces.ReplicaDir(g, n, replica)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: workspace of %s replica %d", ErrNotFound, node, replica)
	} else if err != nil {
		return nil, err
	}
	return e.restoreCheckpoint(ctx, g, node, replica, dir, id, "api", actor, workspace.CheckpointMeta{Tool: "api"})
}

func (e *Engine) replicaInTurn(graphID, node string, replica int) bool {
	gr := e.graphRun(graphID)
	if gr == nil {
		return false
	}
	gr.mu.Lock()
	r := gr.replicas[node+"#"+itoa(replica)]
	gr.mu.Unlock()
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.turnCancel != nil
}

// CheckpointTools are the agent's checkpoint.list and checkpoint.restore,
// scoped to the calling replica. Register them in the tool registry.
func (e *Engine) CheckpointTools() []tools.Tool {
	return tools.CheckpointTools(e.agentListCheckpoints, e.agentRestoreCheckpoint)
}

func (e *Engine) agentScope(ctx context.Context, p *tools.Principal) (*domain.Graph, workspace.CheckpointScope, error) {
	if e.opt.Workspaces == nil {
		return nil, workspace.CheckpointScope{}, errors.New("checkpoints are unavailable: no workspace manager")
	}
	g, _, err := e.GetGraph(ctx, p.GraphID, p.GraphVersion)
	if err != nil {
		return nil, workspace.CheckpointScope{}, err
	}
	scope, err := e.checkpointScope(g, p.NodeID, p.Replica, p.Workdir)
	return g, scope, err
}

func (e *Engine) agentListCheckpoints(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	p := inv.Principal
	_, scope, err := e.agentScope(ctx, p)
	if err != nil {
		return nil, err
	}
	list, err := e.opt.Workspaces.Checkpoints(ctx, scope)
	if errors.Is(err, workspace.ErrNotRepository) {
		return &tools.Result{Content: "Your workspace is not a Git repository, so it has no checkpoints.", IsError: true}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return &tools.Result{Content: "No checkpoints yet. One is recorded before the edits of each round.", Data: json.RawMessage(`[]`)}, nil
	}
	var b strings.Builder
	for _, c := range list {
		fmt.Fprintf(&b, "%s  %s  %s", c.ID, c.CreatedAt.Format(time.RFC3339), c.Reason)
		if c.Tool != "" {
			fmt.Fprintf(&b, " (before %s)", c.Tool)
		}
		if c.ExecutionID == p.ExecutionID {
			b.WriteString("  this turn")
		}
		b.WriteString("\n")
	}
	data, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	return &tools.Result{Content: b.String(), Data: data}, nil
}

func (e *Engine) agentRestoreCheckpoint(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	var args struct {
		Checkpoint string `json:"checkpoint"`
	}
	if err := json.Unmarshal(inv.Args, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	p := inv.Principal
	g, _, err := e.agentScope(ctx, p)
	if err != nil {
		return nil, err
	}
	res, err := e.restoreCheckpoint(ctx, g, p.NodeID, p.Replica, p.Workdir, args.Checkpoint, "agent", p.Actor,
		workspace.CheckpointMeta{ExecutionID: p.ExecutionID, Tool: "checkpoint.restore"})
	if err != nil {
		return nil, err
	}
	content := fmt.Sprintf("Restored checkpoint %s: %d files written, %d removed. The state before the restore is checkpoint %s.",
		res.Restored.ID, res.Written, res.Removed, res.Saved.ID)
	data, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	return &tools.Result{Content: content, Data: data}, nil
}
