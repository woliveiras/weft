package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestChat(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"llama3","message":{"role":"assistant","content":"",
			"tool_calls":[{"function":{"name":"shell__exec","arguments":{"command":"ls"}}}]},
			"done_reason":"stop","prompt_eval_count":9,"eval_count":4}`))
	}))
	defer srv.Close()
	p, _ := New(llm.ProviderConfig{Name: "ol", BaseURL: srv.URL, DefaultModel: "llama3"})
	temp := 0.2
	resp, err := p.Chat(context.Background(), &llm.ChatRequest{
		System: "s", JSON: true, Temperature: &temp, MaxTokens: 50,
		Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "go"}},
		Tools:    []llm.ToolSchema{{Name: "shell.exec"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != "tool_calls" || resp.Message.ToolCalls[0].ID != "call_0" ||
		resp.Message.ToolCalls[0].Name != "shell.exec" || string(resp.Message.ToolCalls[0].Arguments) != `{"command":"ls"}` {
		t.Fatalf("resp %+v", resp)
	}
	if resp.Usage.InputTokens != 9 || resp.Usage.OutputTokens != 4 {
		t.Errorf("usage %+v", resp.Usage)
	}
	if got["stream"] != false || got["format"] != "json" || got["model"] != "llama3" {
		t.Errorf("req %v", got)
	}
	opts := got["options"].(map[string]any)
	if opts["num_predict"].(float64) != 50 || opts["temperature"].(float64) != 0.2 {
		t.Errorf("options %v", opts)
	}
}

func TestRetry5xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done_reason":"stop"}`))
	}))
	defer srv.Close()
	p, _ := New(llm.ProviderConfig{Name: "ol", BaseURL: srv.URL})
	p.HTTP().BaseDelay = time.Millisecond
	resp, err := p.Chat(context.Background(), &llm.ChatRequest{Model: "m"})
	if err != nil || resp.Message.Content != "hi" || n.Load() != 2 {
		t.Fatalf("%+v %v %d", resp, err, n.Load())
	}
}

func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3:8b"}]}`))
	}))
	defer srv.Close()
	p, _ := New(llm.ProviderConfig{Name: "ol", BaseURL: srv.URL})
	ms, err := p.Models(context.Background())
	if err != nil || len(ms) != 1 || ms[0].ID != "llama3:8b" {
		t.Fatalf("%v %v", ms, err)
	}
}

func TestContextWindow(t *testing.T) {
	var got map[string]any
	var calls atomic.Int32
	evalCount := 100
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done_reason":"stop","prompt_eval_count":` +
			strconv.Itoa(evalCount) + `}`))
	}))
	defer srv.Close()
	ask := func(p *Provider, content string) error {
		_, err := p.Chat(context.Background(), &llm.ChatRequest{Model: "m", Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: content}}})
		return err
	}

	// Default: a window large enough for agents, always sent explicitly.
	def, _ := New(llm.ProviderConfig{Name: "ol", BaseURL: srv.URL})
	if err := ask(def, "hi"); err != nil {
		t.Fatal(err)
	}
	if got["options"].(map[string]any)["num_ctx"].(float64) != DefaultContextWindow {
		t.Fatalf("default num_ctx: %v", got["options"])
	}
	// Without MaxTokens the answer is still bounded: Ollama would otherwise
	// generate until the window is full (a runaway answer took 30k tokens).
	if got["options"].(map[string]any)["num_predict"].(float64) != 4096 {
		t.Fatalf("default num_predict: %v", got["options"])
	}

	// Configured window.
	small, _ := New(llm.ProviderConfig{Name: "ol", BaseURL: srv.URL, ContextWindow: 2048})
	if err := ask(small, "hi"); err != nil || got["options"].(map[string]any)["num_ctx"].(float64) != 2048 || got["options"].(map[string]any)["num_predict"].(float64) != 256 {
		t.Fatalf("configured num_ctx: %v %v", err, got["options"])
	}

	// A prompt that cannot fit is refused before it is sent.
	before := calls.Load()
	err := ask(small, strings.Repeat("x", 4*2048))
	if !errors.Is(err, llm.ErrContextExceeded) || !strings.Contains(err.Error(), "context window of 2048") || calls.Load() != before {
		t.Fatalf("oversized prompt: %v (requests %d→%d)", err, before, calls.Load())
	}

	// A prompt Ollama reports as filling the window was truncated.
	evalCount = 2048
	if err := ask(small, "short"); !errors.Is(err, llm.ErrContextExceeded) || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("truncated prompt: %v", err)
	}
}
