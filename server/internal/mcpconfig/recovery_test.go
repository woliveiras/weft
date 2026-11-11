package mcpconfig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionRecoveryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, method         string
		status               int
		always, call, wantOK bool
		sessions, calls      int
	}{
		{"discovery", "tools/list", 404, false, false, true, 2, 0},
		{"before dispatch", "notifications/initialized", 404, false, true, true, 2, 1},
		{"expired call never replayed", "tools/call", 404, false, true, false, 2, 1},
		{"bounded", "tools/list", 404, true, false, false, 2, 0},
		{"missing endpoint", "initialize", 404, true, false, false, 1, 0},
		{"server error", "tools/call", 503, true, true, false, 1, 1},
		{"unauthorized", "tools/list", 401, true, false, false, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions, calls := 0, 0
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					w.WriteHeader(204)
					return
				}
				var q struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					t.Error(err)
					return
				}
				if q.Method == "initialize" {
					sessions++
					if r.Header.Get("Mcp-Session-Id") != "" {
						t.Error("reinitialize sent stale session")
					}
				}
				if q.Method == "tools/call" {
					calls++
				}
				if q.Method == tc.method && (tc.always || sessions == 1) {
					w.WriteHeader(tc.status)
					return
				}
				var result any
				switch q.Method {
				case "initialize":
					w.Header().Set("Mcp-Session-Id", "session")
					result = map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{"tools": map[string]any{}}}
				case "notifications/initialized":
					w.WriteHeader(202)
					return
				case "tools/list":
					result = map[string]any{"tools": []Tool{}}
				case "tools/call":
					result = map[string]any{"content": []any{}}
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
			}))
			defer upstream.Close()
			var err error
			if tc.call {
				_, err = CallWithClient(t.Context(), upstream.URL, "", "write", json.RawMessage(`{}`), upstream.Client())
			} else {
				_, err = DiscoverWithClient(t.Context(), upstream.URL, "", upstream.Client())
			}
			if (err == nil) != tc.wantOK || sessions != tc.sessions || calls != tc.calls {
				t.Fatalf("err=%v sessions=%d calls=%d", err, sessions, calls)
			}
		})
	}
}

func TestRecoveryDoesNotRetryUnboundOrAmbiguousFailures(t *testing.T) {
	for _, status := range []int{404, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: http.NoBody}, nil
			})}
			if _, err := DiscoverWithClient(t.Context(), "https://example.com/mcp", "", client); err == nil {
				t.Fatal("failure hidden")
			}
			if requests != 1 {
				t.Fatalf("unexpected retry: %d requests", requests)
			}
		})
	}
}

func TestRecoveryDiscardsPartialCatalogue(t *testing.T) {
	sessions := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var q struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch q.Method {
		case "initialize":
			sessions++
			w.Header().Set("Mcp-Session-Id", "session")
			result = map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			w.WriteHeader(202)
			return
		case "tools/list":
			if sessions == 1 && q.Params["cursor"] != nil {
				w.WriteHeader(404)
				return
			}
			name := "current"
			if sessions == 1 {
				name = "obsolete"
			}
			result = map[string]any{"tools": []Tool{{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)}}}
			if sessions == 1 {
				result.(map[string]any)["nextCursor"] = "second"
			}
			if sessions == 2 && q.Params["cursor"] != nil {
				t.Error("recovery reused old cursor")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	defer upstream.Close()
	catalog, err := DiscoverWithClient(t.Context(), upstream.URL, "", upstream.Client())
	if err != nil || len(catalog) != 1 || catalog[0].Name != "current" || sessions != 2 {
		t.Fatal(catalog, err, sessions)
	}
}
