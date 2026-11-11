package ollama

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

func streamProvider(t *testing.T, s *llmtest.Stream) *Provider {
	t.Helper()
	p, err := New(llm.ProviderConfig{Name: "ol", BaseURL: s.URL, DefaultModel: "llama3", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func streamReq() *llm.ChatRequest {
	return &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "go"}}, Tools: []llm.ToolSchema{{Name: "shell.exec"}}}
}

func TestChatStreamTextAndUsage(t *testing.T) {
	s := llmtest.NewStream(t, "application/x-ndjson",
		`{"model":"llama3","message":{"role":"assistant","content":"Hel"},"done":false}`+"\n",
		`{"model":"llama3","message":{"role":"assistant","content":"lo","thinking":"hidden"},"done":false}`+"\n",
		`{"model":"llama3","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":9,"eval_count":4}`+"\n")
	var live []string
	resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), func(s string) { live = append(live, s) })
	if err != nil {
		t.Fatal(err)
	}
	want := &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: "Hello"}, StopReason: "stop", Model: "llama3",
		Usage: llm.Usage{InputTokens: 9, OutputTokens: 4}}
	if !reflect.DeepEqual(resp, want) || strings.Join(live, "|") != "Hel|lo" {
		t.Fatalf("resp %+v live %q", resp, live)
	}
	bodies, paths, accept := s.Requests()
	var body map[string]any
	_ = json.Unmarshal([]byte(bodies[0]), &body)
	if paths[0] != "/api/chat" || body["stream"] != true || accept[0] != "application/x-ndjson" {
		t.Fatalf("request %s %v %q", paths[0], body, accept)
	}
}

// The streamed answer is the one Chat returns for the same content.
func TestChatStreamToolCallsMatchChat(t *testing.T) {
	s := llmtest.NewStream(t, "application/x-ndjson",
		`{"model":"llama3","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"shell__exec","arguments":{"command":"ls"}}}]},"done":false}`+"\n",
		`{"model":"llama3","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"git__status","arguments":{}}}]},"done":false}`+"\n",
		`{"model":"llama3","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":20,"eval_count":7}`+"\n")
	resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), nil)
	if err != nil {
		t.Fatal(err)
	}
	whole := llmtest.NewStream(t, "application/json",
		`{"model":"llama3","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"shell__exec","arguments":{"command":"ls"}}},{"function":{"name":"git__status","arguments":{}}}]},"done":true,"done_reason":"stop","prompt_eval_count":20,"eval_count":7}`)
	want, err := streamProvider(t, whole).Chat(context.Background(), streamReq())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp, want) || resp.StopReason != "tool_calls" || len(resp.Message.ToolCalls) != 2 || resp.Message.ToolCalls[1].ID != "call_1" {
		t.Fatalf("streamed %+v\nwhole    %+v", resp, want)
	}
}

func TestChatStreamFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		chunks []string
		want   error
	}{
		"error mid-stream": {[]string{`{"message":{"content":"par"},"done":false}` + "\n", `{"error":"model runner crashed"}` + "\n"}, nil},
		"no done chunk":    {[]string{`{"message":{"content":"par"},"done":false}` + "\n"}, llm.ErrStreamIncomplete},
		"malformed chunk":  {[]string{`{"message":` + "\n"}, llm.ErrBadStream},
		"window filled":    {[]string{`{"done":true,"prompt_eval_count":32768,"eval_count":1}` + "\n"}, llm.ErrContextExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			s := llmtest.NewStream(t, "application/x-ndjson", tc.chunks...)
			resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), func(string) {})
			if resp != nil || err == nil {
				t.Fatalf("a failed stream must be an error without an answer: %+v %v", resp, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var ae *llm.APIError
			if name == "error mid-stream" && (!errors.As(err, &ae) || !strings.Contains(err.Error(), "model runner crashed")) {
				t.Fatalf("err = %v", err)
			}
			if b, _, _ := s.Requests(); len(b) != 1 {
				t.Fatalf("requests = %d", len(b))
			}
		})
	}
}

func TestChatStreamCancel(t *testing.T) {
	s := llmtest.NewStream(t, "application/x-ndjson", `{"message":{"content":"par"},"done":false}`+"\n", llmtest.Hang)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got string
	start := time.Now()
	// The caller cancels once the first text arrives; the server keeps the
	// stream open, so only the cancel can end the call.
	resp, err := streamProvider(t, s).ChatStream(ctx, streamReq(), func(s string) { got += s; cancel() })
	if resp != nil || !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("resp %+v err %v after %s", resp, err, time.Since(start))
	}
	if got != "par" {
		t.Fatal("text before the cancel was not delivered")
	}
}

// A proxy that answers without streaming: the whole answer is accepted.
func TestChatStreamAcceptsAWholeAnswer(t *testing.T) {
	s := llmtest.NewStream(t, "application/json", "{\n  \"model\": \"llama3\",\n  \"message\": {\"role\": \"assistant\", \"content\": \"hi\"},\n  \"done\": true, \"prompt_eval_count\": 3, \"eval_count\": 1\n}")
	var live []string
	resp, err := streamProvider(t, s).ChatStream(context.Background(), streamReq(), func(s string) { live = append(live, s) })
	if err != nil || resp.Message.Content != "hi" || resp.Usage != (llm.Usage{InputTokens: 3, OutputTokens: 1}) || strings.Join(live, "") != "hi" {
		t.Fatalf("resp %+v err %v live %q", resp, err, live)
	}
}
