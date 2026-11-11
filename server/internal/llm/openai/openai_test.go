package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatToolRoundTrip(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"gpt-x","choices":[{"message":{"role":"assistant","content":null,
			"tool_calls":[{"id":"c1","type":"function","function":{"name":"filesystem__read","arguments":"{\"path\":\"a.txt\"}"}}]},
			"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":6}}}`))
	}))
	defer srv.Close()
	t.Setenv("OAI_KEY", "sk-test")
	p, err := New(llm.ProviderConfig{Name: "o", BaseURL: srv.URL + "/v1", APIKeyEnv: "OAI_KEY", DefaultModel: "gpt-x"}, Options{RequireKey: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), &llm.ChatRequest{
		System: "sys",
		JSON:   true,
		Messages: []llm.ChatMessage{
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c0", Name: "git.status", Arguments: json.RawMessage(`{}`)}}},
			{Role: llm.RoleTool, ToolCallID: "c0", Name: "git.status", Content: "clean"},
		},
		Tools: []llm.ToolSchema{{Name: "filesystem.read", Description: "read"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != "tool_calls" || len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("resp %+v", resp)
	}
	tc := resp.Message.ToolCalls[0]
	if tc.Name != "filesystem.read" || string(tc.Arguments) != `{"path":"a.txt"}` {
		t.Fatalf("tool call %+v %s", tc, tc.Arguments)
	}
	if resp.Usage != (llm.Usage{InputTokens: 11, OutputTokens: 7, CacheReadTokens: 6}) {
		t.Fatalf("usage %+v", resp.Usage)
	}
	// request shape
	if got["model"] != "gpt-x" {
		t.Errorf("model %v", got["model"])
	}
	if rf, _ := got["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Errorf("response_format %v", got["response_format"])
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages %v", msgs)
	}
	asst := msgs[2].(map[string]any)
	call := asst["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["name"] != "git__status" || call["arguments"] != "{}" {
		t.Errorf("assistant call %v", call)
	}
	if msgs[3].(map[string]any)["tool_call_id"] != "c0" {
		t.Errorf("tool msg %v", msgs[3])
	}
	tool := got["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if tool["name"] != "filesystem__read" || tool["parameters"] == nil {
		t.Errorf("tool %v", tool)
	}
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p, _ := New(llm.ProviderConfig{Name: "v", BaseURL: srv.URL}, Options{})
	p.HTTP().BaseDelay = time.Millisecond
	resp, err := p.Chat(context.Background(), &llm.ChatRequest{Model: "m", Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "x"}}})
	if err != nil || resp.Message.Content != "ok" || resp.StopReason != "stop" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	if n.Load() != 3 {
		t.Fatalf("attempts %d", n.Load())
	}
}

func TestNoRetryOn400(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad`))
	}))
	defer srv.Close()
	p, _ := New(llm.ProviderConfig{Name: "v", BaseURL: srv.URL}, Options{})
	p.HTTP().BaseDelay = time.Millisecond
	_, err := p.Chat(context.Background(), &llm.ChatRequest{Model: "m"})
	ae, ok := err.(*llm.APIError)
	if !ok || ae.Status != 400 || ae.Retryable || n.Load() != 1 {
		t.Fatalf("err %v attempts %d", err, n.Load())
	}
}

func TestRequireKey(t *testing.T) {
	if _, err := New(llm.ProviderConfig{Name: "o"}, Options{RequireKey: true}); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"a","max_model_len":4096},{"id":"b"}]}`))
	}))
	defer srv.Close()
	p, _ := New(llm.ProviderConfig{Name: "v", BaseURL: srv.URL}, Options{})
	ms, err := p.Models(context.Background())
	if err != nil || len(ms) != 2 || ms[0].ContextSize != 4096 {
		t.Fatalf("%v %v", ms, err)
	}
}
