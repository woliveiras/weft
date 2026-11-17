//go:build e2e

package main

import (
	"bytes"
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/storage"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, key, body string
		status          int
		want            string
	}{
		{"success", fixtureKey, `{"model":"journey-ok","messages":[{"role":"user","content":"provider journey"}]}`, 200, "LANGGRAPH_GATEWAY_OK"},
		{"tool", fixtureKey, `{"model":"journey-ok","messages":[{}],"tools":[{"function":{"name":"connection_check"}}]}`, 200, "tool_calls"},
		{"failure", fixtureKey, `{"model":"journey-fail","messages":[{}]}`, 503, fixtureKey},
		{"bad credential", wrongKey, `{}`, 401, wrongKey},
		{"malformed", fixtureKey, `{`, 400, "invalid chat request"},
		{"missing messages", fixtureKey, `{"model":"journey-ok"}`, 400, "invalid chat request"},
		{"unknown model", fixtureKey, `{"model":"missing","messages":[{}]}`, 404, "unknown model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer "+tc.key)
			w := httptest.NewRecorder()
			(&modelFixture{}).ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
			}
		})
	}
}

// No skip: this dedicated suite requires the real optional SDK dependencies.
func TestRealLangGraphManagedProvider(t *testing.T) {
	f, err := newFixture("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.close)
	call := func(t *testing.T, method, path string, body any, want int) []byte {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if containsCredential(b) {
			t.Fatal("API response exposed artificial credential")
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, b)
		}
		return b
	}
	provider := map[string]any{"name": "selected", "type": "openai", "baseUrl": f.model.URL + "/v1", "defaultModel": "journey-ok", "maxRetries": -1, "apiKey": map[string]string{"value": fixtureKey}}
	call(t, "POST", "/api/v1/providers", provider, 422) // the plugin starts disabled
	call(t, "PUT", "/api/v1/provider-plugins/openai", map[string]bool{"enabled": true}, 200)
	call(t, "POST", "/api/v1/providers", provider, 201)
	if f.app.Models.DefaultName() != "mock" {
		t.Fatal("fixture must keep a different default provider")
	}
	var result struct{ OK bool }
	_ = json.Unmarshal(call(t, "POST", "/api/v1/providers/selected/test", nil, 200), &result)
	if !result.OK {
		t.Fatal("saved provider test failed")
	}
	provider["apiKey"] = map[string]string{"value": wrongKey}
	_ = json.Unmarshal(call(t, "POST", "/api/v1/providers/test", provider, 200), &result)
	if result.OK {
		t.Fatal("invalid credential passed connection test")
	}
	call(t, "POST", "/api/v1/graphs/provider-missing/start", nil, 422)
	for _, tc := range []struct{ id, model, status string }{
		{"provider-success", "journey-ok", "succeeded"}, {"provider-failure", "journey-fail", "failed"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			var graph map[string]any
			_ = json.Unmarshal(call(t, "GET", "/api/v1/graphs/"+tc.id, nil, 200), &graph)
			graph["graph"].(map[string]any)["spec"].(map[string]any)["nodes"].([]any)[0].(map[string]any)["model"] = map[string]string{"provider": "selected", "name": tc.model}
			call(t, "PUT", "/api/v1/graphs/"+tc.id, graph["graph"], 200)
			call(t, "POST", "/api/v1/graphs/"+tc.id+"/start", nil, 200)
			deadline := time.Now().Add(15 * time.Second)
			for {
				b := call(t, "GET", "/api/v1/graphs/"+tc.id+"/executions", nil, 200)
				var executions []struct{ Status, Summary string }
				_ = json.Unmarshal(b, &executions)
				found := false
				for _, e := range executions {
					if e.Status == tc.status {
						found = true
						if tc.status == "succeeded" && e.Summary != "LANGGRAPH_GATEWAY_OK" {
							t.Fatalf("unexpected summary: %s", e.Summary)
						}
					}
				}
				if found {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("execution never %s: %s", tc.status, b)
				}
				time.Sleep(50 * time.Millisecond)
			}
			call(t, "POST", "/api/v1/graphs/"+tc.id+"/stop", nil, 200)
		})
	}
	turns, err := os.ReadFile(filepath.Join(f.root, "turns.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if containsCredential(turns) {
		t.Fatal("runtime input/result exposed artificial credential")
	}
	if !bytes.Contains(turns, []byte(`"provider": "selected"`)) || !bytes.Contains(turns, []byte("LANGGRAPH_GATEWAY_OK")) || !bytes.Contains(turns, []byte(`"status": 500`)) || !bytes.Contains(turns, []byte("deterministic provider failure")) {
		t.Fatal("missing real adapter evidence")
	}
	model := f.model.Config.Handler.(*modelFixture)
	model.mu.Lock()
	requested := append([]string(nil), model.requests...)
	model.mu.Unlock()
	if !strings.Contains(strings.Join(requested, ","), "journey-fail") || !strings.Contains(strings.Join(requested, ","), "journey-ok") {
		t.Fatal("selected model did not reach the provider wire")
	}
	if containsCredential([]byte(strings.Join(f.runtime.Env, "\n"))) {
		t.Fatal("runtime environment received provider credential")
	}
	logs, err := os.ReadFile(filepath.Join(f.root, "runtime.log"))
	if err != nil {
		t.Fatal(err)
	}
	if containsCredential(logs) {
		t.Fatal("runtime log exposed artificial credential")
	}
	setting, err := f.app.Store.GetSetting(t.Context(), "llm/providers")
	if err != nil {
		t.Fatal(err)
	}
	if containsCredential(setting.Value) {
		t.Fatal("credential stored in plaintext")
	}
	events, err := f.app.Store.ListEvents(t.Context(), storage.EventFilter{Limit: 5000})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if containsCredential(b) {
		t.Fatal("event exposed artificial credential")
	}
	for _, path := range []string{"providers", "providers/selected", "provider-settings"} {
		call(t, "GET", "/api/v1/"+path, nil, 200)
	}
	t.Logf("real adapter verified; input/results/logs, API, settings and %d events contain no artificial provider credential", len(events))
}
