package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChatToolRoundTrip(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("headers %v", r.Header)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"claude-x","stop_reason":"tool_use","content":[
			{"type":"text","text":"let me look"},
			{"type":"tool_use","id":"tu1","name":"filesystem__read","input":{"path":"a"}}],
			"usage":{"input_tokens":5,"output_tokens":3,"cache_read_input_tokens":10,"cache_creation_input_tokens":2}}`))
	}))
	defer srv.Close()
	t.Setenv("ANT_KEY", "k")
	p, err := New(llm.ProviderConfig{Name: "a", BaseURL: srv.URL + "/v1", APIKeyEnv: "ANT_KEY", DefaultModel: "claude-x"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), &llm.ChatRequest{
		System: "sys", JSON: true,
		Messages: []llm.ChatMessage{
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "a1", Name: "git.status", Arguments: json.RawMessage(`{}`)},
				{ID: "a2", Name: "git.log", Arguments: json.RawMessage(`"{\"n\":1}"`)},
			}},
			{Role: llm.RoleTool, ToolCallID: "a1", Content: "clean"},
			{Role: llm.RoleTool, ToolCallID: "a2", Content: "log"},
		},
		Tools: []llm.ToolSchema{{Name: "filesystem.read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// input_tokens excludes the cache parts on the wire; Usage counts them all.
	if resp.Usage != (llm.Usage{InputTokens: 17, OutputTokens: 3, CacheReadTokens: 10, CacheWriteTokens: 2}) {
		t.Fatalf("usage %+v", resp.Usage)
	}
	if resp.StopReason != "tool_calls" || resp.Message.Content != "let me look" || len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("resp %+v", resp)
	}
	if tc := resp.Message.ToolCalls[0]; tc.Name != "filesystem.read" || tc.ID != "tu1" || string(tc.Arguments) != `{"path":"a"}` {
		t.Fatalf("tc %+v", tc)
	}
	if got["max_tokens"].(float64) != 4096 || !strings.Contains(got["system"].(string), "JSON") {
		t.Errorf("req %v", got)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("expected user, assistant, merged tool results: %v", msgs)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if msgs[2].(map[string]any)["role"] != "user" || len(results) != 2 || results[1].(map[string]any)["tool_use_id"] != "a2" {
		t.Errorf("tool results %v", msgs[2])
	}
	uses := msgs[1].(map[string]any)["content"].([]any)
	if in := uses[1].(map[string]any)["input"].(map[string]any); in["n"].(float64) != 1 {
		t.Errorf("string args not normalised: %v", uses[1])
	}
	tool := got["tools"].([]any)[0].(map[string]any)
	if tool["name"] != "filesystem__read" || tool["input_schema"] == nil {
		t.Errorf("tool %v", tool)
	}
}

func TestStopMapping(t *testing.T) {
	for in, want := range map[string]string{"end_turn": "stop", "max_tokens": "length", "tool_use": "tool_calls"} {
		if mapStop(in) != want {
			t.Errorf("%s -> %s", in, mapStop(in))
		}
	}
}

func TestKeyRequiredForHostedAPI(t *testing.T) {
	if _, err := New(llm.ProviderConfig{Name: "a"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestContextWindow(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"model":"claude-x","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()
	ask := func(p *Provider, content string) error {
		_, err := p.Chat(context.Background(), &llm.ChatRequest{Model: "m", Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: content}}})
		return err
	}
	def, err := New(llm.ProviderConfig{Name: "a", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got := llm.ContextWindowOf(def); got != 0 {
		t.Fatalf("default window = %d, want 0", got)
	}
	small, err := New(llm.ProviderConfig{Name: "a", BaseURL: srv.URL, ContextWindow: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if got := llm.ContextWindowOf(small); got != 2048 {
		t.Fatalf("configured window = %d, want 2048", got)
	}
	if err := ask(small, "hi"); err != nil || calls.Load() != 1 {
		t.Fatalf("fitting prompt: %v (requests %d)", err, calls.Load())
	}
	err = ask(small, strings.Repeat("x", 4*2048))
	if !errors.Is(err, llm.ErrContextExceeded) || !strings.Contains(err.Error(), "context window of 2048") || calls.Load() != 1 {
		t.Fatalf("oversized prompt: %v (requests %d)", err, calls.Load())
	}
}
