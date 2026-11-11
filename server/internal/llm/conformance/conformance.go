// Package conformance is the shared test suite every built-in model provider
// passes. It drives a provider through llm.Provider only, against a scripted
// fake backend that speaks the provider's wire protocol, and checks the
// contract the gateway relies on: text and tool-call answers, tool results
// sent back, JSON mode, usage, model listing, error mapping and retries,
// cancellation, timeouts and credential redaction.
//
// Adapter tests call Run with the Wire for their protocol and a constructor:
//
//	conformance.Run(t, conformance.OpenAI, func(c llm.ProviderConfig) (llm.Provider, error) {
//		return openai.New(c, openai.Options{RequireKey: true})
//	})
//
// RunLive checks a real backend (for example a local Ollama) with the subset
// of the contract a real model can be held to.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

// Answer is what the scripted backend replies to one chat request.
type Answer struct {
	Text     string
	ToolCall *llm.ToolCall // platform tool name, JSON object arguments
	Usage    llm.Usage
	// Status, when set, answers with that HTTP error status; the body
	// echoes the credential the request carried, as some vendors do.
	Status int
	Delay  time.Duration
}

// Observed is the request the backend received, reduced to what the
// contract checks.
type Observed struct {
	Path       string
	Credential string // value of the credential header, "" when none
	Model      string
	System     string
	Roles      []string // non-system message roles in order
	ToolNames  []string // wire names of the offered tools
	ToolResult string   // content of the last tool-result message
	ToolCallID string   // call id the tool result answers ("" when the wire has none)
	JSON       bool     // JSON mode requested (parameter or instruction)
	Raw        string
}

// Wire is one provider wire protocol.
type Wire struct {
	Name string
	// Keyless protocols never send a credential (Ollama).
	Keyless bool
	// ChatPath and ModelsPath are matched as path suffixes.
	ChatPath, ModelsPath string
	// IDs reports whether tool results carry the call id on the wire.
	IDs bool
	// CacheRead and CacheWrite report whether the protocol reports prompt
	// cache reads and writes.
	CacheRead, CacheWrite bool
	Decode                func(r *http.Request, body map[string]any) Observed
	EncodeChat            func(a Answer) any
	EncodeModels          func(ids []string) any
}

// Backend is a scripted fake model server for one wire protocol.
type Backend struct {
	*httptest.Server
	wire Wire

	mu       sync.Mutex
	answers  []Answer
	observed []Observed
	chats    int
}

// NewBackend starts a backend; it is closed when the test ends.
func NewBackend(t *testing.T, w Wire) *Backend {
	t.Helper()
	b := &Backend{wire: w}
	b.Server = httptest.NewServer(http.HandlerFunc(b.handle))
	t.Cleanup(b.Close)
	return b
}

// Script queues answers for the next chat requests; the last one repeats.
func (b *Backend) Script(a ...Answer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.answers = append([]Answer(nil), a...)
}

// Observed returns every chat request received so far.
func (b *Backend) Observed() []Observed {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Observed(nil), b.observed...)
}

