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
	"io"
	"net/http"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
)

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "openai", Label: "OpenAI", Description: "OpenAI models through the Chat Completions API.",
		Local: false, DefaultBaseURL: "https://api.openai.com/v1", Key: llm.KeyRequired,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true, Streaming: true}, Settings: []string{llm.SettingContextWindow},
	}, func(c llm.ProviderConfig) (llm.Provider, error) {
		return New(c, Options{DefaultBaseURL: "https://api.openai.com/v1", RequireKey: true})
	})
	llm.RegisterPlugin(llm.Manifest{
		ID: "vllm", Label: "vLLM (OpenAI-compatible)", Description: "Models served by vLLM through its OpenAI-compatible API.",
		Local: true, DefaultBaseURL: "http://localhost:8000/v1", Key: llm.KeyOptional,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true, Streaming: true}, Settings: []string{llm.SettingContextWindow},
	}, func(c llm.ProviderConfig) (llm.Provider, error) {
		return New(c, Options{DefaultBaseURL: "http://localhost:8000/v1"})
	})
	llm.RegisterPlugin(llm.Manifest{
		ID: "lmstudio", Label: "LM Studio (OpenAI-compatible)", Description: "Models served by LM Studio through its OpenAI-compatible API.",
		Local: true, DefaultBaseURL: "http://localhost:1234/v1", Key: llm.KeyOptional,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true, Streaming: true}, Settings: []string{llm.SettingContextWindow},
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
	// NoStreaming answers ChatStream without streaming, for servers whose
	// streaming (or its usage report) is not known to work.
	NoStreaming bool
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

// ContextWindow implements llm.ContextSizer: the configured window, or 0
// when none is set. The Chat Completions API has no field for it, so the
// window is only used to budget and check prompts on this side; an unknown
// window leaves native turns on their default.
func (p *Provider) ContextWindow() int { return max(p.cfg.ContextWindow, 0) }

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
	StreamOptions  *streamOptions  `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type wireResponse struct {
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
}

type wireChoice struct {
	Message struct {
		Role      string         `json:"role"`
		Content   *string        `json:"content"`
		ToolCalls []wireToolCall `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type wireUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// wireChunk is one server-sent event of a streamed answer.
type wireChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   *string         `json:"content"`
			ToolCalls []wireCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage      `json:"usage"`
	Error json.RawMessage `json:"error"`
}

// wireCallDelta is a piece of a tool call: the first one has its id and
// name, the following ones more of its arguments (a JSON string in parts).
type wireCallDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
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
	if err := llm.CheckContext(p.cfg.Name, p.ContextWindow(), req); err != nil {
		return nil, err
	}
	var out wireResponse
	if err := p.http.Do(ctx, http.MethodPost, "/chat/completions", p.buildRequest(req), &out); err != nil {
		return nil, err
	}
	return p.finish(req, &out)
}

// ChatStream implements llm.StreamingProvider with stream: true: server-sent
// chunks carry deltas of the text and of the tool calls (by index), one
// carries the finish reason, and with stream_options.include_usage a last
// chunk carries the usage before "data: [DONE]".
func (p *Provider) ChatStream(ctx context.Context, req *llm.ChatRequest, onText func(string)) (*llm.ChatResponse, error) {
	if p.opts.NoStreaming {
		return p.Chat(ctx, req)
	}
	if err := llm.CheckContext(p.cfg.Name, p.ContextWindow(), req); err != nil {
		return nil, err
	}
	wire := p.buildRequest(req)
	wire.Stream = true
	wire.StreamOptions = &streamOptions{IncludeUsage: true}
	var (
		out      wireResponse
		text     strings.Builder
		calls    []*callBuilder
		byIndex  = map[int]*callBuilder{}
		finished bool
		done     bool
	)
	out.Choices = make([]wireChoice, 1)
	whole := false
	err := p.http.DoStream(ctx, http.MethodPost, "/chat/completions", wire, "text/event-stream", func(body io.Reader, isWhole bool) error {
		if isWhole { // a server that ignores stream answers whole
			whole = true
			if err := llm.DecodeWhole(p.cfg.Name, body, &out); err != nil {
				return err
			}
			if len(out.Choices) > 0 && out.Choices[0].Message.Content != nil && *out.Choices[0].Message.Content != "" && onText != nil {
				onText(*out.Choices[0].Message.Content)
			}
			return nil
		}
		return llm.ReadSSE(body, func(data []byte) error {
			if strings.TrimSpace(string(data)) == "[DONE]" {
				done = true
				return io.EOF
			}
			var ch wireChunk
			if err := json.Unmarshal(data, &ch); err != nil {
				return llm.BadStream(p.cfg.Name, err)
			}
			if len(ch.Error) > 0 && string(ch.Error) != "null" {
				return p.http.StreamError(0, errorMessage(ch.Error))
			}
			if ch.Model != "" {
				out.Model = ch.Model
			}
			if ch.Usage != nil {
				out.Usage = *ch.Usage
			}
			if len(ch.Choices) == 0 {
				return nil
			}
			c := ch.Choices[0]
			if c.Delta.Content != nil && *c.Delta.Content != "" {
				text.WriteString(*c.Delta.Content)
				if onText != nil {
					onText(*c.Delta.Content)
				}
			}
			for _, d := range c.Delta.ToolCalls {
				idx := len(calls)
				switch {
				case d.Index != nil:
					idx = *d.Index
				case d.ID == "" && len(calls) > 0:
					idx = calls[len(calls)-1].index
				}
				b := byIndex[idx]
				if b == nil {
					b = &callBuilder{index: idx}
					byIndex[idx] = b
					calls = append(calls, b)
				}
				b.add(d)
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				out.Choices[0].FinishReason = *c.FinishReason
				finished = true
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if whole {
		return p.finish(req, &out)
	}
	if !finished && !done {
		return nil, llm.Incomplete(p.cfg.Name)
	}
	if text.Len() > 0 || len(calls) == 0 {
		s := text.String()
		out.Choices[0].Message.Content = &s
	}
	for _, b := range calls {
		out.Choices[0].Message.ToolCalls = append(out.Choices[0].Message.ToolCalls, b.call())
	}
	return p.finish(req, &out)
}

// callBuilder assembles one streamed tool call.
type callBuilder struct {
	index int
	id    string
	name  string
	args  []byte
	extra json.RawMessage
}

func (b *callBuilder) add(d wireCallDelta) {
	if b.id == "" {
		b.id = d.ID
	}
	if b.name == "" {
		b.name = d.Function.Name
	}
	if raw := d.Function.Arguments; len(raw) > 0 && string(raw) != "null" {
		var s string
		if raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
			b.args = append(b.args, s...)
		} else {
			b.args = append(b.args, raw...) // a server that sends an object
		}
	}
	if len(d.ExtraContent) > 0 && string(d.ExtraContent) != "null" {
		b.extra = d.ExtraContent
	}
}

// call is the tool call as the non-streamed answer carries it: arguments
// as one JSON string.
func (b *callBuilder) call() wireToolCall {
	var w wireToolCall
	w.ID, w.Type = b.id, "function"
	w.Function.Name = b.name
	w.Function.Arguments, _ = json.Marshal(string(b.args))
	w.ExtraContent = b.extra
	return w
}

// errorMessage reads the error a backend sent inside a stream: an object
// with a message, or anything else as it came.
func errorMessage(raw json.RawMessage) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Message != "" {
		return e.Message
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	return string(raw)
}

// finish turns a whole answer, or one assembled from a stream, into the
// platform response.
func (p *Provider) finish(req *llm.ChatRequest, out *wireResponse) (*llm.ChatResponse, error) {
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
