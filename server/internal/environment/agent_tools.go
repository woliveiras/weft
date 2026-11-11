package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

func decodeAgentArgs(args json.RawMessage, out any) error {
	if len(args) > 64<<10 {
		return fmt.Errorf("environment tool arguments exceed 64 KiB")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid environment tool arguments: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one argument object")
	}
	return nil
}
func (p *Proposals) AgentTools() []tools.Tool {
	return tools.EnvironmentTools(p.agentStatus, p.agentPropose)
}
func (p *Proposals) agentStatus(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	var args struct{}
	if err := decodeAgentArgs(inv.Args, &args); err != nil {
		return nil, err
	}
	g, err := p.Store.GetGraph(ctx, inv.Principal.GraphID, 0)
	if err != nil {
		return nil, err
	}
	if g.Node(inv.Principal.NodeID) == nil {
		return nil, tools.ErrDenied
	}
	v, err := p.Manager.Get(ctx, g)
	if err != nil {
		return nil, err
	}
	proposal, err := p.Get(ctx, g.Metadata.ID)
	if err != nil {
		return nil, err
	}
	// Do not expose build logs or arbitrary model rationale as status output.
	data, err := json.Marshal(map[string]any{"defined": v.Defined, "state": v.State, "upToDate": v.UpToDate, "proposal": proposalStatus(proposal)})
	if err != nil {
		return nil, err
	}
	return &tools.Result{Content: string(data), Data: data}, nil
}
func proposalStatus(p *Proposal) any {
	if p == nil {
		return nil
	}
	return map[string]any{"id": p.ID, "state": p.State, "error": p.Error}
}
func (p *Proposals) agentPropose(ctx context.Context, inv *tools.Invocation) (result *tools.Result, err error) {
	var a Analysis
	if err := decodeAgentArgs(inv.Args, &a); err != nil {
		return nil, err
	}
	g, err := p.Store.GetGraph(ctx, inv.Principal.GraphID, 0)
	if err != nil {
		return nil, err
	}
	v, err := p.RequestDefinition(ctx, g, &a, inv.Principal)
	if err != nil {
		return nil, err
	}
	defer func() {
		if ctx.Err() != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			active, e := p.Get(cleanup, g.Metadata.ID)
			if e != nil {
				err = fmt.Errorf("wait cancelled; read proposal: %w", e)
				return
			}
			if active != nil && active.ID == v.ID && active.State == "awaiting_approval" {
				if e := p.Cancel(cleanup, g.Metadata.ID, inv.Principal.Actor); e != nil {
					err = fmt.Errorf("wait cancelled; cancel proposal: %w", e)
				}
			}
		}
	}()
	decision, waitErr := p.Approvals.Wait(ctx, v.ID)
	if waitErr != nil {
		return nil, waitErr
	}
	if decision.Status != domain.ApprovalApproved {
		return nil, fmt.Errorf("environment proposal %s", decision.Status)
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		current, e := p.Get(ctx, g.Metadata.ID)
		if e != nil {
			return nil, e
		}
		if current == nil || current.ID != v.ID {
			return nil, fmt.Errorf("environment proposal was replaced")
		}
		switch current.State {
		case "ready":
			latest, e := p.Store.GetGraph(ctx, g.Metadata.ID, 0)
			if e != nil {
				return nil, e
			}
			if e = p.Check(ctx, latest); e != nil {
				return nil, e
			}
			return &tools.Result{Content: "Developer-approved environment built successfully. Continue in the prepared sandbox."}, nil
		case "failed", "rejected", "cancelled", "expired":
			return nil, fmt.Errorf("environment proposal %s: %s", current.State, current.Error)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}
