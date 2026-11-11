package gemini

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
		out[i] = "data: " + c + "\r\n\r\n"
	}
	return out
}

func streamProvider(t *testing.T, s *llmtest.Stream) *Provider {
	t.Helper()
	p, err := New(llm.ProviderConfig{Name: "gem", BaseURL: s.URL + "/v1beta", APIKeyValue: testKey, DefaultModel: "gemini-3-flash", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func streamReq() *llm.ChatRequest {
	return &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "go"}}, Tools: []llm.ToolSchema{{Name: "filesystem.read"}}}
}

func TestChatStreamTextUsageAndThinking(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", sse(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"weighing options","thought":true}]}}],"usageMetadata":{"promptTokenCount":10,"thoughtsTokenCount":5},"modelVersion":"gemini-3-flash-001"}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1,"thoughtsTokenCount":5}}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"},{"text":"","thoughtSignature":"c2ln"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":5,"cachedContentTokenCount":4}}`)...)
	var live []string
	resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), func(s string) { live = append(live, s) })
	if err != nil {
		t.Fatal(err)
	}
	// Thinking is never shown, and is counted as output.
	want := &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: "Hello"}, StopReason: "stop", Model: "gemini-3-flash-001",
		Usage: llm.Usage{InputTokens: 10, OutputTokens: 7, CacheReadTokens: 4}}
	if !reflect.DeepEqual(resp, want) || strings.Join(live, "|") != "Hel|lo" {
		t.Fatalf("resp %+v live %q", resp, live)
	}
	bodies, paths, accept := s.Requests()
	if paths[0] != "/v1beta/models/gemini-3-flash:streamGenerateContent?alt=sse" || accept[0] != "text/event-stream" {
		t.Fatalf("request %s %q", paths[0], accept)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil || body["contents"] == nil {
		t.Fatalf("body %s", bodies[0])
	}
}

// Function calls keep their thought signatures, and the joined answer is
// the one Chat returns for the same parts.
func TestChatStreamFunctionCallsMatchChat(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", sse(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Reading."}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"filesystem__read","args":{"path":"a.txt"}},"thoughtSignature":"c2ln"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":6,"thoughtsTokenCount":3}}`)...)
	var live []string
	resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), func(s string) { live = append(live, s) })
	if err != nil {
		t.Fatal(err)
	}
	whole := llmtest.NewStream(t, "application/json", `{"candidates":[{"content":{"role":"model","parts":[{"text":"Reading."},
		{"functionCall":{"name":"filesystem__read","args":{"path":"a.txt"}},"thoughtSignature":"c2ln"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":6,"thoughtsTokenCount":3}}`)
	want, err := streamProvider(t, whole).Chat(context.Background(), streamReq())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp, want) || strings.Join(live, "") != "Reading." {
		t.Fatalf("streamed %+v\nwhole    %+v live %q", resp, want, live)
	}
	tc := resp.Message.ToolCalls
	if len(tc) != 1 || tc[0].Name != "filesystem.read" || signatureOf(tc[0].ProviderData) != "c2ln" || resp.StopReason != "tool_calls" ||
		resp.Usage.OutputTokens != 9 {
		t.Fatalf("resp %+v", resp)
	}
}

func TestChatStreamFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		chunks []string
		want   error
	}{
		"quota error mid-stream":  {sse(`{"candidates":[{"content":{"parts":[{"text":"par"}]}}]}`, `{"error":{"code":429,"message":"Resource has been exhausted","status":"RESOURCE_EXHAUSTED"}}`), ErrQuota},
		"server error mid-stream": {sse(`{"candidates":[{"content":{"parts":[{"text":"par"}]}}]}`, `{"error":{"code":500,"message":"internal","status":"INTERNAL"}}`), nil},
		"ended early":             {sse(`{"candidates":[{"content":{"parts":[{"text":"par"}]}}]}`), llm.ErrStreamIncomplete},
		"malformed chunk":         {sse(`{"candidates":`), llm.ErrBadStream},
		"prompt blocked":          {sse(`{"promptFeedback":{"blockReason":"SAFETY"}}`), ErrSafetyBlocked},
		"answer blocked":          {sse(`{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`), ErrSafetyBlocked},
	} {
		t.Run(name, func(t *testing.T) {
			s := llmtest.NewStream(t, "text/event-stream", tc.chunks...)
			resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), func(string) {})
			if resp != nil || err == nil {
				t.Fatalf("a failed stream must be an error without an answer: %+v %v", resp, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Fatalf("error leaks the key: %v", err)
			}
			var ae *llm.APIError
			if strings.Contains(name, "mid-stream") && (!errors.As(err, &ae) || ae.Retryable) {
				t.Fatalf("err = %#v", err)
			}
		})
	}
}

func TestChatStreamCancel(t *testing.T) {
	s := llmtest.NewStream(t, "text/event-stream", append(sse(`{"candidates":[{"content":{"parts":[{"text":"par"}]}}]}`), llmtest.Hang)...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	resp, err := streamProvider(t, s).ChatStream(ctx, streamReq(), func(string) { cancel() })
	if resp != nil || !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("resp %+v err %v after %s", resp, err, time.Since(start))
	}
}

func TestChatStreamVertexPath(t *testing.T) {
	p := &Provider{cfg: llm.ProviderConfig{Name: "v"}, vertex: &vertex{project: "my-project", region: "europe-west4"}}
	if got := p.chatPath("gemini-3-flash", "streamGenerateContent"); got != "/v1/projects/my-project/locations/europe-west4/publishers/google/models/gemini-3-flash:streamGenerateContent" {
		t.Fatalf("path %s", got)
	}
}

// Without SSE, streamGenerateContent answers one JSON array of the chunks.
func TestChatStreamAcceptsAJSONArray(t *testing.T) {
	s := llmtest.NewStream(t, "application/json", `[
		{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]},
		{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2}}]`)
	var live []string
	resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), func(s string) { live = append(live, s) })
	if err != nil || resp.Message.Content != "Hello" || resp.Usage != (llm.Usage{InputTokens: 4, OutputTokens: 2}) || strings.Join(live, "|") != "Hel|lo" {
		t.Fatalf("resp %+v err %v live %q", resp, err, live)
	}
}
