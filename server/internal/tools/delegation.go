package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// The gateway checks grants before this execution-scoped callback is reachable.
func delegationTools() []Tool {
	return []Tool{&funcTool{name: "delegation.request", capability: "delegation.request",
		description: "Request an approved analysis child using this agent's model and the profile-selected subset of explicit parent capabilities. Follows installation approval policy and returns the child's result. Requires an explicit delegation.request grant.",
		schema:      json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"template":{"type":"string","pattern":"^[a-z0-9][a-z0-9-]{0,62}$"},"objective":{"type":"string","maxLength":16384}},"required":["template","objective"]}`),
		fn: func(ctx context.Context, inv *Invocation) (*Result, error) {
			exact := false
			for _, grant := range inv.Principal.Grants {
				if grant.Name == "delegation.request" {
					exact = true
				}
			}
			if !exact || inv.Principal.Delegate == nil {
				return nil, fmt.Errorf("%w: delegation requires an explicit grant and an active supported parent", ErrDenied)
			}
			return inv.Principal.Delegate(ctx, inv.Args)
		}}}
}
