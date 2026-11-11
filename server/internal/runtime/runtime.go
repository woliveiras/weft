// Package runtime abstracts how an agent's turn is executed. The engine owns
// the loop (when to take a turn, which messages to claim, acking, retries,
// stop conditions); a Runtime owns exactly one thing - turning inputs into
// outputs for one turn. That split is what keeps the platform framework
// agnostic: LangGraph, AutoGen, CrewAI or hand-written code only ever see a
// TurnInput and return a TurnResult, and they reach tools and models only
// through the TurnEnv the engine hands them.
package runtime

import (
	"context"
	"encoding/json"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Runtime executes turns for nodes whose RuntimeRef.Kind matches Kind().
type Runtime interface {
	Kind() domain.RuntimeKind
	// Turn executes one turn. It must honour ctx (turn timeout, kill).
	Turn(ctx context.Context, in *TurnInput, env TurnEnv) (*TurnResult, error)
	// Prepare is called when a replica starts (launch processes, warm up).
	Prepare(ctx context.Context, rc ReplicaContext) error
	// Release is called when a replica stops.
	Release(ctx context.Context, rc ReplicaContext) error
}

// ReplicaContext identifies a replica for Prepare/Release.
type ReplicaContext struct {
	Graph   *domain.Graph
	Node    *domain.Node
	Replica int
	Workdir string
}

// TurnInput is everything an agent knows at the start of a turn. It is also
// the request body of the external Runtime Protocol (POST /v1/turn).
type TurnInput struct {
	Instructions *domain.InstructionSnapshot `json:"instructions,omitempty"`

	ExecutionID string `json:"executionId"`
	GraphID     string `json:"graphId"`
	GraphName   string `json:"graphName"`
	NodeID      string `json:"nodeId"`
	NodeName    string `json:"nodeName"`
	Replica     int    `json:"replica"`
	Replicas    int    `json:"replicas"`
	Iteration   int    `json:"iteration"`
	// Agent is the external agent name (RuntimeRef.Agent).
	Agent string `json:"agent,omitempty"`
	// Prompt is the node prompt with parameters substituted.
	Prompt string `json:"prompt"`
	// System is the platform preamble: the agent's situation, its inputs and
	// outputs, its tools, the stop protocol.
	System string `json:"system"`
	// Inputs are the messages claimed for this turn.
	Inputs []*domain.Message `json:"inputs"`
	// OperatorMessages are human messages addressed to this replica.
	OperatorMessages []string `json:"operatorMessages,omitempty"`
	// Neighbors are the latest broadcast lines of ring neighbours.
	Neighbors []NeighborNote `json:"neighbors,omitempty"`
	// Outputs describes the channels this node may emit on.
	Outputs []OutputInfo `json:"outputs"`
	// Tools are the tools this node is granted (schemas, for the model).
	Tools []llm.ToolSchema `json:"tools"`
	// MaxToolCalls is the node's tool-call budget for this turn
	// (limits.maxToolCalls); the gateway enforces it, and runtimes may use it
	// to size their own loops.
	MaxToolCalls int `json:"maxToolCalls,omitempty"`
	// Model is the resolved model reference.
	Model domain.ModelRef `json:"model"`
	// ModelGuidance is the provider's guidance setting ("" or "small"):
	// small models get compact tool schemas, argument examples and one
	// tool call per round.
	ModelGuidance string `json:"modelGuidance,omitempty"`
	// PendingStop is true when the previous turn declared no further work;
	// this turn must either find work or confirm the stop.
	PendingStop bool `json:"pendingStop,omitempty"`
	// Workdir is the replica's working directory (as seen by the runtime's
	// tools; external runtimes should not touch the filesystem directly).
	Workdir string `json:"workdir"`
	Branch  string `json:"branch,omitempty"`
	// Parameters are the graph parameters.
	Parameters map[string]string `json:"parameters,omitempty"`
	// Config is Node.Config verbatim.
	Config map[string]any `json:"config,omitempty"`
	// Callback is how an external runtime reaches the gateway.
	Callback *Callback `json:"callback,omitempty"`
}

type NeighborNote struct {
	Replica int    `json:"replica"`
	Text    string `json:"text"`
	At      string `json:"at"`
}

type OutputInfo struct {
	Channel string             `json:"channel"`
	Type    domain.ChannelType `json:"type"`
	Targets []string           `json:"targets"`
	Types   []string           `json:"types,omitempty"`
	When    string             `json:"when,omitempty"`
}

// Callback carries the gateway URL and a bearer token scoped to exactly one
// execution: it authorises tool calls under this node's grants, LLM calls
// for this node's model, emits and state access - and nothing else. It
// expires when the turn ends.
type Callback struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Emit is one output message an agent wants to send.
type Emit struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	// Target narrows delivery to channels that reach this node (dynamic routing).
	Target string `json:"target,omitempty"`
	// Channel forces a specific outgoing channel.
	Channel string `json:"channel,omitempty"`
	// Metadata is merged into the message metadata (non-reserved keys only).
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Signal is how a turn tells the loop about the work, not the turn.
type Signal string

const (
	SignalNone          Signal = ""
	SignalNoFurtherWork Signal = "no_further_work" // declaration; one confirmation turn follows
	SignalConfirmStop   Signal = "confirm_stop"    // confirmation; the loop completes
)

// TurnResult is the outcome of a turn (and the external protocol response).
type TurnResult struct {
	// Emits are routed and published by the engine after the turn succeeds.
	// External runtimes may also emit during the turn via the callback; both
	// end up in the same place.
	Emits []Emit `json:"emits,omitempty"`
	// Summary is a short human-readable account of the turn.
	Summary string `json:"summary,omitempty"`
	// Broadcast is the line ring neighbours will read next turn.
	Broadcast string `json:"broadcast,omitempty"`
	Signal    Signal `json:"signal,omitempty"`
	// Usage counts runtime-local inference only; TurnEnv.Chat is charged by the engine.
	Usage llm.Usage `json:"usage"`
	// ToolCalls counts runtime-local calls only; TurnEnv.InvokeTool calls are counted by the engine.
	ToolCalls int `json:"toolCalls"`
	// Retry asks the engine to nack the inputs instead of acking them
	// (the agent could not handle them now, e.g. a dependency is down).
	Retry bool `json:"retry,omitempty"`
}

// TurnEnv is the engine's side of a turn: the only doors out of the agent.
type TurnEnv interface {
	// InvokeTool runs a tool through the gateway under this replica's grants.
	InvokeTool(ctx context.Context, name string, args json.RawMessage) (*tools.Result, error)
	// Chat calls the node's model (or another permitted model) via the LLM gateway.
	Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error)
	// Emit queues an output; it is published when the turn succeeds.
	Emit(ctx context.Context, e Emit) error
	// State is the agent's durable memory.
	StateGet(ctx context.Context, key string) ([]byte, error)
	StatePut(ctx context.Context, key string, value []byte) error
	// Output streams narration to observers (UI log, event log).
	Output(ctx context.Context, text string)
	// AskHuman blocks until an operator answers (HITL); returns the answer.
	AskHuman(ctx context.Context, question string) (string, error)
}

