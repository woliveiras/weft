// Package protocol implements the Weft Runtime Protocol v1 HTTP boundary.
package protocol

import "encoding/json"

type TurnInput struct {
	Config      map[string]json.RawMessage `json:"config,omitempty"`
	Branch      string                     `json:"branch,omitempty"`
	ExecutionID string                     `json:"executionId"`
	NodeID      string                     `json:"nodeId"`
	Agent       string                     `json:"agent"`
	Prompt      string                     `json:"prompt"`
	System      string                     `json:"system"`
	Inputs      []Message                  `json:"inputs"`
	Callback    struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	} `json:"callback"`
}

type Message struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type Emit struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type TurnResult struct {
	Summary string `json:"summary,omitempty"`
	Emits   []Emit `json:"emits,omitempty"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"`
}

type ToolCall struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Arguments    json.RawMessage `json:"arguments"`
	ProviderData json.RawMessage `json:"providerData,omitempty"`
}

type ChatRequest struct {
	JSON     bool          `json:"json,omitempty"`
	System   string        `json:"system,omitempty"`
	Messages []ChatMessage `json:"messages"`
}

type ChatResponse struct {
	Message    ChatMessage `json:"message"`
	StopReason string      `json:"stopReason"`
}

type ToolResult struct {
	Content   string          `json:"content"`
	Data      json.RawMessage `json:"data"`
	IsError   bool            `json:"isError"`
	Truncated bool            `json:"truncated"`
	Yield     bool            `json:"yield"`
}
