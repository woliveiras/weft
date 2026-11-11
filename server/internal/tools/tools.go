// Package tools implements the tool registry, the permission policy and the
// gateway through which every tool invocation passes - from native agents
// and from external agents alike. The gateway is the single enforcement
// point: an agent cannot reach a tool except through it, and the gateway
// checks the caller's grants before anything executes.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

var (
	ErrDenied      = errors.New("tool: permission denied")
	ErrUnknownTool = errors.New("tool: unknown tool")
	ErrRejected    = errors.New("tool: rejected by approver")
	ErrRateLimited = errors.New("tool: rate limit exceeded")
)

// Tool is one capability an agent may invoke.
type Tool interface {
	// Name is the invocation name, e.g. "filesystem.list".
	Name() string
	// Capability is the permission it requires, e.g. "filesystem.read".
	// Several tools may share a capability.
	Capability() string
	Description() string
	// Schema is the JSON Schema of the arguments object.
	Schema() json.RawMessage
	// Invoke executes the tool. Implementations must honour ctx cancellation
	// and must confine themselves to inv.Sandbox / inv.Grant constraints.
	Invoke(ctx context.Context, inv *Invocation) (*Result, error)
}

// Principal identifies who is calling: one replica during one execution.
type Principal struct {
	Delegate func(context.Context, json.RawMessage) (*Result, error)

	GraphVersion int
	GraphID      string
	NodeID       string
	Replica      int
	ExecutionID  string
	Grants       []domain.ToolGrant
	// Checks are the declared checks check.run may run (graph snapshot).
	Checks []domain.Check
	// BaseCommit is the task's reviewed base commit, when known; check
	// evidence compares the workspace against it.
	BaseCommit string
	// Workdir is the replica's workspace root (host path). Filesystem tools
	// are jailed to it.
	Workdir           string
	WorkspaceReadOnly bool
	// Sandbox executes commands for this replica (process or container).
	Sandbox Sandbox
	// PrepareTool validates/prepares the execution environment after authorization.
	PrepareTool func(context.Context, string) error
	// BeforeMutation runs after authorization and approval, right before a
	// tool that may change the workspace (file writes and deletes, Git
	// writes, commands). The engine records a worktree checkpoint there
	// (NAT-07). It reports its own failures and cannot refuse the call.
	BeforeMutation func(context.Context, string)
	// Actor is recorded in the audit log.
	Actor string
}

// Invocation is what a Tool receives.
type Invocation struct {
	Principal *Principal
	Args      json.RawMessage
	// Grant is the (merged) grant that authorised the call; tools apply its
	// Paths/Commands/Hosts constraints.
	Grant   domain.ToolGrant
	Sandbox Sandbox
	Workdir string
	// Approver lets tools that talk to humans (human.ask) open an approval.
	// Nil when the gateway has no approver configured.
	Approver Approver
}

// Result is returned to the agent. Content is text for the model; Data is
// optional structured output for programmatic callers.
type Result struct {
	// Yield ends the current turn after persisting a durable continuation gate.
	Yield     bool            `json:"yield,omitempty"`
	Content   string          `json:"content"`
	Data      json.RawMessage `json:"data,omitempty"`
	IsError   bool            `json:"isError,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

// Sandbox runs commands on behalf of one replica. The process sandbox runs
// them as child processes confined to the workdir with a scrubbed
// environment; the docker sandbox runs them in a per-replica container with
// the workspace bind-mounted and networking disabled by default.
type Sandbox interface {
	Kind() string
	Exec(ctx context.Context, req ExecRequest) (*ExecResult, error)
	Close() error
}

type ExecRequest struct {
	Argv    []string          // argv form, preferred (no shell interpretation)
	Shell   string            // if set, run via /bin/sh -c (requires shell.exec grant)
	Dir     string            // relative to the workdir
	Env     map[string]string // added to the scrubbed base environment
	Stdin   string
	Timeout time.Duration
	// MaxOutput caps captured stdout+stderr bytes (default 64KiB).
	MaxOutput int
	// TailOutput additionally keeps the last TailOutput bytes of each
	// stream when MaxOutput is exceeded, so trailing summaries survive.
	TailOutput int
}

type ExecResult struct {
	ExitCode  int           `json:"exitCode"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	Duration  time.Duration `json:"duration"`
	Truncated bool          `json:"truncated"`
	TimedOut  bool          `json:"timedOut"`
}

// Approver lets the gateway ask a human before a call proceeds. It blocks
// until the approval is decided, expires, or ctx ends.
type Approver interface {
	RequestApproval(ctx context.Context, a *domain.Approval) (*domain.Approval, error)
}

// Auditor receives every gateway decision (wired to the event log).
type Auditor interface {
	Audit(ctx context.Context, ev *domain.Event)
}
