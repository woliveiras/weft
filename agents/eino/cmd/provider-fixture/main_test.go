//go:build e2e

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

func TestProviderWriterUsesSelectedModelThroughGateway(t *testing.T) {
	agent, err := writer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{200, 502} {
		var calls atomic.Int32
		controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Method != "POST" || r.URL.Path != "/internal/v1/executions/fixture/llm/chat" || r.Header.Get("Authorization") != "Bearer synthetic" {
				t.Error("wrong execution callback")
			}
			var msg struct {
				System, Model string
				Messages      []protocol.ChatMessage
			}
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Error(err)
			}
			if msg.System != "system instruction\n" || msg.Model != "" || len(msg.Messages) != 1 || msg.Messages[0].Role != "user" || msg.Messages[0].Content != "provider journey" {
				t.Errorf("unexpected callback %+v", msg)
			}
			if code != 200 {
				http.Error(w, "sensitive fixture detail", code)
				return
			}
			_ = json.NewEncoder(w).Encode(protocol.ChatResponse{Message: protocol.ChatMessage{Role: "assistant", Content: "deterministic answer"}, Usage: protocol.Usage{InputTokens: 10}})
		}))
		handler, err := protocol.NewHandler([]string{controller.URL}, "", map[string]protocol.Agent{"writer": agent})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/v1/turn", strings.NewReader(`{"executionId":"fixture","agent":"writer","nodeId":"writer","prompt":"system instruction","callback":{"url":"`+controller.URL+`/internal/v1/executions/fixture","token":"synthetic"}}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		controller.Close()
		if calls.Load() != 1 {
			t.Fatal("expected exactly one gateway invocation")
		}
		if code == 200 {
			var result protocol.TurnResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || result.Summary != "deterministic answer" || result.Usage != nil || len(result.Emits) != 0 {
				t.Fatalf("unexpected local result %+v", result)
			}
		} else if rec.Code != 500 || strings.Contains(rec.Body.String(), "sensitive fixture detail") || strings.Contains(rec.Body.String(), "emits") {
			t.Fatalf("unexpected runtime failure %s", rec.Body.String())
		}
	}
}
