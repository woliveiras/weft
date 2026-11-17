//go:build e2e

package main

import (
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/llm"
	"strings"
)

// Scripted model only; storage, gateway, approvals and Git execute normally.
func promptJourneyResponse(r *llm.ChatRequest) *llm.ChatResponse {
	for _, m := range r.Messages {
		if m.Role == llm.RoleTool {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Fixture tool outcome: " + m.Content}, StopReason: "stop"}
		}
	}
	name := "filesystem.write"
	args := json.RawMessage(`{"path":"prompt-result.txt","content":"Reviewed prompt fixture change\n"}`)
	if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "deny-tool") {
		name = "shell.exec"
		args = json.RawMessage(`{"command":"echo forbidden"}`)
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "prompt-write", Name: name, Arguments: args}}}, StopReason: "tool_calls"}
}
