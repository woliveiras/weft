package mcpconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/tools"
)

// Available reads the current catalogue; callers must not retain it as authority.
func (m *Manager) Available(ctx context.Context) ([]tools.Tool, error) {
	views, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []tools.Tool{}
	for _, v := range views {
		if v.Enabled && v.Status == "ready" {
			for _, t := range v.Tools {
				out = append(out, &remoteTool{m: m, connection: v.ID, revision: v.Revision, tool: t})
			}
		}
	}
	return out, nil
}
func (m *Manager) Resolve(ctx context.Context, name string) (tools.Tool, error) {
	if !strings.HasPrefix(name, "mcp.") {
		return nil, nil
	}
	parts := strings.Split(name, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: invalid MCP tool", tools.ErrDenied)
	}
	v, err := m.Get(ctx, parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: MCP connection unavailable", tools.ErrDenied)
	}
	if v.Enabled && v.Status == "ready" {
		for _, t := range v.Tools {
			if t.Name == parts[2] {
				return &remoteTool{m: m, connection: v.ID, revision: v.Revision, tool: t}, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: MCP tool unavailable", tools.ErrDenied)
}

type remoteTool struct {
	m          *Manager
	connection string
	revision   int64
	tool       Tool
}

func (t *remoteTool) Name() string            { return "mcp." + t.connection + "." + t.tool.Name }
func (t *remoteTool) Capability() string      { return t.Name() }
func (t *remoteTool) Description() string     { return t.tool.Description }
func (t *remoteTool) Schema() json.RawMessage { return t.tool.InputSchema }
func (t *remoteTool) Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error) {
	// Recheck after human approval or environment preparation. Never use a stale endpoint/token.
	if inv == nil {
		return nil, tools.ErrDenied
	}
	grant, err := tools.Authorize(inv.Principal, t)
	if err != nil {
		return nil, err
	}
	r, err := t.m.load(ctx, t.connection)
	if err != nil || !r.Enabled || r.Status != "ready" || r.Revision != t.revision {
		return nil, fmt.Errorf("%w: MCP connection changed; retry with current catalogue", tools.ErrDenied)
	}
	if blockedDispatcher(r.View, t.tool.Name) {
		return nil, fmt.Errorf("%w: select an individual Atlassian operation instead of a dispatcher", tools.ErrDenied)
	}
	if len(grant.Paths) > 0 || len(grant.Commands) > 0 {
		return nil, fmt.Errorf("%w: workspace constraints cannot authorize remote MCP operations", tools.ErrDenied)
	}
	if r.LocalProcess != nil {
		if !t.m.AllowLocalProcesses || len(grant.Hosts) > 0 {
			return nil, fmt.Errorf("%w: local MCP process unavailable or incompatible host constraint", tools.ErrDenied)
		}
	} else {
		u, err := url.Parse(r.Endpoint)
		if err != nil {
			return nil, tools.ErrDenied
		}
		if err = tools.CheckHost(grant, u.Hostname()); err != nil {
			return nil, err
		}
	}
	var obj map[string]json.RawMessage
	if len(inv.Args) > 64<<10 || json.Unmarshal(inv.Args, &obj) != nil || obj == nil {
		return nil, fmt.Errorf("invalid MCP arguments: bounded JSON object required")
	}
	if !argumentPolicyAllows(r.ArgumentPolicy, t.tool, obj) {
		return nil, fmt.Errorf("%w: arguments are outside the connection policy", tools.ErrDenied)
	}
	if len(r.ArgumentPolicy) > 0 {
		// Forward exactly the object checked above, including duplicate-key normalization.
		normalized, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		copy := *inv
		copy.Args = normalized
		inv = &copy
	}
	if !atlassianPolicyArguments(r.View, t.tool, obj) {
		return nil, fmt.Errorf("%w: operation or cloud ID is outside the connection policy", tools.ErrDenied)
	}
	var token string
	r, token, err = t.m.accessToken(ctx, r)
	if err != nil {
		return nil, err
	}
	// Renewal may have advanced the revision; recheck local revocation before dispatch.
	current, err := t.m.load(ctx, r.ID)
	if err != nil || current.Revision != r.Revision || !current.Enabled || current.Status != "ready" {
		return nil, fmt.Errorf("%w: MCP connection changed", tools.ErrDenied)
	}
	if err = t.m.admitCall(ctx, current); err != nil {
		return nil, err
	}
	callctx, cancel := context.WithTimeout(credentialContext(ctx, r.View), 15*time.Second)
	defer cancel()
	raw, err := t.m.callConnection(callctx, r, token, t.tool.Name, inv.Args)
	if err != nil {
		if saveErr := t.m.noteOAuthUnauthorized(ctx, r, err); saveErr != nil {
			return nil, saveErr
		}
		if guidance := accessGuidance(err); guidance != "" {
			return nil, fmt.Errorf("%s Remote outcome may be unknown; no automatic retry.", guidance)
		}
		return nil, fmt.Errorf("MCP call failed; remote outcome may be unknown; no automatic retry")
	}
	if len(raw) > maxBody || containsTransportCredential(callctx, raw, token) {
		return nil, fmt.Errorf("MCP result rejected")
	}
	var result struct {
		Content    []json.RawMessage `json:"content"`
		IsError    bool              `json:"isError"`
		Structured json.RawMessage   `json:"structuredContent"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Content == nil {
		return nil, fmt.Errorf("invalid MCP tool result")
	}
	// Preserve all content block types as bounded JSON text; never dereference resource links.
	for _, block := range result.Content {
		var b struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(block, &b) != nil || b.Type == "" {
			return nil, fmt.Errorf("invalid MCP content block")
		}
	}
	payload, _ := json.Marshal(struct {
		Content    []json.RawMessage `json:"content"`
		Structured json.RawMessage   `json:"structuredContent,omitempty"`
	}{result.Content, result.Structured})
	return &tools.Result{Content: string(payload), IsError: result.IsError}, nil
}

// Decode JSON string escapes before checking credential echoes.
func containsCredential(raw json.RawMessage, token string) bool {
	if token == "" {
		return false
	}
	if strings.Contains(string(raw), token) {
		return true
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	var contains func(any) bool
	contains = func(v any) bool {
		switch x := v.(type) {
		case string:
			return strings.Contains(x, token)
		case []any:
			for _, item := range x {
				if contains(item) {
					return true
				}
			}
		case map[string]any:
			for k, item := range x {
				if strings.Contains(k, token) || contains(item) {
					return true
				}
			}
		}
		return false
	}
	return contains(v)
}
