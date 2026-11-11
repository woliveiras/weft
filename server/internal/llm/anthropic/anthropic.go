// Package anthropic implements the Anthropic Messages API. Any endpoint
// that speaks the same protocol (proxies, gateways, self-hosted
// compatibles) works by changing baseUrl.
//
// Mapping notes and trade-offs:
//   - The system prompt is a top-level field, not a message.
//   - Tool results are content blocks inside a *user* turn; consecutive
//     platform RoleTool messages are merged into one user turn because the
//     API requires strict user/assistant alternation.
//   - There is no JSON response-format switch; JSON mode is an instruction
//     appended to the system prompt. Callers must still validate the output.
//   - max_tokens is mandatory on this API; we default to 4096.
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	apiVersion       = "2023-06-01"
	defaultMaxTokens = 4096
)

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "anthropic", Label: "Anthropic", Description: "Claude models through the Anthropic Messages API.",
		DefaultBaseURL: "https://api.anthropic.com", Key: llm.KeyRequired,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONInstruction, Usage: true, ModelListing: true}, Settings: []string{llm.SettingContextWindow},
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
		if key == "" {
			return nil, fmt.Errorf("llm %s: an API key is required for %s", cfg.Name, defaultBaseURL)
		}
	}
	base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")
	h := map[string]string{"anthropic-version": apiVersion}
	if key != "" {
		h["x-api-key"] = key
	}
	return &Provider{cfg: cfg, http: llm.NewHTTPClient(cfg, base, h)}, nil
}

func (p *Provider) Name() string          { return p.cfg.Name }
func (p *Provider) HTTP() *llm.HTTPClient { return p.http }

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type message struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type request struct {
	Model         string    `json:"model"`
	System        string    `json:"system,omitempty"`
	Messages      []message `json:"messages"`
	Tools         []tool    `json:"tools,omitempty"`
	MaxTokens     int       `json:"max_tokens"`
	Temperature   *float64  `json:"temperature,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
}

type response struct {
	Model      string  `json:"model"`
	Content    []block `json:"content"`
	StopReason string  `json:"stop_reason"`
	Usage      struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (p *Provider) buildRequest(req *llm.ChatRequest) *request {
	r := &request{
		Model:         req.Model,
		System:        req.System,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		StopSequences: req.Stop,
	}
	if r.Model == "" {
		r.Model = p.cfg.DefaultModel
	}
	if r.MaxTokens <= 0 {
		r.MaxTokens = defaultMaxTokens
	}
	if req.JSON {
		r.System = strings.TrimSpace(r.System + "\n\nRespond with a single valid JSON object and nothing else - no prose, no code fences.")
	}
	appendBlocks := func(role string, bs ...block) {
		if n := len(r.Messages); n > 0 && r.Messages[n-1].Role == role {
			r.Messages[n-1].Content = append(r.Messages[n-1].Content, bs...)
			return
		}
		r.Messages = append(r.Messages, message{Role: role, Content: bs})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleSystem:
			// Mid-conversation system text: fold into the system prompt.
			r.System = strings.TrimSpace(r.System + "\n\n" + m.Content)
		case llm.RoleTool:
			appendBlocks("user", block{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content})
		case llm.RoleAssistant:
			var bs []block
			if m.Content != "" {
				bs = append(bs, block{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				bs = append(bs, block{Type: "tool_use", ID: tc.ID, Name: llm.WireToolName(tc.Name), Input: llm.NormalizeArgs(tc.Arguments)})
			}
			if len(bs) == 0 {
				bs = []block{{Type: "text", Text: " "}}
			}
			appendBlocks("assistant", bs...)
		default:
			text := m.Content
			if text == "" {
				text = " "
			}
			appendBlocks("user", block{Type: "text", Text: text})
		}
	}
	for _, t := range req.Tools {
		r.Tools = append(r.Tools, tool{Name: llm.WireToolName(t.Name), Description: t.Description, InputSchema: llm.SchemaOrEmpty(t.Parameters)})
	}
	return r
}

// ContextWindow implements llm.ContextSizer: the configured window, or 0
// when none is set. The Messages API has no field for it, so the window is
// only used to budget and check prompts on this side.
func (p *Provider) ContextWindow() int { return max(p.cfg.ContextWindow, 0) }

func (p *Provider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := llm.CheckContext(p.cfg.Name, p.ContextWindow(), req); err != nil {
		return nil, err
	}
	var out response
	if err := p.http.Do(ctx, http.MethodPost, "/v1/messages", p.buildRequest(req), &out); err != nil {
		return nil, err
	}
	msg := llm.ChatMessage{Role: llm.RoleAssistant}
	var texts []string
	for _, b := range out.Content {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
				ID: b.ID, Name: llm.PlatformToolName(b.Name), Arguments: llm.NormalizeArgs(b.Input),
			})
		}
	}
	msg.Content = strings.Join(texts, "")
	model := out.Model
	if model == "" {
		model = req.Model
	}
	return &llm.ChatResponse{
		Message:    msg,
		StopReason: mapStop(out.StopReason),
		Model:      model,
		// input_tokens excludes cache reads and writes; count them all.
		Usage: llm.Usage{
			InputTokens:     out.Usage.InputTokens + out.Usage.CacheReadInputTokens + out.Usage.CacheCreationInputTokens,
			OutputTokens:    out.Usage.OutputTokens,
			CacheReadTokens: out.Usage.CacheReadInputTokens, CacheWriteTokens: out.Usage.CacheCreationInputTokens,
		},
	}, nil
}

func mapStop(s string) string {
	switch s {
	case "tool_use":
		return "tool_calls"
	case "end_turn", "stop_sequence", "":
		return "stop"
	case "max_tokens":
		return "length"
	case "refusal":
		return "content_filter"
	}
	return s
}

func (p *Provider) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := p.http.Do(ctx, http.MethodGet, "/v1/models", nil, &out); err != nil {
		return nil, err
	}
	res := make([]llm.ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		res = append(res, llm.ModelInfo{ID: m.ID, Provider: p.cfg.Name})
	}
	return res, nil
}
