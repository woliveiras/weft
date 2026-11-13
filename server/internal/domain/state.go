package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Graph run state machine
//
//	         start            pause            resume
//	stopped ───────▶ running ───────▶ paused ───────▶ running
//	   ▲                │  ▲             │
//	   │        stop    │  └─────────────┘ (resume)
//	   │                ▼
//	   └──────────── stopping ◀──────────── paused (stop)
//	   ▲                │ (all replicas drained)
//	   │                ▼
//	   └─────────── stopped          any ──(fatal)──▶ failed ──start──▶ running
//
// The desired state is persisted; the actual state is derived from replicas.
// After a restart the supervisor drives actual toward desired.
// ---------------------------------------------------------------------------

type GraphState string

const (
	GraphStopped  GraphState = "stopped"
	GraphRunning  GraphState = "running"
	GraphPaused   GraphState = "paused"
	GraphStopping GraphState = "stopping"
	GraphFailed   GraphState = "failed"
)

var graphTransitions = map[GraphState][]GraphState{
	GraphStopped:  {GraphRunning},
	GraphRunning:  {GraphPaused, GraphStopping, GraphStopped, GraphFailed},
	GraphPaused:   {GraphRunning, GraphStopping, GraphStopped, GraphFailed},
	GraphStopping: {GraphStopped, GraphFailed},
	GraphFailed:   {GraphRunning, GraphStopped},
}

func (s GraphState) CanTransition(to GraphState) bool { return allowed(graphTransitions, s, to) }

// ---------------------------------------------------------------------------
// Replica (agent instance) state machine
//
//	stopped ─start─▶ starting ─▶ idle ◀──────────────┐
//	                               │ work/tick        │ turn done
//	                               ▼                  │
//	                            running ──────────────┘
//	                               │  ▲
//	              approval needed  ▼  │ decided
//	                         awaiting_approval
//	idle/running ─pause─▶ paused ─resume─▶ idle
//	any ─stop─▶ stopping ─(turn drained)─▶ stopped
//	running ─stop policy met─▶ completed       (stopped itself; shown distinctly)
//	running ─unrecoverable─▶ failed ─(backoff)─▶ starting
// ---------------------------------------------------------------------------

type ReplicaState string

const (
	ReplicaStopped          ReplicaState = "stopped"
	ReplicaStarting         ReplicaState = "starting"
	ReplicaIdle             ReplicaState = "idle"
	ReplicaRunning          ReplicaState = "running"
	ReplicaAwaitingApproval ReplicaState = "awaiting_approval"
	ReplicaPaused           ReplicaState = "paused"
	ReplicaStopping         ReplicaState = "stopping"
	ReplicaCompleted        ReplicaState = "completed"
	ReplicaFailed           ReplicaState = "failed"
)

var replicaTransitions = map[ReplicaState][]ReplicaState{
	ReplicaStopped:          {ReplicaStarting},
	ReplicaStarting:         {ReplicaIdle, ReplicaRunning, ReplicaFailed, ReplicaStopping, ReplicaStopped, ReplicaPaused},
	ReplicaIdle:             {ReplicaRunning, ReplicaPaused, ReplicaStopping, ReplicaStopped, ReplicaCompleted, ReplicaAwaitingApproval},
	ReplicaRunning:          {ReplicaIdle, ReplicaAwaitingApproval, ReplicaPaused, ReplicaStopping, ReplicaCompleted, ReplicaFailed, ReplicaStopped},
	ReplicaAwaitingApproval: {ReplicaRunning, ReplicaIdle, ReplicaStopping, ReplicaFailed, ReplicaPaused},
	ReplicaPaused:           {ReplicaIdle, ReplicaRunning, ReplicaStopping, ReplicaStopped},
	ReplicaStopping:         {ReplicaStopped, ReplicaFailed},
	ReplicaCompleted:        {ReplicaStarting, ReplicaStopped},
	ReplicaFailed:           {ReplicaStarting, ReplicaStopped},
}

func (s ReplicaState) CanTransition(to ReplicaState) bool {
	return s == to || allowed(replicaTransitions, s, to)
}

