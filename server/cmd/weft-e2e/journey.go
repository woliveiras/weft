//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
)

// A deterministic model fixture drives the real engine/gateways in order.
// It is not compiled into production and never calls an external provider.
func journeyResponse(r *llm.ChatRequest) (*llm.ChatResponse, error) {
	if r == nil || len(r.Messages) == 0 {
		return nil, fmt.Errorf("journey requires an initial message")
	}
	mode := "required"
	if strings.Contains(r.Messages[0].Content, "journey-automatic") {
		mode = "automatic"
	}
	calls := []llm.ToolCall{
		{ID: "journey-mcp", Name: "mcp.journey-" + mode + ".search", Arguments: json.RawMessage(`{"query":"public fixture"}`)},
		{ID: "journey-child", Name: "delegation.request", Arguments: json.RawMessage(`{"template":"analysis","objective":"Summarize supplied fixture text without tools"}`)},
		{ID: "journey-write", Name: "filesystem.write", Arguments: json.RawMessage(`{"path":"journey.txt","content":"Reviewed fixture result\n"}`)},
	}
	completed := 0
	for _, message := range r.Messages {
		if message.Role != llm.RoleTool {
			continue
		}
		if completed >= len(calls) || message.ToolCallID != calls[completed].ID {
			return nil, fmt.Errorf("unexpected journey tool result order")
		}
		if strings.Contains(message.Content, `"error"`) || strings.Contains(message.Content, `"isError":true`) {
			return nil, fmt.Errorf("journey tool failed")
		}
		completed++
	}
	response := &llm.ChatResponse{Usage: llm.Usage{InputTokens: 9, OutputTokens: 4}}
	if completed == len(calls) {
		response.Message.Content = "Integrated journey completed"
		return response, nil
	}
	response.Message.ToolCalls = []llm.ToolCall{calls[completed]}
	response.StopReason = "tool_calls"
	return response, nil
}
