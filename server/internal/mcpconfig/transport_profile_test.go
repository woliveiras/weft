package mcpconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransportProfileValidation(t *testing.T) {
	for _, profile := range []string{"", statelessProfile, "auto", "2026-07-28"} {
		in := Input{ID: "test", Name: "Test", Service: "generic", Endpoint: "https://example.com/mcp", TransportProfile: profile}
		err := validate(in)
		if (err == nil) != (profile == "" || profile == statelessProfile) {
			t.Fatalf("profile %q: %v", profile, err)
		}
	}
	in := Input{ID: "test", Name: "Test", Service: "generic", Endpoint: "https://mcp.deepwiki.com/mcp", TransportProfile: statelessProfile, Template: &TemplateRef{ID: "deepwiki", Version: 1}}
	if validate(in) == nil {
		t.Fatal("pinned template transport overridden")
	}
}

func TestStatelessTransportTLS(t *testing.T) {
	for _, scenario := range []string{"json", "sse", "additional-input", "header-schema", "wrong-id", "http-error", "credential-echo"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.Header.Get("Mcp-Session-Id") != "" {
					t.Error("unexpected session lifecycle")
				}
				var body struct {
					ID     int            `json:"id"`
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid request")
				}
				meta, _ := body.Params["_meta"].(map[string]any)
				if r.Header.Get("MCP-Protocol-Version") != statelessProtocol || meta["io.modelcontextprotocol/protocolVersion"] != statelessProtocol || r.Header.Get("Mcp-Method") != body.Method {
					t.Error("metadata mismatch")
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-wire-token" {
					t.Error("credential absent")
				}
				var result any
				switch body.Method {
				case "tools/list":
					schema := map[string]any{"type": "object"}
					if scenario == "header-schema" {
						schema["properties"] = map[string]any{"project": map[string]any{"type": "string", "x-mcp-header": "Project"}}
					}
					result = map[string]any{"tools": []any{map[string]any{"name": "read", "inputSchema": schema}}}
				case "tools/call":
					if r.Header.Get("Mcp-Name") != "read" {
						t.Error("missing name header")
					}
					result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "public data"}}}
				default:
					t.Error("unexpected handshake or notification", body.Method)
				}
				if scenario == "additional-input" {
					result = map[string]any{"inputRequests": []any{map[string]any{"method": "sampling/createMessage"}}}
				}
				if scenario == "credential-echo" {
					result = map[string]any{"token": "synthetic-wire-token"}
				}
				if scenario == "wrong-id" {
					body.ID++
				}
				if scenario == "http-error" {
					w.WriteHeader(403)
					fmt.Fprint(w, "upstream-private-diagnostic")
					return
				}
				raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": result})
				w.Header().Set("Mcp-Session-Id", "must-not-be-used")
				if scenario == "sse" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\n", raw)
				} else {
					w.Header().Set("Content-Type", "application/json")
					w.Write(raw)
				}
			}))
			defer server.Close()
			ctx := credentialContext(t.Context(), View{TransportProfile: statelessProfile})
			catalogue, err := DiscoverWithClient(ctx, server.URL, "synthetic-wire-token", server.Client())
			success := scenario == "json" || scenario == "sse"
			if (err == nil) != success {
				t.Fatal(catalogue, err)
			}
			if err != nil && strings.Contains(err.Error(), "upstream-private") {
				t.Fatal("upstream diagnostic leaked")
			}
			if calls != 1 {
				t.Fatalf("discovery replay or lifecycle: %d", calls)
			}
			if success || scenario == "additional-input" || scenario == "http-error" || scenario == "wrong-id" || scenario == "credential-echo" {
				_, err = CallWithClient(ctx, server.URL, "synthetic-wire-token", "read", json.RawMessage(`{}`), server.Client())
				if (err == nil) != success {
					t.Fatal(err)
				}
				if calls != 2 {
					t.Fatalf("tool replay or lifecycle: %d", calls)
				}
			}
		})
	}
}

func TestTransportProfileChangeInvalidatesCatalogue(t *testing.T) {
	m := testManager(t)
	in := Input{ID: "profile", Name: "Profile", Service: "generic", Endpoint: "https://example.com/mcp", Enabled: true}
	first, err := m.Put(t.Context(), in, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return []Tool{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
	}
	first, err = m.Refresh(t.Context(), in.ID, "test")
	if err != nil || len(first.Tools) != 1 {
		t.Fatal(first, err)
	}
	in.TransportProfile = statelessProfile
	next, err := m.Put(t.Context(), in, first.Revision, "test")
	if err != nil {
		t.Fatal(err)
	}
	if next.TransportProfile != statelessProfile || len(next.Tools) != 0 || next.Status != "untested" {
		t.Fatal(next)
	}
	stored, err := m.Get(t.Context(), in.ID)
	if err != nil || stored.TransportProfile != statelessProfile {
		t.Fatal(stored, err)
	}
}

func TestCustomHeaderAnnotationDetection(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{`{"type":"object","description":"x-mcp-header"}`, false},
		{`{"properties":{"a":{"x-mcp-header":"Project"}}}`, true},
		{`{"allOf":[{"properties":{"a":{"x-mcp-header":"Project"}}}]}`, true},
		{`{"properties":{"a":{"type":"integer"}}}`, false},
	} {
		var schema any
		if err := json.Unmarshal([]byte(test.raw), &schema); err != nil {
			t.Fatal(err)
		}
		if containsHeaderAnnotation(schema) != test.want {
			t.Fatal(test.raw)
		}
	}
}
