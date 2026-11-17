package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// EnvironmentTools delegates lifecycle operations to the environment service;
// graph identity always comes from the authenticated invocation principal.
func EnvironmentTools(status, propose func(context.Context, *Invocation) (*Result, error)) []Tool {
	wrap := func(fn func(context.Context, *Invocation) (*Result, error)) func(context.Context, *Invocation) (*Result, error) {
		return func(ctx context.Context, inv *Invocation) (*Result, error) {
			if inv.Principal == nil || inv.Principal.GraphID == "" || inv.Principal.NodeID == "" {
				return nil, ErrDenied
			}
			if fn == nil {
				return nil, fmt.Errorf("environment service unavailable")
			}
			return fn(ctx, inv)
		}
	}
	return []Tool{
		&funcTool{name: "environment.status", capability: "environment.status", description: "Read your graph's environment readiness and proposal state. No build or approval is performed.", schema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), fn: wrap(status)},
		&funcTool{name: "environment.propose", capability: "environment.propose", description: "Propose a complete environment when no repository definition or accepted environment exists. Requires developer approval. Waits for approval and build before returning; rejection and timeout are errors. Use environment.status to inspect progress. Cannot choose another graph or approve its own proposal.", schema: json.RawMessage(`{"type":"object","properties":{"environment":{"type":"object","description":"Exactly one image or inline dockerfile, optional setup, caches, target and network. No origin, context, CPU, memory or privileges."},"rationale":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}}},"required":["environment","rationale","tools"],"additionalProperties":false}`), fn: wrap(propose)},
	}
}