func (b *Backend) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, b.wire.ModelsPath) {
		_ = json.NewEncoder(w).Encode(b.wire.EncodeModels([]string{"conf-small", "conf-large"}))
		return
	}
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, b.wire.ChatPath) {
		http.Error(w, `{"error":"unexpected path"}`, http.StatusNotFound)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	obs := b.wire.Decode(r, body)
	obs.Path, obs.Raw = r.URL.Path, string(raw)
	b.mu.Lock()
	b.observed = append(b.observed, obs)
	a := Answer{Text: "OK"}
	if n := len(b.answers); n > 0 {
		a = b.answers[min(b.chats, n-1)]
	}
	b.chats++
	b.mu.Unlock()
	if a.Delay > 0 {
		select {
		case <-time.After(a.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if a.Status != 0 {
		w.WriteHeader(a.Status)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"rejected credential %s"}}`, obs.Credential)
		return
	}
	_ = json.NewEncoder(w).Encode(b.wire.EncodeChat(a))
}

// New builds the provider under test from a config.
type New func(cfg llm.ProviderConfig) (llm.Provider, error)

const key = "conformance-secret-key"

// Run executes the suite for one provider type.
func Run(t *testing.T, w Wire, build New) {
	t.Helper()
	setup := func(t *testing.T, cfg llm.ProviderConfig) (*Backend, llm.Provider) {
		t.Helper()
		b := NewBackend(t, w)
		cfg.Name, cfg.BaseURL, cfg.DefaultModel = "conf", b.URL+"/v1", "conf-small"
		if cfg.MaxRetries == 0 {
			cfg.MaxRetries = -1
		}
		if !w.Keyless {
			cfg.APIKeyValue = key
		}
		p, err := build(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if h, ok := p.(interface{ HTTP() *llm.HTTPClient }); ok {
			h.HTTP().BaseDelay = time.Millisecond
		}
		return b, p
	}
	user := []llm.ChatMessage{{Role: llm.RoleUser, Content: "Say OK"}}

	t.Run("text", func(t *testing.T) {
		b, p := setup(t, llm.ProviderConfig{})
		b.Script(Answer{Text: "hello", Usage: llm.Usage{InputTokens: 20, OutputTokens: 3, CacheReadTokens: 8, CacheWriteTokens: 4}})
		resp, err := p.Chat(context.Background(), &llm.ChatRequest{System: "be brief", Messages: user})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Message.Content != "hello" || resp.StopReason != "stop" || len(resp.Message.ToolCalls) != 0 {
			t.Fatalf("response = %+v", resp)
		}
		want := llm.Usage{InputTokens: 20, OutputTokens: 3}
		if w.CacheRead {
			want.CacheReadTokens = 8
		}
		if w.CacheWrite {
			want.CacheWriteTokens = 4
		}
		if resp.Usage != want {
			t.Fatalf("usage = %+v", resp.Usage)
		}
		o := b.Observed()[0]
		if o.Model != "conf-small" || !strings.Contains(o.System, "be brief") || strings.Join(o.Roles, ",") != "user" {
			t.Fatalf("request = %+v", o)
		}
		if !w.Keyless && !strings.Contains(o.Credential, key) {
			t.Fatalf("credential not sent in its header: %q", o.Credential)
		}
		if strings.Contains(o.Raw, key) {
			t.Fatal("credential leaked into the request body")
		}
	})

	t.Run("tool call round trip", func(t *testing.T) {
		b, p := setup(t, llm.ProviderConfig{})
		b.Script(Answer{ToolCall: &llm.ToolCall{ID: "call-7", Name: "fixture.echo", Arguments: json.RawMessage(`{"word":"OK"}`)}})
		resp, err := p.Chat(context.Background(), &llm.ChatRequest{
			Model: "conf-large",
			Messages: []llm.ChatMessage{
				{Role: llm.RoleUser, Content: "echo"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "fixture.echo", Arguments: json.RawMessage(`{"word":"hi"}`)}}},
				{Role: llm.RoleTool, ToolCallID: "call-1", Name: "fixture.echo", Content: "result-42"},
			},
			Tools: []llm.ToolSchema{{Name: "fixture.echo", Description: "Echo a word", Parameters: json.RawMessage(`{"type":"object","properties":{"word":{"type":"string"}}}`)}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.StopReason != "tool_calls" || len(resp.Message.ToolCalls) != 1 {
			t.Fatalf("response = %+v", resp)
		}
		tc := resp.Message.ToolCalls[0]
		var args map[string]string
		if tc.Name != "fixture.echo" || tc.ID == "" || json.Unmarshal(tc.Arguments, &args) != nil || args["word"] != "OK" {
			t.Fatalf("tool call = %+v (%s)", tc, tc.Arguments)
		}
		o := b.Observed()[0]
		if o.Model != "conf-large" || len(o.ToolNames) != 1 || o.ToolNames[0] != llm.WireToolName("fixture.echo") {
			t.Fatalf("offered tools = %+v", o)
		}
		if strings.Join(o.Roles, ",") != "user,assistant,tool" || o.ToolResult != "result-42" || (w.IDs && o.ToolCallID != "call-1") {
			t.Fatalf("tool result not sent back: %+v", o)
		}
	})

	t.Run("json mode", func(t *testing.T) {
		b, p := setup(t, llm.ProviderConfig{})
		b.Script(Answer{Text: `{"ok":true}`})
		resp, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: user, JSON: true})
		if err != nil || resp.Message.Content != `{"ok":true}` {
			t.Fatalf("response = %+v, %v", resp, err)
		}
		if o := b.Observed()[0]; !o.JSON {
			t.Fatalf("JSON mode not requested: %s", o.Raw)
		}
		if _, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: user}); err != nil {
			t.Fatal(err)
		}
		if o := b.Observed()[1]; o.JSON {
			t.Fatalf("JSON mode requested without asking: %s", o.Raw)
		}
	})

	t.Run("models", func(t *testing.T) {
		_, p := setup(t, llm.ProviderConfig{})
		ms, err := p.Models(context.Background())
		if err != nil || len(ms) != 2 || ms[0].ID != "conf-small" || ms[1].ID != "conf-large" {
			t.Fatalf("models = %+v, %v", ms, err)
		}
	})

	t.Run("client error is not retried and is redacted", func(t *testing.T) {
		b, p := setup(t, llm.ProviderConfig{MaxRetries: 3})
		b.Script(Answer{Status: http.StatusUnauthorized})
		_, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: user})
		var apiErr *llm.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || apiErr.Retryable {
			t.Fatalf("err = %v", err)
		}
		if strings.Contains(err.Error(), key) {
			t.Fatalf("credential in error: %v", err)
		}
		if n := len(b.Observed()); n != 1 {
			t.Fatalf("401 retried: %d requests", n)
		}
	})

	t.Run("server error is retried", func(t *testing.T) {
		b, p := setup(t, llm.ProviderConfig{MaxRetries: 2})
		b.Script(Answer{Status: http.StatusServiceUnavailable}, Answer{Text: "recovered"})
		resp, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: user})
		if err != nil || resp.Message.Content != "recovered" || len(b.Observed()) != 2 {
			t.Fatalf("response = %+v, %v after %d requests", resp, err, len(b.Observed()))
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		b, p := setup(t, llm.ProviderConfig{})
		b.Script(Answer{Text: "late", Delay: 5 * time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		start := time.Now()
		_, err := p.Chat(ctx, &llm.ChatRequest{Messages: user})
		if !errors.Is(err, context.Canceled) || time.Since(start) > 2*time.Second {
			t.Fatalf("err = %v after %s", err, time.Since(start))
		}
	})

	t.Run("timeout", func(t *testing.T) {
		b, p := setup(t, llm.ProviderConfig{TimeoutSec: 1})
		b.Script(Answer{Text: "late", Delay: 5 * time.Second})
		start := time.Now()
		_, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: user})
		if err == nil || !strings.Contains(err.Error(), "timed out") || time.Since(start) > 3*time.Second {
			t.Fatalf("err = %v after %s", err, time.Since(start))
		}
	})
}

// RunLive checks a real backend. Real models are not deterministic, so it
// holds them only to what agents need: an answer, a structured tool call
// when one is clearly asked for, valid JSON in JSON mode, reported usage,
// the model listed, and prompt cancellation.
// liveAnswerTokens leaves room for models that reason before answering
// (their hidden thinking counts against max_tokens).
const liveAnswerTokens = 1024

func RunLive(t *testing.T, p llm.Provider, model string) {
	t.Helper()
	ctx := context.Background()
	t.Run("models", func(t *testing.T) {
		ms, err := p.Models(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ms {
			// Gemini lists models as models/<id> and accepts <id>.
			if strings.TrimPrefix(m.ID, "models/") == strings.TrimPrefix(model, "models/") {
				return
			}
		}
		t.Fatalf("model %q not listed: %+v", model, ms)
	})
	t.Run("text and usage", func(t *testing.T) {
		resp, err := p.Chat(ctx, &llm.ChatRequest{Model: model, MaxTokens: liveAnswerTokens, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "Reply with the single word OK."}}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(resp.Message.Content) == "" || resp.Usage.InputTokens == 0 || resp.Usage.OutputTokens == 0 {
			t.Fatalf("response = %+v", resp)
		}
	})
	t.Run("tool call", func(t *testing.T) {
		resp, err := p.Chat(ctx, &llm.ChatRequest{
			Model:    model,
			System:   "You can only act through tools. Always call the provided tool.",
			Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "Use the echo tool to echo the word OK."}},
			Tools: []llm.ToolSchema{{Name: "fixture.echo", Description: "Echo a word back.",
				Parameters: json.RawMessage(`{"type":"object","properties":{"word":{"type":"string"}},"required":["word"]}`)}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Message.ToolCalls) == 0 || resp.Message.ToolCalls[0].Name != "fixture.echo" || !json.Valid(resp.Message.ToolCalls[0].Arguments) {
			t.Fatalf("no structured tool call (the model may not support tool calling): %+v", resp.Message)
		}
	})
	t.Run("json mode", func(t *testing.T) {
		resp, err := p.Chat(ctx, &llm.ChatRequest{Model: model, JSON: true, MaxTokens: liveAnswerTokens,
			Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: `Return a JSON object {"ok": true}.`}}})
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(resp.Message.Content)), &v) != nil {
			t.Fatalf("not a JSON object: %q", resp.Message.Content)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		c, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		_, err := p.Chat(c, &llm.ChatRequest{Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "Write a long story."}}})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}
