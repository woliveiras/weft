package openai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/llmtest"
)

func sse(chunks ...string) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = "data: " + c + "\n\n"
	}
	return out
}

func streamProvider(t *testing.T, s *llmtest.Stream, opts Options) *Provider {
	t.Helper()
	p, err := New(llm.ProviderConfig{Name: "o", BaseURL: s.URL + "/v1", APIKeyValue: "sk-test", DefaultModel: "gpt-x", MaxRetries: -1}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func streamReq() *llm.ChatRequest {
	return &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "go"}}, Tools: []llm.ToolSchema{{Name: "filesystem.read"}}}
}

func TestChatStreamTextAndUsage(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", append([]string{": keep-alive\n\n"}, sse(
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"reasoning_content":"hidden"}}]}`,
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
		// Hidden thinking (Gemini's compatible endpoint) is only in the total.
		`{"model":"gpt-x","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":4}}}`,
		`[DONE]`)...)...)
	var live []string
	resp, err := streamProvider(t, s, Options{}).ChatStream(context.Background(), streamReq(), func(s string) { live = append(live, s) })
	if err != nil {
		t.Fatal(err)
	}
	want := &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: "Hello"}, StopReason: "stop", Model: "gpt-x",
		Usage: llm.Usage{InputTokens: 10, OutputTokens: 9, CacheReadTokens: 4}}
	if !reflect.DeepEqual(resp, want) || strings.Join(live, "|") != "Hel|lo" {
		t.Fatalf("resp %+v live %q", resp, live)
	}
	bodies, paths, accept := s.Requests()
	var body map[string]any
	_ = json.Unmarshal([]byte(bodies[0]), &body)
	if paths[0] != "/v1/chat/completions" || body["stream"] != true || accept[0] != "text/event-stream" ||
		!reflect.DeepEqual(body["stream_options"], map[string]any{"include_usage": true}) {
		t.Fatalf("request %s %v %q", paths[0], body, accept)
	}
}

// Tool calls are assembled from their deltas into the answer Chat returns
// for the same content, vendor data (Gemini's thought signature) included.
func TestChatStreamToolCallDeltasMatchChat(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", sse(
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"filesystem__read","arguments":""}}]}}]}`,
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pa"}}]}}]}`,
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.txt\"}"}}]}}]}`,
		`{"model":"gpt-x","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"git__status","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig=="}}}]}}]}`,
		`{"model":"gpt-x","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"model":"gpt-x","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`,
		`[DONE]`)...)
	resp, err := streamProvider(t, s, Options{}).ChatStream(context.Background(), streamReq(), func(string) { t.Error("tool calls are not text") })
	if err != nil {
		t.Fatal(err)
	}
	whole := llmtest.NewStream(t, "application/json", `{"model":"gpt-x","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
		{"id":"c1","type":"function","function":{"name":"filesystem__read","arguments":"{\"path\":\"a.txt\"}"}},
		{"id":"c2","type":"function","function":{"name":"git__status","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig=="}}}]},
		"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	want, err := streamProvider(t, whole, Options{}).Chat(context.Background(), streamReq())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp, want) {
		t.Fatalf("streamed %+v\nwhole    %+v", resp, want)
	}
	tc := resp.Message.ToolCalls
	if len(tc) != 2 || string(tc[0].Arguments) != `{"path":"a.txt"}` || tc[0].Name != "filesystem.read" ||
		string(tc[1].ProviderData) != `{"google":{"thought_signature":"sig=="}}` || resp.StopReason != "tool_calls" {
		t.Fatalf("tool calls %+v", tc)
	}
}

func TestChatStreamFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		chunks []string
		want   error
		text   string
	}{
		"error mid-stream": {sse(`{"choices":[{"delta":{"content":"par"}}]}`, `{"error":{"message":"upstream overloaded","type":"server_error"}}`), nil, "upstream overloaded"},
		"ended early":      {sse(`{"choices":[{"delta":{"content":"par"}}]}`), llm.ErrStreamIncomplete, ""},
		"malformed chunk":  {sse(`{"choices":`), llm.ErrBadStream, ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := llmtest.NewStream(t, "text/event-stream", tc.chunks...)
			resp, err := streamProvider(t, s, Options{}).ChatStream(context.Background(), streamReq(), func(string) {})
			if resp != nil || err == nil {
				t.Fatalf("a failed stream must be an error without an answer: %+v %v", resp, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.text != "" && !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("err = %v", err)
			}
		})
	}

	// A finish reason without [DONE] (some compatible servers) is complete.
	s := llmtest.NewStream(t, "text/event-stream", sse(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)...)
	resp, err := streamProvider(t, s, Options{}).ChatStream(context.Background(), streamReq(), nil)
	if err != nil || resp.Message.Content != "ok" || resp.StopReason != "stop" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
}

func TestChatStreamCancel(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", append(sse(`{"choices":[{"delta":{"content":"par"}}]}`), llmtest.Hang)...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	resp, err := streamProvider(t, s, Options{}).ChatStream(ctx, streamReq(), func(string) { cancel() })
	if resp != nil || !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("resp %+v err %v after %s", resp, err, time.Since(start))
	}
}

// Servers whose streaming is not known to work answer whole.
func TestNoStreamingAnswersWhole(t *testing.T) {
	s := llmtest.NewStream(t, "application/json", `{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	resp, err := streamProvider(t, s, Options{NoStreaming: true}).ChatStream(context.Background(), streamReq(), func(string) { t.Error("no live text") })
	if err != nil || resp.Message.Content != "hi" || resp.Usage.InputTokens != 3 {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	bodies, _, _ := s.Requests()
	if !strings.Contains(bodies[0], `"stream":false`) || strings.Contains(bodies[0], "stream_options") {
		t.Fatalf("body %s", bodies[0])
	}
}

// A compatible server that ignores stream answers whole: the answer is
// accepted, and its text is shown once.
func TestChatStreamAcceptsAWholeAnswer(t *testing.T) {
	s := llmtest.NewStream(t, "application/json; charset=utf-8", `{"model":"gpt-x","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	var live []string
	resp, err := streamProvider(t, s, Options{}).ChatStream(context.Background(), streamReq(), func(s string) { live = append(live, s) })
	if err != nil || resp.Message.Content != "hi" || resp.Usage != (llm.Usage{InputTokens: 3, OutputTokens: 1}) || strings.Join(live, "|") != "hi" {
		t.Fatalf("resp %+v err %v live %q", resp, err, live)
	}
	bad := llmtest.NewStream(t, "application/json", `{"choices":`)
	if _, err := streamProvider(t, bad, Options{}).ChatStream(context.Background(), streamReq(), nil); !errors.Is(err, llm.ErrBadStream) {
		t.Fatalf("err = %v", err)
	}
}