// Live reports whether the replica holds resources and can be told to stop.
func (s ReplicaState) Live() bool {
	switch s {
	case ReplicaStopped, ReplicaCompleted, ReplicaFailed:
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Execution (one turn) state machine
//
//	pending ─▶ running ─▶ succeeded
//	              │  ├──▶ failed      (inputs nacked, redelivered with backoff)
//	              │  ├──▶ cancelled   (operator kill / shutdown; inputs nacked)
//	              │  └──▶ interrupted (process died; detected on recovery)
//	              ▼
//	      awaiting_approval ─▶ running
// ---------------------------------------------------------------------------

type ExecutionStatus string

const (
	ExecPending          ExecutionStatus = "pending"
	ExecRunning          ExecutionStatus = "running"
	ExecAwaitingApproval ExecutionStatus = "awaiting_approval"
	ExecSucceeded        ExecutionStatus = "succeeded"
	ExecFailed           ExecutionStatus = "failed"
	ExecCancelled        ExecutionStatus = "cancelled"
	ExecInterrupted      ExecutionStatus = "interrupted"
)

var execTransitions = map[ExecutionStatus][]ExecutionStatus{
	ExecPending:          {ExecRunning, ExecCancelled, ExecAwaitingApproval},
	ExecRunning:          {ExecSucceeded, ExecFailed, ExecCancelled, ExecInterrupted, ExecAwaitingApproval},
	ExecAwaitingApproval: {ExecRunning, ExecFailed, ExecCancelled, ExecInterrupted},
}

func (s ExecutionStatus) CanTransition(to ExecutionStatus) bool {
	return allowed(execTransitions, s, to)
}

func (s ExecutionStatus) Terminal() bool {
	switch s {
	case ExecSucceeded, ExecFailed, ExecCancelled, ExecInterrupted:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Message delivery lifecycle (as recorded in message history)
//
//	published ─▶ delivered ─▶ acked
//	    │            └──────▶ nacked ─▶ delivered (retry) ... ─▶ dead_lettered
//	    └─▶ held (approval gate) ─▶ published | rejected
// ---------------------------------------------------------------------------

type MessageStatus string

const (
	MsgPublished    MessageStatus = "published"
	MsgHeld         MessageStatus = "held"
	MsgRejected     MessageStatus = "rejected"
	MsgDelivered    MessageStatus = "delivered"
	MsgAcked        MessageStatus = "acked"
	MsgNacked       MessageStatus = "nacked"
	MsgDeadLettered MessageStatus = "dead_lettered"
	MsgDropped      MessageStatus = "dropped" // no route matched / hop limit
)

func allowed[S comparable](m map[S][]S, from, to S) bool {
	for _, s := range m[from] {
		if s == to {
			return true
		}
	}
	return false
}

// TransitionError is returned when a state change is not allowed.
type TransitionError struct {
	Entity   string
	From, To string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s: invalid transition %s -> %s", e.Entity, e.From, e.To)
}

// ---------------------------------------------------------------------------
// Records persisted by the store.
// ---------------------------------------------------------------------------

// GraphRecord is the persisted control state of a graph.
type GraphRecord struct {
	ID             string     `json:"id"`
	Version        int        `json:"version"`
	DesiredState   GraphState `json:"desiredState"`
	State          GraphState `json:"state"`
	RunID          string     `json:"runId,omitempty"`
	BudgetResumeID string     `json:"budgetResumeId,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// Execution is one turn of one replica.
// DelegationAudit records enforced access and the approval outcome, independently
// of mutable installation settings. Nil on historical executions means unknown.
type DelegationAudit struct {
	Runtime         string      `json:"runtime"`
	EffectiveGrants []ToolGrant `json:"effectiveGrants"`
	WorkspaceAccess bool        `json:"workspaceAccess"`
	RecursiveAccess bool        `json:"recursiveAccess"`
	MaxOutputTokens int         `json:"maxOutputTokens"`
	Decision        string      `json:"decision"`
	DecidedBy       string      `json:"decidedBy,omitempty"`
	DecidedAt       *time.Time  `json:"decidedAt,omitempty"`
}

type Execution struct {
	DelegationAudit     *DelegationAudit `json:"delegationAudit,omitempty"`
	ParentExecutionID   string           `json:"parentExecutionId,omitempty"`
	DelegationTemplate  string           `json:"delegationTemplate,omitempty"`
	DelegationConfig    json.RawMessage  `json:"delegationConfig,omitempty"`
	DelegationObjective string           `json:"delegationObjective,omitempty"`
	DelegationModel     *ModelRef        `json:"delegationModel,omitempty"`
	ApprovalID          string           `json:"approvalId,omitempty"`

	ID          string          `json:"id"`
	GraphID     string          `json:"graphId"`
	GraphVer    int             `json:"graphVersion"`
	RunID       string          `json:"runId"`
	NodeID      string          `json:"nodeId"`
	Replica     int             `json:"replica"`
	Iteration   int             `json:"iteration"`
	Status      ExecutionStatus `json:"status"`
	InputIDs    []string        `json:"inputIds,omitempty"`
	OutputIDs   []string        `json:"outputIds,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Error       string          `json:"error,omitempty"`
	Signal      string          `json:"signal,omitempty"`
	Usage       Usage           `json:"usage"`
	ToolCalls   int             `json:"toolCalls"`
	TraceID     string          `json:"traceId,omitempty"`
	Instance    string          `json:"instance,omitempty"`
	StartedAt   time.Time       `json:"startedAt"`
	CompletedAt *time.Time      `json:"completedAt,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	// Cache tokens are the parts of InputTokens read from or written to the
	// provider's prompt cache, when it reports them.
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
}

// TaskRunGraphPrefix starts the id of every task run's execution graph.
const TaskRunGraphPrefix = "task-run-"

// Labels a task run's execution graph carries for attribution.
const (
	LabelTask         = "weft.task"
	LabelTaskWorkflow = "weft.workflow"
	LabelTaskUser     = "weft.user"
	LabelTaskProject  = "weft.project"
	LabelTaskSCM      = "weft.scm"
	// LabelTaskBase is the reviewed base commit of a project task; check
	// evidence lists check-defining files changed since it.
	LabelTaskBase = "weft.base"
)

func (u *Usage) Add(o Usage) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
}

// MessageRecord is message history: one row per (message, channel, group).
type MessageRecord struct {
	Message   Message       `json:"message"`
	GraphID   string        `json:"graphId"`
	Channel   string        `json:"channel"`
	Group     string        `json:"group,omitempty"`
	Status    MessageStatus `json:"status"`
	Attempt   int           `json:"attempt"`
	Consumer  string        `json:"consumer,omitempty"`
	Error     string        `json:"error,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// ApprovalKind says what a human is being asked to approve.
type ApprovalKind string

const (
	ApprovalDelegation  ApprovalKind = "delegation"
	ApprovalEnvironment ApprovalKind = "environment" // proposed environment requiring developer approval
	ApprovalTool        ApprovalKind = "tool"        // a tool call requiring approval
	ApprovalMessage     ApprovalKind = "message"     // a message held at a gate
	ApprovalTurn        ApprovalKind = "turn"        // a node's turn requiring approval
	ApprovalQuestion    ApprovalKind = "question"    // an agent asked a human (human.ask)
)

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalExpired  ApprovalStatus = "expired"
)

type Approval struct {
	ID          string          `json:"id"`
	GraphID     string          `json:"graphId"`
	NodeID      string          `json:"nodeId,omitempty"`
	Replica     int             `json:"replica"`
	ExecutionID string          `json:"executionId,omitempty"`
	Kind        ApprovalKind    `json:"kind"`
	Status      ApprovalStatus  `json:"status"`
	Title       string          `json:"title"`
	Detail      json.RawMessage `json:"detail,omitempty"`
	// Answer carries a human's free-text response (question approvals) or an
	// edited payload (message approvals).
	Answer    string     `json:"answer,omitempty"`
	DecidedBy string     `json:"decidedBy,omitempty"`
	Comment   string     `json:"comment,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
}

// OperatorMessage is a human message to a specific replica (or any replica
// of a node when Replica < 0). It wakes a waiting loop.
type OperatorMessage struct {
	ID        string     `json:"id"`
	GraphID   string     `json:"graphId"`
	NodeID    string     `json:"nodeId"`
	Replica   int        `json:"replica"`
	Text      string     `json:"text"`
	Author    string     `json:"author"`
	CreatedAt time.Time  `json:"createdAt"`
	Consumed  *time.Time `json:"consumedAt,omitempty"`
}

// ReplicaStatus is the live status of one replica, published to the UI.
type ReplicaStatus struct {
	GraphID      string       `json:"graphId"`
	NodeID       string       `json:"nodeId"`
	Replica      int          `json:"replica"`
	State        ReplicaState `json:"state"`
	Iteration    int          `json:"iteration"`
	RunIteration int          `json:"runIteration"`
	Execution    string       `json:"executionId,omitempty"`
	Instance     string       `json:"instance,omitempty"`
	Detail       string       `json:"detail,omitempty"`
	Workdir      string       `json:"workdir,omitempty"`
	Branch       string       `json:"branch,omitempty"`
	Deadline     *time.Time   `json:"deadline,omitempty"`
	UpdatedAt    time.Time    `json:"updatedAt"`
}

// Lease grants one platform instance the right to run one replica. It is
// how several control-plane instances share a fleet without double-running.
type Lease struct {
	Key       string    `json:"key"`
	Holder    string    `json:"holder"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ReplicaKey is the canonical key of a replica: graph/node#n.
func ReplicaKey(graph, node string, replica int) string {
	return fmt.Sprintf("%s/%s#%d", graph, node, replica)
}

// DeferredEnvironmentCleanup is the KV namespace for graph cleanup after forced deletion.
const DeferredEnvironmentCleanup = "environment-deferred-cleanup-v1"
