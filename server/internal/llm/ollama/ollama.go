// Package ollama implements Ollama's native /api/chat endpoint. Ollama also
// serves an OpenAI-compatible /v1 API (usable through the "openai" type), but
// the native API exposes options (num_ctx, num_predict, format) and returns
// tool-call arguments as objects, and it is what every Ollama version ships.
//
// Ollama does not assign tool-call ids; we synthesise stable ones
// ("call_<n>") so tool results can be correlated by the platform. Ollama
// matches tool results by order, not by id, so this is lossless.
package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/woliveiras/weft/server/internal/llm"
)

const defaultBaseURL = "http://localhost:11434"

// DefaultContextWindow is the num_ctx sent when the provider sets none.
// Ollama's own default (4096, or OLLAMA_CONTEXT_LENGTH) is too small for
// agents: it silently drops the start of the conversation, task included.
const DefaultContextWindow = 32768

// ContextWindow is the num_ctx this provider sends.
func (p *Provider) ContextWindow() int {
	if p.cfg.ContextWindow > 0 {
		return p.cfg.ContextWindow
	}
	return DefaultContextWindow
}

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "ollama", Label: "Ollama", Description: "Models served by Ollama on this machine or network (native /api/chat).",
		Local: true, DefaultBaseURL: "http://localhost:11434", Key: llm.KeyNone,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true}, Settings: []string{llm.SettingContextWindow},
	}, func(c llm.ProviderConfig) (llm.Provider, error) { return New(c) })
}

type Provider struct {
	cfg  llm.ProviderConfig
	http *llm.HTTPClient
}

func New(cfg llm.ProviderConfig) (*Provider, error) {
	key, err := cfg.APIKey()
	if err != nil {
		return nil, err
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	h := map[string]string{}
	if key != "" { // reverse proxies in front of Ollama often need one
		h["Authorization"] = "Bearer " + key
	}
	return &Provider{cfg: cfg, http: llm.NewHTTPClient(cfg, base, h)}, nil
}

func (p *Provider) Name() string          { return p.cfg.Name }
func (p *Provider) HTTP() *llm.HTTPClient { return p.http }

type toolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

type tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type request struct {
	Model    string         `json:"model"`
	Messages []message      `json:"messages"`
	Tools    []tool         `json:"tools,omitempty"`
	Stream   bool           `json:"stream"`
	Format   string         `json:"format,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type response struct {
	Model           string  `json:"model"`
	Message         message `json:"message"`
	DoneReason      string  `json:"done_reason"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	Error           string  `json:"error"`
}

func (p *Provider) buildRequest(req *llm.ChatRequest) *request {
	r := &request{Model: req.Model, Stream: false}
	if r.Model == "" {
		r.Model = p.cfg.DefaultModel
	}
	if req.JSON {
		r.Format = "json"
	}
	opts := map[string]any{"num_ctx": p.ContextWindow()}
	if req.Temperature != nil {
		opts["temperature"] = *req.Temperature
	}
	// Without a limit Ollama generates until the context window is full.
	opts["num_predict"] = llm.DefaultAnswerTokens(p.ContextWindow())
	if req.MaxTokens > 0 {
		opts["num_predict"] = req.MaxTokens
	}
	if len(req.Stop) > 0 {
		opts["stop"] = req.Stop
	}
	r.Options = opts
	if req.System != "" {
		r.Messages = append(r.Messages, message{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		wm := message{Role: string(m.Role), Content: m.Content}
		if m.Role == llm.RoleTool {
			wm.ToolName = llm.WireToolName(m.Name)
		}
		for _, tc := range m.ToolCalls {
			var w toolCall
			w.Function.Name = llm.WireToolName(tc.Name)
			w.Function.Arguments = llm.NormalizeArgs(tc.Arguments)
			wm.ToolCalls = append(wm.ToolCalls, w)
		}
		r.Messages = append(r.Messages, wm)
	}
	for _, t := range req.Tools {
		var w tool
		w.Type = "function"
		w.Function.Name = llm.WireToolName(t.Name)
		w.Function.Description = t.Description
		w.Function.Parameters = llm.SchemaOrEmpty(t.Parameters)
		r.Tools = append(r.Tools, w)
	}
	return r
}

func (p *Provider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	window := p.ContextWindow()
	// Fail before sending what Ollama would truncate without saying so.
	if err := llm.CheckContext(p.cfg.Name, window, req); err != nil {
		return nil, err
	}
	var out response
	if err := p.http.Do(ctx, http.MethodPost, "/api/chat", p.buildRequest(req), &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("llm %s: %s", p.cfg.Name, out.Error)
	}
	// A prompt that filled the window was cut from the start.
	if out.PromptEvalCount >= window {
		return nil, fmt.Errorf("%w: the prompt filled %s's context window of %d tokens and was truncated; raise the provider's context window or shorten the task", llm.ErrContextExceeded, p.cfg.Name, window)
	}
	msg := llm.ChatMessage{Role: llm.RoleAssistant, Content: out.Message.Content}
	for i, tc := range out.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      llm.PlatformToolName(tc.Function.Name),
			Arguments: llm.NormalizeArgs(tc.Function.Arguments),
		})
	}
	stop := "stop"
	switch {
	case len(msg.ToolCalls) > 0:
		stop = "tool_calls"
	case out.DoneReason == "length":
		stop = "length"
	}
	model := out.Model
	if model == "" {
		model = req.Model
	}
	return &llm.ChatResponse{
		Message: msg, StopReason: stop, Model: model,
		Usage: llm.Usage{InputTokens: out.PromptEvalCount, OutputTokens: out.EvalCount},
	}, nil
}

func (p *Provider) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := p.http.Do(ctx, http.MethodGet, "/api/tags", nil, &out); err != nil {
		return nil, err
	}
	res := make([]llm.ModelInfo, 0, len(out.Models))
	for _, m := range out.Models {
		res = append(res, llm.ModelInfo{ID: m.Name, Provider: p.cfg.Name})
	}
	return res, nil
}
