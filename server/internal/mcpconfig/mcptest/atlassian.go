package mcptest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

// AtlassianFlatServer exercises reviewed flat-list routing without vendor accounts.
type AtlassianFlatServer struct {
	*httptest.Server
	Drift          atomic.Bool
	Fail           atomic.Bool
	Calls          atomic.Int32
	EchoCredential atomic.Bool
}

func NewAtlassianFlat() *AtlassianFlatServer {
	f := &AtlassianFlatServer{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "tools=all" {
			http.Error(w, "flat option required", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-mcp-token" && r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("fixture@example.test:fixture-mcp-token")) {
			w.WriteHeader(401)
			return
		}
		if f.Fail.Load() {
			w.WriteHeader(503)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		var q struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			w.WriteHeader(400)
			return
		}
		var result any
		schema := map[string]any{"type": "object", "properties": map[string]any{"cloudId": map[string]string{"type": "string"}}}
		switch q.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "flat-session")
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			w.WriteHeader(202)
			return
		case "tools/list":
			if r.Header.Get("Mcp-Session-Id") != "flat-session" {
				w.WriteHeader(400)
				return
			}
			if q.Params["cursor"] == nil {
				result = map[string]any{"tools": []any{map[string]any{"name": "getJiraIssue", "inputSchema": schema}, map[string]any{"name": "executeRead", "inputSchema": schema}, map[string]any{"name": "executeWrite", "inputSchema": schema}, map[string]any{"name": "executeDestructive", "inputSchema": schema}}, "nextCursor": "flat-next"}
			} else if q.Params["cursor"] == "flat-next" {
				name := "getConfluenceContent"
				if f.Drift.Load() {
					name = "listJiraProjects"
				}
				result = map[string]any{"tools": []any{map[string]any{"name": name, "inputSchema": schema}}}
			} else {
				w.WriteHeader(400)
				return
			}
		case "tools/call":
			f.Calls.Add(1)
			text := "Flat operation completed"
			if f.EchoCredential.Load() {
				text = "fixture@example.test fixture-mcp-token"
			}
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}}
		default:
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	return f
}
