// Package protocol implements the Weft Runtime Protocol v1 HTTP boundary.
package protocol

import "encoding/json"

type TurnInput struct {
	GraphID          string                     `json:"graphId"`
	GraphName        string                     `json:"graphName"`
	NodeName         string                     `json:"nodeName"`
	Replica          int                        `json:"replica"`
	Replicas         int                        `json:"replicas"`
	Iteration        int                        `json:"iteration"`
	Workdir          string                     `json:"workdir"`
	PendingStop      bool                       `json:"pendingStop,omitempty"`
	OperatorMessages []string                   `json:"operatorMessages,omitempty"`
	Parameters       map[string]string          `json:"parameters,omitempty"`
	Instructions     json.RawMessage            `json:"instructions,omitempty"`
	Model            ModelRef                   `json:"model"`
	ModelGuidance    string                     `json:"modelGuidance,omitempty"`
	MaxToolCalls     int                        `json:"maxToolCalls,omitempty"`
	Tools            []ToolSchema               `json:"tools"`
	Neighbors        []NeighborNote             `json:"neighbors,omitempty"`
	Outputs          []OutputInfo               `json:"outputs"`
	Config           map[string]json.RawMessage `json:"config,omitempty"`
	Branch           string                     `json:"branch,omitempty"`
	ExecutionID      string                     `json:"executionId"`
	NodeID           string                     `json:"nodeId"`
	Agent            string                     `json:"agent"`
	Prompt           string                     `json:"prompt"`
	System           string                     `json:"system"`
	Inputs           []Message                  `json:"inputs"`
	Callback         struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	} `json:"callback"`
}

type ModelRef struct {
	Provider    string   `json:"provider,omitempty"`
	Name        string   `json:"name,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int      `json:"maxTokens,omitempty"`
}
type NeighborNote struct {
	Replica int    `json:"replica"`
	Text    string `json:"text"`
	At      string `json:"at"`
}
type OutputInfo struct {
	Channel string   `json:"channel"`
	Type    string   `json:"type"`
	Targets []string `json:"targets"`
	Types   []string `json:"types,omitempty"`
	When    string   `json:"when,omitempty"`
}

type Message struct {
	Source   string                     `json:"source"`
	Target   string                     `json:"target"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Payload  json.RawMessage            `json:"payload"`
}

type Emit struct {
	Target   string         `json:"target,omitempty"`
	Channel  string         `json:"channel,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Type     string         `json:"type"`
	Payload  any            `json:"payload"`
}

type TurnResult struct {
	Broadcast string `json:"broadcast,omitempty"`
	Signal    string `json:"signal,omitempty"`
	Retry     bool   `json:"retry,omitempty"`
	// Only runtime-local usage belongs in a result. Weft charges callback calls.
	Usage     *Usage `json:"usage,omitempty"`
	ToolCalls int    `json:"toolCalls,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Emits     []Emit `json:"emits,omitempty"`
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

type Usage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
}
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
type ChatRequest struct {
	Tools       []ToolSchema  `json:"tools,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   int           `json:"maxTokens,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	JSON        bool          `json:"json,omitempty"`
	System      string        `json:"system,omitempty"`
	Messages    []ChatMessage `json:"messages"`
}

type ChatResponse struct {
	Model      string      `json:"model"`
	Usage      Usage       `json:"usage"`
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

// Add records usage from another runtime-local call. The controller accounts for
// model calls made through Client.Chat independently.
func (u *Usage) Add(local Usage) {
	u.InputTokens += local.InputTokens
	u.OutputTokens += local.OutputTokens
	u.CacheReadTokens += local.CacheReadTokens
	u.CacheWriteTokens += local.CacheWriteTokens
}
