//go:build e2e

package main

import (
	"context"
	"encoding/json"

	"github.com/woliveiras/weft/server/internal/tools"
)

// trackerTool is a fixture GitHub MCP tool for issue trigger journeys on the
// connection "e2e-tracker". It never reaches a network.
type trackerTool struct{ name, content string }

func (t trackerTool) Name() string            { return "mcp.e2e-tracker." + t.name }
func (t trackerTool) Capability() string      { return t.Name() }
func (t trackerTool) Description() string     { return "E2E issue tracker fixture" }
func (t trackerTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t trackerTool) Invoke(context.Context, *tools.Invocation) (*tools.Result, error) {
	return &tools.Result{Content: t.content}, nil
}

func trackerTools() []tools.Tool {
	return []tools.Tool{
		trackerTool{"search_issues", `{"items":[{"number":11,"title":"Add CSV export","state":"open","html_url":"https://github.example/acme/app/issues/11","labels":[{"name":"weft"}],"assignees":[]},{"number":12,"title":"Waits on design","state":"open","labels":[{"name":"weft"},{"name":"blocked"}],"assignees":[]}]}`},
		trackerTool{"update_issue", `{}`},
		trackerTool{"add_issue_comment", `{}`},
	}
}
