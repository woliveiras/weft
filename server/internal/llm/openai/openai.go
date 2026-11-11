// Package openai implements the OpenAI Chat Completions wire protocol. It is
// the lingua franca of model serving: OpenAI itself, vLLM, LM Studio,
// llama.cpp server, LocalAI, HuggingFace TGI's Messages API and OpenRouter
// all speak it, so this one adapter covers most backends. Variants differ
// only in base URL, whether a key is needed, and a few headers, which is why
// other packages (openrouter, huggingface) are thin wrappers around New.
//
// Trade-off: we target the widely implemented subset (messages, tools,
// json_object response format, usage) rather than vendor-only features
// (structured outputs with strict schemas, logprobs, audio) so behaviour is
// the same against every compatible server.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
)

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "openai", Label: "OpenAI", Description: "OpenAI models through the Chat Completions API.",
		Local: false, DefaultBaseURL: "https://api.openai.com/v1", Key: llm.KeyRequired,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true},
	}, func(c llm.ProviderConfig) (llm.Provider, error) {
		return New(c, Options{DefaultBaseURL: "https://api.openai.com/v1", RequireKey: true})
	})
	llm.RegisterPlugin(llm.Manifest{
		ID: "vllm", Label: "vLLM (OpenAI-compatible)", Description: "Models served by vLLM through its OpenAI-compatible API.",
		Local: true, DefaultBaseURL: "http://localhost:8000/v1", Key: llm.KeyOptional,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true},
	}, func(c llm.ProviderConfig) (llm.Provider, error) {
		return New(c, Options{DefaultBaseURL: "http://localhost:8000/v1"})
	})
	llm.RegisterPlugin(llm.Manifest{
		ID: "lmstudio", Label: "LM Studio (OpenAI-compatible)", Description: "Models served by LM Studio through its OpenAI-compatible API.",
		Local: true, DefaultBaseURL: "http://localhost:1234/v1", Key: llm.KeyOptional,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true},
	}, func(c llm.ProviderConfig) (llm.Provider, error) {
		return New(c, Options{DefaultBaseURL: "http://localhost:1234/v1"})
	})
}

// Options tailor the client to a compatible backend.
type Options struct {
	DefaultBaseURL string
	// RequireKey fails construction when no key is configured.
	RequireKey bool
	// ExtraHeaders are sent on every request (config headers override).
	ExtraHeaders map[string]string
	// NoJSONMode skips response_format for servers that reject it; a system
	// instruction is used instead.
	NoJSONMode bool
}

// Provider is an OpenAI-compatible chat client.
type Provider struct {
	cfg  llm.ProviderConfig
	opts Options
	http *llm.HTTPClient
}

// New builds an OpenAI-compatible provider.
func New(cfg llm.ProviderConfig, opts Options) (*Provider, error) {
	key, err := cfg.APIKey()
	if err != nil {
		return nil, err
	}
	if key == "" && opts.RequireKey {
		return nil, fmt.Errorf("llm %s: an API key is required (set apiKeyEnv or apiKeyFile)", cfg.Name)
	}
	base := cfg.BaseURL
	if base == "" {
		base = opts.DefaultBaseURL
	}
	h := map[string]string{}
	for k, v := range opts.ExtraHeaders {
		h[k] = v
	}
	if key != "" {
		h["Authorization"] = "Bearer " + key
	}
	return &Provider{cfg: cfg, opts: opts, http: llm.NewHTTPClient(cfg, base, h)}, nil
}

func (p *Provider) Name() string { return p.cfg.Name }

// HTTP exposes the underlying client (tests tune backoff through it).
func (p *Provider) HTTP() *llm.HTTPClient { return p.http }

type wireMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
	// ExtraContent is vendor data on the call (Gemini's thought_signature);
	// it is echoed back verbatim on later turns.
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireRequest struct {
	Model          string          `json:"model"`
	Messages       []wireMessage   `json:"messages"`
	Tools          []wireTool      `json:"tools,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type wireResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role      string         `json:"role"`
			Content   *string        `json:"content"`
			ToolCalls []wireToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

const jsonInstruction = "Respond with a single valid JSON object and nothing else."

// buildRequest maps a platform request to the wire format.
func (p *Provider) buildRequest(req *llm.ChatRequest) *wireRequest {
	w := &wireRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
	}
	if w.Model == "" {
		w.Model = p.cfg.DefaultModel
	}
	system := req.System
	if req.JSON {
		if p.opts.NoJSONMode {
			system = strings.TrimSpace(system + "\n\n" + jsonInstruction)
		} else {
			w.ResponseFormat = &responseFormat{Type: "json_object"}
			// OpenAI requires the word "JSON" to appear in the messages.
			if !strings.Contains(strings.ToLower(system), "json") {
				system = strings.TrimSpace(system + "\n\n" + jsonInstruction)
			}
		}
	}
	if system != "" {
		w.Messages = append(w.Messages, wireMessage{Role: "system", Content: strPtr(system)})
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role)}
		switch m.Role {
		case llm.RoleTool:
			wm.ToolCallID = m.ToolCallID
			wm.Content = strPtr(m.Content)
		case llm.RoleAssistant:
			if m.Content != "" || len(m.ToolCalls) == 0 {
				wm.Content = strPtr(m.Content)
			}
			for _, tc := range m.ToolCalls {
				var wtc wireToolCall
				wtc.ID, wtc.Type = tc.ID, "function"
				wtc.Function.Name = llm.WireToolName(tc.Name)
				// The OpenAI wire format carries arguments as a JSON string.
				s, _ := json.Marshal(string(llm.NormalizeArgs(tc.Arguments)))
				wtc.Function.Arguments = s
				wtc.ExtraContent = tc.ProviderData
				wm.ToolCalls = append(wm.ToolCalls, wtc)
			}
		default:
			wm.Content = strPtr(m.Content)
		}
		w.Messages = append(w.Messages, wm)
	}
	for _, t := range req.Tools {
		var wt wireTool
		wt.Type = "function"
		wt.Function.Name = llm.WireToolName(t.Name)
		wt.Function.Description = t.Description
		wt.Function.Parameters = llm.SchemaOrEmpty(t.Parameters)
		w.Tools = append(w.Tools, wt)
	}
	return w
}

func (p *Provider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	var out wireResponse
	if err := p.http.Do(ctx, http.MethodPost, "/chat/completions", p.buildRequest(req), &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm %s: response has no choices", p.cfg.Name)
	}
	ch := out.Choices[0]
	msg := llm.ChatMessage{Role: llm.RoleAssistant}
	if ch.Message.Content != nil {
		msg.Content = *ch.Message.Content
	}
	for i, tc := range ch.Message.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
			ID:        id,
			Name:      llm.PlatformToolName(tc.Function.Name),
			Arguments: llm.NormalizeArgs(tc.Function.Arguments),
			// Only a JSON object is kept, so arbitrary values never reach a later request.
			ProviderData: objectOrNil(tc.ExtraContent),
		})
	}
	model := out.Model
	if model == "" {
		model = req.Model
	}
	return &llm.ChatResponse{
		Message:    msg,
		StopReason: mapFinish(ch.FinishReason, len(msg.ToolCalls) > 0),
		Model:      model,
		// prompt_tokens already includes the cached part.
		Usage: llm.Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: outputTokens(out.Usage.PromptTokens, out.Usage.CompletionTokens, out.Usage.TotalTokens),
			CacheReadTokens: out.Usage.PromptTokensDetails.CachedTokens},
	}, nil
}

func mapFinish(r string, hasTools bool) string {
	switch r {
	case "tool_calls", "function_call":
		return "tool_calls"
	case "length":
		return "length"
	case "content_filter":
		return "content_filter"
	case "stop", "eos", "":
		if hasTools {
			return "tool_calls"
		}
		return "stop"
	}
	return r
}

func (p *Provider) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	var out struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			MaxModelLen   int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := p.http.Do(ctx, http.MethodGet, "/models", nil, &out); err != nil {
		return nil, err
	}
	res := make([]llm.ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		cs := m.ContextLength
		if cs == 0 {
			cs = m.MaxModelLen
		}
		res = append(res, llm.ModelInfo{ID: m.ID, Provider: p.cfg.Name, ContextSize: cs})
	}
	return res, nil
}

func strPtr(s string) *string { return &s }

// outputTokens is what the answer cost. OpenAI counts reasoning inside
// completion_tokens; Gemini's compatible endpoint leaves its thinking tokens
// out of completion_tokens but bills them and includes them in
// total_tokens. Tokens in the total beyond prompt and completion are
// therefore output too.
func outputTokens(prompt, completion, total int) int {
	if hidden := total - prompt - completion; hidden > 0 {
		return completion + hidden
	}
	return completion
}

func objectOrNil(raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &obj) != nil || len(obj) == 0 {
		return nil
	}
	return raw
}
