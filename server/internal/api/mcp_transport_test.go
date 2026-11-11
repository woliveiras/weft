package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestStatelessMCPConnectionAPIAndGateway(t *testing.T) {
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid RPC")
		}
		if r.Header.Get("MCP-Protocol-Version") != "2026-07-28" || r.Header.Get("Authorization") != "Bearer synthetic-stateless-secret" {
			t.Error("profile or credential lost")
		}
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch request.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "read", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "fixture read"}}}
		default:
			t.Error("unexpected lifecycle", request.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer upstream.Close()
	srv, a := setupApp(t, nil)
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, token, upstream.Client())
	}
	a.MCP.Call = func(ctx context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
		return mcpconfig.CallWithClient(ctx, endpoint, token, name, args, upstream.Client())
	}
	input := mcpconfig.Input{ID: "stateless", Name: "Stateless", Service: "generic", Endpoint: upstream.URL, Enabled: true, TransportProfile: "stateless-2026-07-28", Credential: &mcpconfig.Credential{Value: "synthetic-stateless-secret"}}
	raw, _ := json.Marshal(input)
	path := "/api/v1/mcp/connections"
	if c := call(t, srv, "POST", path, viewerTok, "application/json", string(raw), nil); c != 403 {
		t.Fatal(c)
	}
	var view mcpconfig.View
	if c := call(t, srv, "POST", path, adminTok, "application/json", string(raw), &view); c != 201 {
		t.Fatal(c)
	}
	if c := call(t, srv, "POST", path+"/stateless/tools/refresh", operatorTok, "", "", &view); c != 200 || len(view.Tools) != 1 {
		t.Fatal(c, view)
	}
	if view.TransportProfile != input.TransportProfile {
		t.Fatal("profile not exposed")
	}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = a.MCP.Resolve
	principal := &tools.Principal{GraphID: "test", NodeID: "reader"}
	if _, err := gateway.Invoke(t.Context(), principal, "mcp.stateless.read", json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing grant accepted")
	}
	if calls != 1 {
		t.Fatal("denied request reached upstream")
	}
	principal.Grants = []domain.ToolGrant{{Name: "mcp.stateless.read"}}
	if result, err := gateway.Invoke(t.Context(), principal, "mcp.stateless.read", json.RawMessage(`{}`)); err != nil || result.IsError {
		t.Fatal(result, err)
	}
	if calls != 2 {
		t.Fatal("unexpected protocol requests", calls)
	}
	_, _, body := rawCall(t, "GET", srv.URL+path, viewerTok, "", nil)
	if strings.Contains(body, "synthetic-stateless-secret") {
		t.Fatal("credential exposed")
	}
	input.ID = "invalid-profile"
	input.TransportProfile = "auto"
	raw, _ = json.Marshal(input)
	if c := call(t, srv, "POST", path, adminTok, "application/json", string(raw), nil); c != 422 {
		t.Fatal(c)
	}
}
