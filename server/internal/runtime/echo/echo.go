// Package echo is a runtime that calls no model: every input is forwarded
// to the node's outputs unchanged (type and payload), and a source node
// with no inputs emits one "tick" per turn. It exercises the whole machinery
// - queues, routing, clusters, approvals, persistence, observability - at
// zero cost, which makes it the way to test a topology before paying for
// it.
package echo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/runtime"
)

type Runtime struct{}

func New() *Runtime { return &Runtime{} }

func (Runtime) Kind() domain.RuntimeKind                              { return domain.RuntimeEcho }
func (Runtime) Prepare(context.Context, runtime.ReplicaContext) error { return nil }
func (Runtime) Release(context.Context, runtime.ReplicaContext) error { return nil }

func (Runtime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	res := &runtime.TurnResult{}
	// Config "type" renames forwarded messages, "fail" makes the turn fail
	// (for testing retries and dead-lettering).
	if f, _ := in.Config["fail"].(bool); f {
		return nil, fmt.Errorf("echo: configured to fail")
	}
	typ, _ := in.Config["type"].(string)
	for _, m := range in.Inputs {
		t := m.Type
		if typ != "" {
			t = typ
		}
		if len(in.Outputs) > 0 {
			if err := env.Emit(ctx, runtime.Emit{Type: t, Payload: m.Payload}); err != nil {
				return nil, err
			}
		}
	}
	for _, op := range in.OperatorMessages {
		env.Output(ctx, "operator said: "+op)
	}
	if len(in.Inputs) == 0 && len(in.Outputs) > 0 {
		t := typ
		if t == "" {
			t = "tick"
		}
		payload, _ := json.Marshal(map[string]any{"iteration": in.Iteration, "node": in.NodeID, "replica": in.Replica})
		if err := env.Emit(ctx, runtime.Emit{Type: t, Payload: payload}); err != nil {
			return nil, err
		}
	}
	if in.PendingStop {
		res.Signal = runtime.SignalConfirmStop
	} else if s, _ := in.Config["stopAfter"].(float64); s > 0 && float64(in.Iteration) >= s {
		res.Signal = runtime.SignalNoFurtherWork
	}
	res.Summary = fmt.Sprintf("echoed %d message(s)", len(in.Inputs))
	res.Broadcast = res.Summary
	env.Output(ctx, res.Summary)
	return res, nil
}
