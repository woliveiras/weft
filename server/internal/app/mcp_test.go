package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestMCPModelSchemasAndApprovalFollowCurrentCatalogue(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m := mcpconfig.New(store, nil, nil)
	in := mcpconfig.Input{ID: "team", Name: "Team", Service: "generic", Endpoint: "https://example.com/mcp", Enabled: true}
	if _, err = m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	m.Discover = func(context.Context, string, string) ([]mcpconfig.Tool, error) {
		return []mcpconfig.Tool{{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
	}
	v, err := m.Refresh(t.Context(), "team", "admin")
	if err != nil {
		t.Fatal(err)
	}
	adapter := toolGateway{reg: tools.NewRegistry(), mcp: m}
	p := &tools.Principal{Grants: []domain.ToolGrant{{Name: "*"}}}
	if len(adapter.Schemas(p)) != 0 {
		t.Fatal("wildcard exposed MCP")
	}
	p.Grants = []domain.ToolGrant{{Name: "mcp.team.search", RequiresApproval: true}}
	if schemas := adapter.Schemas(p); len(schemas) != 1 || schemas[0].Name != "mcp.team.search" {
		t.Fatal(schemas)
	}
	if !adapter.NeedsApproval(p, "mcp.team.search") {
		t.Fatal("missing approval")
	}
	in.Enabled = false
	if _, err = m.Put(t.Context(), in, v.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	if len(adapter.Schemas(p)) != 0 || adapter.NeedsApproval(p, "mcp.team.search") {
		t.Fatal("disabled connection remains offered")
	}
}
