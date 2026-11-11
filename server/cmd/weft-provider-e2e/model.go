//go:build e2e

package main

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Only external inference is simulated. This handler speaks the OpenAI wire
// protocol to the production provider implementation, including error bodies.
const fixtureKey = "sk-artificial-provider-journey"
const wrongKey = "sk-artificial-wrong-key"

type modelFixture struct {
	mu       sync.Mutex
	requests []string
}

func (m *modelFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+fixtureKey {
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "invalid credential " + r.Header.Get("Authorization")}})
		return
	}
	if r.URL.Path == "/v1/models" {
		_, _ = w.Write([]byte(`{"data":[{"id":"journey-ok"},{"id":"journey-fail"}]}`))
		return
	}
	if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) == 0 {
		http.Error(w, "invalid chat request", 400)
		return
	}
	m.mu.Lock()
	m.requests = append(m.requests, body.Model)
	m.mu.Unlock()
	if body.Model == "journey-fail" {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"deterministic provider failure ` + fixtureKey + `"}}`))
		return
	}
	if body.Model != "journey-ok" {
		http.Error(w, "unknown model", 404)
		return
	}
	message := map[string]any{"role": "assistant", "content": "OK"}
	reason := "stop"
	if len(body.Tools) > 0 {
		message["tool_calls"] = []any{map[string]any{"id": "call_fixture", "type": "function", "function": map[string]string{"name": body.Tools[0].Function.Name, "arguments": `{"word":"OK"}`}}}
		reason = "tool_calls"
	} else {
		for _, msg := range body.Messages {
			if msg.Content == "provider journey" {
				message["content"] = "LANGGRAPH_GATEWAY_OK"
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"model": body.Model, "choices": []any{map[string]any{"message": message, "finish_reason": reason}}, "usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 1}})
}
