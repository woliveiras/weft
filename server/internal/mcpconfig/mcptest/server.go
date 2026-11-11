// Package mcptest supplies a real TLS MCP fixture for integration/browser tests.
package mcptest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
)

func New() *httptest.Server { return newServer(false, nil, false, 0, "") }

// NewRecovering expires every first session before dispatch to exercise recovery.
func NewRecovering() *httptest.Server { return newServer(true, nil, false, 0, "") }
func NewWithHeaders(headers map[string]string) *httptest.Server {
	return newServer(false, headers, false, 0, "")
}
func NewIssues() *httptest.Server { return newServer(false, nil, true, 0, "") }

// NewAccessFailure returns a real TLS response with deliberately hostile error
// content. Tests must prove that only the status reaches platform diagnostics.
func NewAccessFailure(status int, method string) *httptest.Server {
	return newServer(false, nil, false, status, method)
}
func newServer(recovering bool, headers map[string]string, issues bool, status int, failureMethod string) *httptest.Server {
	var sessions atomic.Int64
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		valid := true
		if headers == nil {
			valid = r.Header.Get("Authorization") == "Bearer fixture-mcp-token"
		} else {
			for name, value := range headers {
				if r.Header.Get(name) != value {
					valid = false
				}
			}
			if _, ok := headers["Langsmith-Api-Key"]; ok && r.Header.Get("Authorization") != "" {
				valid = false
			}
		}
		if !valid {
			http.Error(w, "fixture authentication required", 401)
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
		if q.Method == failureMethod {
			w.Header().Set("WWW-Authenticate", `Bearer error_description="fixture-mcp-token private-detail"`)
			w.Header().Set("Location", "https://example.invalid/fixture-mcp-token")
			http.Error(w, "fixture-mcp-token private-detail", status)
			return
		}
		var result any
		switch q.Method {
		case "initialize":
			session := "fixture-session"
			if recovering {
				session = strconv.FormatInt(sessions.Add(1), 10)
			}
			w.Header().Set("Mcp-Session-Id", session)
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			if recovering {
				n, _ := strconv.Atoi(r.Header.Get("Mcp-Session-Id"))
				if n%2 == 1 {
					w.WriteHeader(404)
					return
				}
			}
			w.WriteHeader(202)
			return
		case "tools/call":
			if issues {
				args, _ := q.Params["arguments"].(map[string]any)
				validIssue := q.Params["name"] == "issue_read" && args["method"] == "get" && args["owner"] == "octocat" && args["repo"] == "Hello-World" && args["issue_number"] == float64(42)
				validIssue = validIssue || q.Params["name"] == "getJiraIssue" && args["issueIdOrKey"] == "APP-42" && args["cloudId"] == "11111111-2222-3333-4444-555555555555"
				if !validIssue {
					http.Error(w, "invalid fixture issue", 400)
					return
				}
				result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "Issue fixture read completed"}}}
				break
			}

			if (!recovering && r.Header.Get("Mcp-Session-Id") != "fixture-session") || r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
				w.WriteHeader(400)
				return
			}
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "MCP fixture search completed"}}}
		case "tools/list":
			if issues {
				result = map[string]any{"tools": []any{
					map[string]any{"name": "issue_read", "inputSchema": map[string]any{"type": "object", "required": []string{"method", "owner", "repo", "issue_number"}}},
					map[string]any{"name": "getJiraIssue", "inputSchema": map[string]any{"type": "object", "required": []string{"cloudId", "issueIdOrKey"}}},
				}}
				break
			}

			if (!recovering && r.Header.Get("Mcp-Session-Id") != "fixture-session") || r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
				w.WriteHeader(400)
				return
			}
			if q.Params["cursor"] == nil {
				result = map[string]any{"tools": []any{map[string]any{"name": "search", "description": "Search fixture issues", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]string{"type": "string"}, "workspace": map[string]string{"type": "string"}}}}}, "nextCursor": "second"}
			} else {
				result = map[string]any{"tools": []any{map[string]any{"name": "read_issue", "description": "Read fixture issue", "inputSchema": map[string]string{"type": "object"}}}}
			}
		default:
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
}
