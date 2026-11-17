// Package llm is the model-provider abstraction. One interface, Provider,
// is everything a new backend must implement. The rest of the platform -
// native agents, and external agents through the LLM gateway endpoint -
// speaks only this vocabulary, so swapping Ollama for vLLM or Anthropic for
// OpenRouter is a config change, never a code change.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Provider is implemented by every model backend.
type Provider interface {
	// Name is the configured instance name ("local-ollama"), not the type.
	Name() string
	// Chat performs one completion, optionally with tool definitions. A
	// response either carries text, tool calls, or both.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// Models lists models the backend offers (best effort; may be empty).
	Models(ctx context.Context) ([]ModelInfo, error)
}

// Role of a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ChatMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// ToolCalls is set on assistant messages that request tool invocations.
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	// ToolCallID links a RoleTool message to the call it answers.
	ToolCallID string `json:"toolCallId,omitempty"`
	// Name of the tool for RoleTool messages.
	Name string `json:"name,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"` // platform name, e.g. "filesystem.read"
	Arguments json.RawMessage `json:"arguments"`
	// ProviderData is opaque provider state attached to the call that must
	// be sent back unchanged with the conversation (Gemini 3's
	// thought_signature in extra_content). Runtimes keep it, never read it.
	ProviderData json.RawMessage `json:"providerData,omitempty"`
}

// ToolSchema describes a tool to the model (JSON Schema for parameters).
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	System      string        `json:"system,omitempty"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolSchema  `json:"tools,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   int           `json:"maxTokens,omitempty"`
	// JSON asks for a JSON object response where the backend supports it.
	JSON bool `json:"json,omitempty"`
	// Stop sequences.
	Stop []string `json:"stop,omitempty"`
	// Metadata is passed through for logging/tracing, never to the model.
	Metadata map[string]string `json:"metadata,omitempty"`
}

type ChatResponse struct {
	Message    ChatMessage `json:"message"`
	StopReason string      `json:"stopReason"` // "stop", "tool_calls", "length", ...
	Model      string      `json:"model"`
	Usage      Usage       `json:"usage"`
}

// Usage is what one call consumed. InputTokens counts every prompt token the
// backend processed, including the cached ones; CacheReadTokens and
// CacheWriteTokens are the parts of it served from or written to the
// provider's prompt cache, when the provider reports them.
type Usage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
}

type ModelInfo struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	ContextSize int    `json:"contextSize,omitempty"`
}

// ProviderConfig is one entry of the "providers" list in platform config.
type ProviderConfig struct {
	Name    string `json:"name" yaml:"name"`
	Type    string `json:"type" yaml:"type"` // openai | anthropic | ollama | openrouter | huggingface | vllm | mock
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	// APIKeyEnv names the environment variable holding the key. Keys are
	// never written in config files and never forwarded to agents.
	APIKeyEnv string `json:"apiKeyEnv,omitempty" yaml:"apiKeyEnv,omitempty"`
	// APIKeyFile reads the key from a file (Docker/K8s secrets).
	APIKeyFile string `json:"apiKeyFile,omitempty" yaml:"apiKeyFile,omitempty"`
	// APIKeyValue is a key decrypted in memory from a credential the
	// operator stored through the console. It is never marshalled.
	APIKeyValue  string            `json:"-" yaml:"-"`
	DefaultModel string            `json:"defaultModel,omitempty" yaml:"defaultModel,omitempty"`
	Headers      map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	TimeoutSec   int               `json:"timeoutSec,omitempty" yaml:"timeoutSec,omitempty"`
	MaxRetries   int               `json:"maxRetries,omitempty" yaml:"maxRetries,omitempty"`
	// Concurrency caps simultaneous requests to this backend (0 = unlimited).
	Concurrency int `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	// ContextWindow is the context size in tokens the backend should use for
	// a request (prompt plus answer). Providers that let the client choose it
	// (Ollama's num_ctx) send it; the others report it to native turns, which
	// budget their conversation against it. 0 means the provider's default.
	// Prompts that do not fit fail with ErrContextExceeded before sending.
	ContextWindow int `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`
	// ModelGuidance tunes how native agents talk to this provider's models:
	// "" (standard) or GuidanceSmall.
	ModelGuidance string `json:"modelGuidance,omitempty" yaml:"modelGuidance,omitempty"`
	// Project and Region select a cloud project and its region for plugins
	// that declare SettingProject and SettingRegion (Gemini on Vertex AI).
	// They are identifiers, never credentials.
	Project string `json:"project,omitempty" yaml:"project,omitempty"`
	Region  string `json:"region,omitempty" yaml:"region,omitempty"`
}

// GuidanceSmall asks native agents to use compact tool schemas, argument
// examples and one tool call per round: what small local models need.
const GuidanceSmall = "small"

// ErrContextExceeded: a request does not fit the model's context window.
// Backends that truncate silently (Ollama) would otherwise drop the start of
// the conversation, including the task.
var ErrContextExceeded = errors.New("prompt exceeds the model context window")

// EstimateTokens is a deliberately rough count (about 4 bytes per token)
// used to catch prompts that cannot fit before sending them.
func EstimateTokens(req *ChatRequest) int {
	n := len(req.System)
	for _, m := range req.Messages {
		n += len(m.Content) + len(m.Name) + 8
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(tc.Arguments)
		}
	}
	for _, t := range req.Tools {
		n += len(t.Name) + len(t.Description) + len(t.Parameters)
	}
	return n / 4
}

// ContextSizer is implemented by providers that know the context window
// they send requests with (Ollama's num_ctx) or that were configured with
// one. ContextWindow returns 0 when the window is unknown.
type ContextSizer interface {
	ContextWindow() int
}

// CheckContext fails with ErrContextExceeded when req clearly cannot fit a
// window of the given size, before it is sent. window 0 (unknown) never
// fails. The estimate is rough, so only prompts that reach the whole window
// fail here; the backend still rejects the rest itself.
func CheckContext(provider string, window int, req *ChatRequest) error {
	if window <= 0 {
		return nil
	}
	if est := EstimateTokens(req); est >= window {
		return fmt.Errorf("%w: about %d tokens for %s's context window of %d; raise the provider's context window or shorten the task", ErrContextExceeded, est, provider, window)
	}
	return nil
}

// ContextWindowOf returns p's context window in tokens, or 0 when the
// provider does not say.
func ContextWindowOf(p Provider) int {
	if s, ok := p.(ContextSizer); ok {
		return s.ContextWindow()
	}
	return 0
}

// DefaultAnswerTokens is the answer budget for a request that sets no
// MaxTokens: an eighth of the context window, between 256 and 8192 tokens.
// Native turns keep this much of the window free for the answer, and
// providers that would otherwise generate until the window is full (Ollama)
// stop there, so a degenerate answer cannot run for many minutes.
func DefaultAnswerTokens(window int) int {
	return max(256, min(window/8, 8192))
}

// Factory builds a Provider from its config. Register one per type.
type Factory func(cfg ProviderConfig) (Provider, error)

// WireToolName maps platform tool names ("filesystem.read") to the
// character set every vendor accepts for function names (^[a-zA-Z0-9_-]+$).
func WireToolName(name string) string { return strings.ReplaceAll(name, ".", "__") }

// PlatformToolName reverses WireToolName.
func PlatformToolName(wire string) string { return strings.ReplaceAll(wire, "__", ".") }
