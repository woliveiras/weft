package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// CheckpointTools exposes the caller's worktree checkpoints (NAT-07). The
// scope (graph, node, replica, directory) always comes from the
// authenticated principal; only grants that name these tools enable them
// (see ExplicitGrantOnly).
func CheckpointTools(list, restore func(context.Context, *Invocation) (*Result, error)) []Tool {
	wrap := func(fn func(context.Context, *Invocation) (*Result, error)) func(context.Context, *Invocation) (*Result, error) {
		return func(ctx context.Context, inv *Invocation) (*Result, error) {
			if inv.Principal == nil || inv.Principal.GraphID == "" || inv.Principal.NodeID == "" || inv.Principal.Workdir == "" {
				return nil, ErrDenied
			}
			if fn == nil {
				return nil, fmt.Errorf("checkpoint service unavailable")
			}
			return fn(ctx, inv)
		}
	}
	return []Tool{
		&funcTool{name: "checkpoint.list", capability: "checkpoint.list",
			description: "List the checkpoints of your workspace, newest first. A checkpoint is recorded before the edits of each round.",
			schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), fn: wrap(list)},
		&funcTool{name: "checkpoint.restore", capability: "checkpoint.restore",
			description: "Restore your workspace files to a checkpoint: by default the newest one recorded before an edit, or the id from checkpoint.list. The current state is saved as a checkpoint first, so a restore can be undone. Ignored files, .weft/, the Git index, HEAD and the branch are not changed.",
			schema:      json.RawMessage(`{"type":"object","properties":{"checkpoint":{"type":"string","description":"Checkpoint id from checkpoint.list; omit for the newest checkpoint recorded before an edit."}},"additionalProperties":false}`), fn: wrap(restore)},
	}
}
