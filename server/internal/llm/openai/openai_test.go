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

// Gemini's OpenAI-compatible endpoint leaves thinking tokens out of
// completion_tokens but includes (and bills) them in total_tokens.
func TestHiddenThinkingTokensCountAsOutput(t *testing.T) {
	for name, tc := range map[string]struct {
		usage string
		want  int
	}{
		"gemini thinking": {`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":134}`, 124},
		"openai":          {`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`, 5},
		"no total":        {`{"prompt_tokens":10,"completion_tokens":5}`, 5},
		"inconsistent":    {`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":3}`, 5},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":` + tc.usage + `}`))
			}))
			defer srv.Close()
			p, err := New(llm.ProviderConfig{Name: "o", BaseURL: srv.URL + "/v1", DefaultModel: "m"}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}})
			if err != nil {
				t.Fatal(err)
			}
			if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != tc.want {
				t.Fatalf("usage %+v, want output %d", resp.Usage, tc.want)
			}
		})
	}
}

// Gemini 3 rejects a follow-up request whose earlier tool calls lack the
// thought_signature it attached; the call's extra_content is echoed back.
func TestToolCallExtraContentRoundTrips(t *testing.T) {
	var second map[string]any
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function",
				"function":{"name":"echo","arguments":"{\"word\":\"OK\"}"},"extra_content":{"google":{"thought_signature":"SIG=="}}},
				{"id":"c2","type":"function","function":{"name":"echo","arguments":"{}"},"extra_content":"not an object"}]},"finish_reason":"tool_calls"}]}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&second)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p, err := New(llm.ProviderConfig{Name: "g", BaseURL: srv.URL + "/v1", DefaultModel: "m"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: "echo"}}
	first, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Message.ToolCalls[0].ProviderData) != `{"google":{"thought_signature":"SIG=="}}` || first.Message.ToolCalls[1].ProviderData != nil {
		t.Fatalf("provider data %s / %s", first.Message.ToolCalls[0].ProviderData, first.Message.ToolCalls[1].ProviderData)
	}
	msgs = append(msgs, first.Message,
		llm.ChatMessage{Role: llm.RoleTool, ToolCallID: "c1", Name: "echo", Content: "OK"},
		llm.ChatMessage{Role: llm.RoleTool, ToolCallID: "c2", Name: "echo", Content: "OK"})
	if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	sent := second["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)
	if sig := sent[0].(map[string]any)["extra_content"]; sig == nil || sig.(map[string]any)["google"].(map[string]any)["thought_signature"] != "SIG==" {
		t.Fatalf("signature not echoed: %v", sent[0])
	}
	if _, has := sent[1].(map[string]any)["extra_content"]; has {
		t.Fatalf("invalid extra content must not be echoed: %v", sent[1])
	}
}