// ContextEnv is an optional extension of TurnEnv for runtimes that manage
// their own conversation (the native runtime). The engine implements it;
// runtimes must work without it.
type ContextEnv interface {
	// ContextWindow is the context window in tokens of the node's model
	// (0 when the provider does not say).
	ContextWindow(ctx context.Context) int
	// RecordContext records that the turn elided or compacted its
	// conversation, for the event log.
	RecordContext(ctx context.Context, ev ContextEvent)
}

// ContextEvent describes one context-management step of a turn.
type ContextEvent struct {
	// Operation is "elide" (older tool results replaced by receipts) or
	// "compact" (earlier conversation summarised by a model call).
	Operation string `json:"operation"`
	// Outcome is "ok" or "failed".
	Outcome string `json:"outcome"`
	// Messages is how many messages were elided or summarised.
	Messages     int    `json:"messages"`
	TokensBefore int    `json:"tokensBefore"`
	TokensAfter  int    `json:"tokensAfter"`
	Window       int    `json:"window"`
	Error        string `json:"error,omitempty"`
}

// PlanEnv is an optional extension of TurnEnv: the engine records an
// agent's working plan (plan.update) so people can follow it live.
type PlanEnv interface {
	RecordPlan(ctx context.Context, items []PlanItem)
}

// PlanItem is one step of an agent's working plan.
type PlanItem struct {
	Step string `json:"step"`
	// Status is "pending", "in_progress" or "done".
	Status string `json:"status"`
}
