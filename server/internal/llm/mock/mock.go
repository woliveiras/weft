// Package mock is a deterministic, offline provider. It lets the whole
// platform - native agent loops, tool calls, emits, routing, stop handshake -
// run with no model and no network, which is what `docker compose up` uses
// by default and what tests rely on. It is not a simulator of model quality;
// it produces the smallest response that keeps a turn moving:
//
//  1. Metadata["mock_response"] set → returned verbatim (scripted tests).
//  2. "connection_check" tool offered (provider test) → call it with word=OK.
//  3. "emit" tool offered and no tool result yet → call emit with a summary
//     of the last user message (a native turn forwards work downstream).
//  4. "finish" tool offered and a tool result exists → call finish.
//  5. JSON mode → {"mock":true,"summary":...}.
//  6. Otherwise → "MOCK: " + summary.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/woliveiras/weft/server/internal/llm"
)

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "mock", Label: "Mock (offline, deterministic)", Description: "Scripted answers for development and tests; never calls a model.",
		Local: true, Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true}, DevOnly: true,
	}, func(c llm.ProviderConfig) (llm.Provider, error) { return New(c), nil })
}

const summaryLen = 300

type Provider struct {
	cfg   llm.ProviderConfig
	calls atomic.Int64
}

func New(cfg llm.ProviderConfig) *Provider {
	if cfg.Name == "" {
		cfg.Name = "mock"
	}
	return &Provider{cfg: cfg}
}

func (p *Provider) Name() string { return p.cfg.Name }

// Calls reports how many Chat calls were served (for tests).
func (p *Provider) Calls() int64 { return p.calls.Load() }

func (p *Provider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n := p.calls.Add(1)
	summary := truncate(llm.LastUserText(req.Messages), summaryLen)
	model := req.Model
	if model == "" {
		model = "mock-1"
	}
	resp := &llm.ChatResponse{Model: model, StopReason: "stop", Message: llm.ChatMessage{Role: llm.RoleAssistant}}

	hasToolResult := false
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			hasToolResult = true
			break
		}
	}
	call := func(name string, args any) {
		b, _ := json.Marshal(args)
		resp.Message.ToolCalls = []llm.ToolCall{{ID: fmt.Sprintf("mock_call_%d", n), Name: name, Arguments: b}}
		resp.StopReason = "tool_calls"
	}

	switch {
	case req.Metadata != nil && req.Metadata["mock_response"] != "":
		resp.Message.Content = req.Metadata["mock_response"]
	case hasTool(req.Tools, "connection_check"):
		call("connection_check", map[string]any{"word": "OK"})
	case hasTool(req.Tools, "emit") && !hasToolResult:
		call("emit", map[string]any{"type": "mock.output", "payload": map[string]any{"summary": summary}})
	case hasTool(req.Tools, "finish") && hasToolResult:
		call("finish", map[string]any{"signal": "", "summary": "mock turn complete"})
	case req.JSON:
		b, _ := json.Marshal(map[string]any{"mock": true, "summary": summary})
		resp.Message.Content = string(b)
	default:
		resp.Message.Content = "MOCK: " + summary
	}

	in := len(req.System)
	for _, m := range req.Messages {
		in += len(m.Content)
	}
	out := len(resp.Message.Content)
	for _, tc := range resp.Message.ToolCalls {
		out += len(tc.Arguments)
	}
	resp.Usage = llm.Usage{InputTokens: in / 4, OutputTokens: out / 4}
	return resp, nil
}

func (p *Provider) Models(context.Context) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "mock-1", Provider: p.cfg.Name}}, nil
}

func hasTool(ts []llm.ToolSchema, name string) bool {
	for _, t := range ts {
		if llm.PlatformToolName(t.Name) == name {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
