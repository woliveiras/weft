package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/tools"
	"io"
)

type requestTool struct{ manager *Manager }

func (m *Manager) Tool() tools.Tool       { return &requestTool{m} }
func (t *requestTool) Name() string       { return "review.request" }
func (t *requestTool) Capability() string { return "review.request" }
func (t *requestTool) Description() string {
	return "Freeze the current Docker workspace and request durable human review. Optionally start a managed HTTP application. Ends this turn; do not emit completion until the human decision arrives in a later turn. Checks are your reported evidence, not platform-verified tests."
}
func (t *requestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["title","instructions"],"additionalProperties":false,"properties":{"title":{"type":"string"},"instructions":{"type":"string"},"command":{"type":"array","items":{"type":"string"}},"cwd":{"type":"string"},"port":{"type":"integer","minimum":1024,"maximum":65535},"readyPath":{"type":"string"},"lifetimeSeconds":{"type":"integer","minimum":60,"maximum":7200},"checks":{"type":"array","items":{"type":"string"}},"externalUrl":{"type":"string"}}}`)
}
func (t *requestTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	var req Request
	d := json.NewDecoder(bytes.NewReader(inv.Args))
	d.DisallowUnknownFields()
	if e := d.Decode(&req); e != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, e)
	}
	var rest any
	if d.Decode(&rest) != io.EOF {
		return nil, ErrInvalid
	}
	p := inv.Principal
	if p == nil {
		return nil, tools.ErrDenied
	}
	if len(req.Command) > 0 {
		grant, e := tools.Authorize(p, &commandPermission{})
		if e != nil {
			return nil, e
		}
		if e = tools.CheckCommand(grant, req.Command); e != nil {
			return nil, e
		}
		if grant.RequiresApproval {
			if inv.Approver == nil {
				return nil, tools.ErrDenied
			}
			detail, _ := json.Marshal(map[string]any{"tool": "shell.exec", "argv": req.Command, "purpose": "managed preview"})
			decision, e := inv.Approver.RequestApproval(ctx, &domain.Approval{GraphID: p.GraphID, NodeID: p.NodeID, Replica: p.Replica, ExecutionID: p.ExecutionID, Kind: domain.ApprovalTool, Title: "Start preview command", Detail: detail})
			if e != nil {
				return nil, e
			}
			if decision.Status != domain.ApprovalApproved {
				return nil, tools.ErrRejected
			}
		}
	}
	g, e := t.manager.Store.GetGraph(ctx, p.GraphID, 0)
	if e != nil {
		return nil, e
	}
	graph.Normalize(g)
	r, e := t.manager.Create(ctx, g, p.NodeID, p.Replica, p.ExecutionID, p.Workdir, req)
	if e != nil {
		return nil, e
	}
	data, e := json.Marshal(r.View())
	if e != nil {
		return nil, e
	}
	return &tools.Result{Yield: true, Content: "Human review requested. This turn is complete; wait for the durable review result.", Data: data}, nil
}

type commandPermission struct{ requestTool }

func (*commandPermission) Name() string       { return "shell.exec" }
func (*commandPermission) Capability() string { return "shell.exec" }
