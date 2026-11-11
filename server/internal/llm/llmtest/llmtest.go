// Package llmtest provides a fake OpenAI-compatible model server for tests
// that need a real HTTP provider (credentials on the wire, failures,
// timeouts) without network access.
package llmtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Server is a fake OpenAI-compatible API under /v1.
type Server struct {
	*httptest.Server
	// Key is the only accepted bearer token. Wrong keys get a 401 whose
	// body echoes the key, like some real providers do.
	Key    string
	Models []string
	// Reply is the assistant content returned by chat completions.
	Reply string
	// Delay is applied to chat completions (timeout tests).
	Delay time.Duration
	// FailChat makes chat completions answer HTTP 500.
	FailChat atomic.Bool
	// ToolCalls makes requests that offer tools answer with a structured
	// call to the first tool (arguments {"word":"OK"}); otherwise the model
	// answers in text, like one without tool calling.
	ToolCalls atomic.Bool

	mu       sync.Mutex
	chats    int
	lastAuth string
	lastBody map[string]any
}

// New starts a fake server; it is closed when the test ends.
func New(t *testing.T, key string) *Server {
	t.Helper()
	s := &Server{Key: key, Models: []string{"fake-small", "fake-large"}, Reply: "OK"}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// BaseURL is the URL to configure as the provider base URL.
func (s *Server) BaseURL() string { return s.URL + "/v1" }

// Chats is the number of chat completions served (any outcome).
func (s *Server) Chats() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chats
}

// LastAuthorization is the Authorization header of the last request.
func (s *Server) LastAuthorization() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAuth
}

// LastChat is the decoded body of the last chat completion request.
func (s *Server) LastChat() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBody
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	s.mu.Lock()
	s.lastAuth = auth
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if s.Key != "" && auth != "Bearer "+s.Key {
		got := auth
		if len(got) > 7 {
			got = got[7:]
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Incorrect API key provided: " + got}})
		return
	}
	switch r.URL.Path {
	case "/v1/models":
		data := []map[string]any{}
		for _, m := range s.Models {
			data = append(data, map[string]any{"id": m, "object": "model"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case "/v1/chat/completions":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.chats++
		s.lastBody = body
		s.mu.Unlock()
		if s.Delay > 0 {
			select {
			case <-time.After(s.Delay):
			case <-r.Context().Done():
				return
			}
		}
		if s.FailChat.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"model crashed"}}`))
			return
		}
		model, _ := body["model"].(string)
		if tools, _ := body["tools"].([]any); s.ToolCalls.Load() && len(tools) > 0 {
			fn, _ := tools[0].(map[string]any)["function"].(map[string]any)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": model,
				"choices": []map[string]any{{
					"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{{
						"id": "call_1", "type": "function",
						"function": map[string]any{"name": fn["name"], "arguments": `{"word":"OK"}`},
					}}},
					"finish_reason": "tool_calls",
				}},
				"usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": model,
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": s.Reply},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 1},
		})
	default:
		http.NotFound(w, r)
	}
}
