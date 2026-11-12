package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
)

// DefaultMaxResultBytes caps the content handed back to a model.
const DefaultMaxResultBytes = 32 << 10

const maxAuditArgBytes = 2 << 10

// Gateway is the single door through which agents reach tools. Every call
// is looked up, authorised against the caller's grants, rate limited,
// optionally held for human approval, executed, audited, measured and traced.
type Gateway struct {
	Registry          *Registry
	TaskAuthorization func(context.Context, *Principal, Tool, bool) (bool, string, error)
	// ResolveTool resolves dynamic tools; errors fail closed. Configured at startup.
	ResolveTool func(context.Context, string) (Tool, error)
	Approver    Approver
	Auditor     Auditor
	// MaxResultBytes caps Result.Content (default 32 KiB).
	MaxResultBytes int
	// Now is overridable for tests.
	Now func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// NewGateway builds a gateway. approver and auditor may be nil: without an
// approver, calls that require approval are denied; without an auditor,
// nothing is recorded beyond metrics and traces.
func NewGateway(reg *Registry, approver Approver, auditor Auditor) *Gateway {
	return &Gateway{Registry: reg, Approver: approver, Auditor: auditor, MaxResultBytes: DefaultMaxResultBytes, buckets: map[string]*bucket{}}
}

func (g *Gateway) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Invoke runs tool name for principal p.
//
// Go errors are returned only for refusals - ErrUnknownTool, ErrDenied,
// ErrRejected, ErrRateLimited (wrapped) - and for invalid arguments that
// are not a JSON object. A tool that fails while executing produces a
// Result with IsError set, so the model can read the failure and recover.
func (g *Gateway) Invoke(ctx context.Context, p *Principal, name string, args json.RawMessage) (*Result, error) {
	return g.invoke(ctx, p, name, args, "", nil)
}

// ReadGuidanceReference runs a package-scoped, bounded reader behind the
// existing filesystem.read grants, path constraints, rate and approval gates.
// The caller confines the target to the workdir and supplies its relative path.
func (g *Gateway) ReadGuidanceReference(ctx context.Context, p *Principal, path string, read func() string) (*Result, error) {
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return nil, err
	}
	return g.invoke(ctx, p, "filesystem.read", args, path, read)
}

func (g *Gateway) invoke(ctx context.Context, p *Principal, name string, args json.RawMessage, referencePath string, read func() string) (res *Result, err error) {
	if p == nil {
		return nil, fmt.Errorf("%w: no principal", ErrDenied)
	}
	telemetryName := name
	if read != nil {
		telemetryName = "guidance.reference"
	}
	ctx, span := observability.Start(ctx, "tool."+telemetryName,
		attribute.String("weft.tool", telemetryName),
		attribute.String("weft.graph", p.GraphID),
		attribute.String("weft.node", p.NodeID),
		attribute.Int("weft.replica", p.Replica),
		attribute.String("weft.execution", p.ExecutionID),
	)
	metricName := telemetryName
	if strings.HasPrefix(name, "mcp.") {
		metricName = "mcp"
	}
	start := g.now()
	outcome := "ok"
	defer func() {
		observability.ToolInvocations.WithLabelValues(metricName, outcome).Inc()
		observability.ToolDuration.WithLabelValues(metricName).Observe(time.Since(start).Seconds())
		span.SetAttributes(attribute.String("weft.tool.outcome", outcome))
		observability.EndSpan(span, err)
	}()

	t, ok := g.Registry.Get(name)
	if !ok && g.ResolveTool != nil {
		t, err = g.ResolveTool(ctx, name)
		if err != nil {
			outcome = "denied"
			g.audit(ctx, p, domain.EvToolDenied, name, nil, outcome, 0, err)
			return nil, err
		}
		ok = t != nil
	}
	if !ok {
		outcome = "unknown"
		err = fmt.Errorf("%w: %s", ErrUnknownTool, name)
		g.audit(ctx, p, domain.EvToolDenied, name, args, outcome, 0, err)
		return nil, err
	}
	// shown is what approvals and the audit record: the arguments, or what
	// the call resolves to (check.run shows its declared command).
	shown := args
	if d, ok := t.(argsDescriber); ok {
		if b := d.DescribeArgs(p, args); len(b) > 0 {
			shown = b
		}
	}
	grant, aerr := Authorize(p, t)
	if aerr != nil {
		outcome = "denied"
		g.audit(ctx, p, domain.EvToolDenied, name, shown, outcome, 0, aerr)
		return nil, aerr
	}
	if read != nil {
		if err := CheckPath(grant, referencePath); err != nil {
			outcome = "denied"
			g.audit(ctx, p, domain.EvToolDenied, name, shown, outcome, 0, err)
			return nil, err
		}
	}
	if p.WorkspaceReadOnly && (workspaceMutation(t) || t.Capability() == "git.read") {
		outcome = "denied"
		err = fmt.Errorf("%w: workspace is read-only", ErrDenied)
		g.audit(ctx, p, domain.EvToolDenied, name, nil, outcome, 0, err)
		return nil, err
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(args, &obj) != nil || obj == nil {
		outcome = "error"
		err = fmt.Errorf("tool %s: arguments must be a JSON object", name)
		return nil, err
	}
	// Convert values sent in the wrong JSON type before approval and audit,
	// so people approve and the log records what actually runs.
	coerced, coercions := CoerceArgs(t.Schema(), args)
	if len(coercions) > 0 {
		args, shown = coerced, coerced
		if d, ok := t.(argsDescriber); ok {
			if b := d.DescribeArgs(p, args); len(b) > 0 {
				shown = b
			}
		}
		observability.ToolArgumentCoercions.WithLabelValues(name).Inc()
	}
	if grant.RateLimit > 0 && !g.allow(p.GraphID+"/"+p.NodeID+"/"+rateKey(t), grant.RateLimit) {
		outcome = "rate_limited"
		err = fmt.Errorf("%w: %s allows %d calls per minute", ErrRateLimited, name, grant.RateLimit)
		g.audit(ctx, p, domain.EvToolDenied, name, shown, outcome, 0, err)
		return nil, err
	}
	required, consent := grant.RequiresApproval, ""
	if g.TaskAuthorization != nil {
		var e error
		required, consent, e = g.TaskAuthorization(ctx, p, t, required)
		if e != nil {
			outcome = "denied"
			err = taskConsentDenied(e, "task consent unavailable or stale")
			g.audit(ctx, p, domain.EvToolDenied, name, nil, outcome, 0, err)
			return nil, err
		}
	}
	if required {
		if rerr := g.approve(ctx, p, name, shown); rerr != nil {
			outcome = "rejected"
			if errors.Is(rerr, ErrDenied) {
				outcome = "denied"
			}
			g.audit(ctx, p, domain.EvToolDenied, name, shown, outcome, 0, rerr)
			return nil, rerr
		}
	}

	if p.PrepareTool != nil {
		if prepareErr := p.PrepareTool(ctx, name); prepareErr != nil {
			outcome = "error"
			g.audit(ctx, p, domain.EvToolCompleted, name, shown, outcome, time.Since(start), prepareErr)
			result := &Result{Content: prepareErr.Error(), IsError: true}
			g.cap(result)
			return result, nil
		}
	}
	if g.TaskAuthorization != nil {
		_, current, e := g.TaskAuthorization(ctx, p, t, grant.RequiresApproval)
		if e != nil || current != consent {
			outcome = "denied"
			err = taskConsentDenied(e, "task consent changed while waiting; retry under current permissions")
			g.audit(ctx, p, domain.EvToolDenied, name, nil, outcome, 0, err)
			return nil, err
		}
		if consent != "" {
			g.audit(ctx, p, "task.authorization_decision", name, nil, consent, 0, nil)
		}
	}
	if p.BeforeMutation != nil && checkpointedMutation(t) {
		p.BeforeMutation(ctx, name)
	}
	g.audit(ctx, p, domain.EvToolInvoked, name, shown, "started", 0, nil)
	inv := &Invocation{Principal: p, Args: args, Grant: grant, Workdir: p.Workdir, Sandbox: p.Sandbox, Approver: g.Approver}
	if inv.Sandbox == nil {
		inv.Sandbox = &ProcessSandbox{Root: p.Workdir}
	}
	// Observe changes during this invocation; concurrent writers are not excluded.
	if observesWorkspace(t) && p.Workdir != "" && g.Auditor != nil {
		before := snapshotWorkspace(ctx, p.Workdir)
		defer func() {
			g.auditWorkspace(context.WithoutCancel(ctx), p, name, before, snapshotWorkspace(context.WithoutCancel(ctx), p.Workdir))
		}()
	}
	var terr error
	if read != nil {
		res = &Result{Content: "guidance reference read"}
		if read() != "" {
			res = &Result{Content: "guidance reference could not be read", IsError: true}
		}
	} else {
		res, terr = g.safeInvoke(ctx, t, inv)
	}
	if terr != nil {
		if errors.Is(terr, ErrDenied) || errors.Is(terr, ErrRateLimited) {
			// Constraint violation detected inside the tool (paths, commands, hosts).
			outcome = "denied"
			if errors.Is(terr, ErrRateLimited) {
				outcome = "rate_limited"
			}
			g.audit(ctx, p, domain.EvToolDenied, name, shown, outcome, time.Since(start), terr)
			return nil, terr
		}
		outcome = "error"
		res = &Result{Content: terr.Error(), IsError: true}
	} else if res == nil {
		res = &Result{}
	} else if res.IsError {
		outcome = "error"
	}
	if len(coercions) > 0 {
		res.Content += "\n(note: Weft converted argument types: " + strings.Join(coercions, "; ") + ". Send these JSON types next time.)"
	}
	g.cap(res)
	g.audit(ctx, p, domain.EvToolCompleted, name, shown, outcome, time.Since(start), terr)
	if name == CheckToolName && terr == nil {
		g.auditCheck(ctx, p, res)
	}
	return res, nil
}

// auditCheck records a check.run result as task evidence. Only runs that
// executed produce one: a denied or unknown check is not evidence.
func (g *Gateway) auditCheck(ctx context.Context, p *Principal, res *Result) {
	if g.Auditor == nil || len(res.Data) == 0 {
		return
	}
	var run CheckRun
	if err := json.Unmarshal(res.Data, &run); err != nil || run.Check == "" {
		observability.CheckEvidenceDropped.Inc()
		slog.WarnContext(ctx, "check result not recorded as evidence", "graph", p.GraphID, "node", p.NodeID, "execution", p.ExecutionID, "err", err)
		return
	}
	ev := domain.NewEvent(domain.EvCheckCompleted, p.GraphID, run).
		WithNode(p.NodeID, p.Replica).
		WithExecution(p.ExecutionID).
		WithTrace(observability.TraceID(ctx))
	if p.Actor != "" {
		ev.WithActor(p.Actor)
	} else if p.NodeID != "" {
		ev.WithActor(domain.ReplicaKey(p.GraphID, p.NodeID, p.Replica))
	}
	g.Auditor.Audit(ctx, ev)
}

// taskConsentDenied forwards the actionable isolation refusal and keeps
// other authorization failures generic at the tool boundary.
func taskConsentDenied(cause error, fallback string) error {
	if errors.Is(cause, ErrWorkspaceShellIsolation) {
		return fmt.Errorf("%w: %w", ErrDenied, ErrWorkspaceShellIsolation)
	}
	return fmt.Errorf("%w: %s", ErrDenied, fallback)
}

// rateKey shares one allowance between a capability-only tool and its
// capability (check.run counts against shell.exec's rate limit).
func rateKey(t Tool) string {
	if _, ok := t.(capabilityOnly); ok {
		return t.Capability()
	}
	return t.Name()
}

// safeInvoke converts a panicking tool into an error result.
func (g *Gateway) safeInvoke(ctx context.Context, t Tool, inv *Invocation) (res *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("tool %s panicked: %v", t.Name(), r)
		}
	}()
	return t.Invoke(ctx, inv)
}

func (g *Gateway) cap(res *Result) {
	max := g.MaxResultBytes
	if max <= 0 {
		max = DefaultMaxResultBytes
	}
	if len(res.Content) > max {
		res.Content = truncateUTF8(res.Content, max) + "\n…[truncated]"
		res.Truncated = true
	}
}

type noApprovalKey struct{}

// WithoutApproval marks ctx as non-interactive: a call that would need a
// human approval (from its grant or the task consent) is denied instead of
// waiting for one. Prompt construction uses it so building a system prompt
// never blocks on a person.
func WithoutApproval(ctx context.Context) context.Context {
	return context.WithValue(ctx, noApprovalKey{}, true)
}

func (g *Gateway) approve(ctx context.Context, p *Principal, name string, args json.RawMessage) error {
	if v, _ := ctx.Value(noApprovalKey{}).(bool); v {
		return fmt.Errorf("%w: %s requires approval, which a non-interactive call cannot request", ErrDenied, name)
	}
	if g.Approver == nil {
		return fmt.Errorf("%w: %s requires approval but no approver is configured", ErrDenied, name)
	}
	detail, _ := json.Marshal(map[string]any{"tool": name, "args": args})
	who := p.NodeID
	if who == "" {
		who = "agent"
	}
	a := &domain.Approval{
		ID:          domain.NewID("apr"),
		GraphID:     p.GraphID,
		NodeID:      p.NodeID,
		Replica:     p.Replica,
		ExecutionID: p.ExecutionID,
		Kind:        domain.ApprovalTool,
		Status:      domain.ApprovalPending,
		Title:       fmt.Sprintf("%s wants to run %s", who, name),
		Detail:      detail,
		CreatedAt:   g.now().UTC(),
	}
	decided, err := g.Approver.RequestApproval(ctx, a)
	if err != nil {
		return fmt.Errorf("%w: approval for %s failed: %v", ErrRejected, name, err)
	}
	if decided == nil || decided.Status != domain.ApprovalApproved {
		status := "no decision"
		reason := ""
		if decided != nil {
			status = string(decided.Status)
			if decided.Comment != "" {
				reason = ": " + decided.Comment
			}
		}
		return fmt.Errorf("%w: %s was %s%s", ErrRejected, name, status, reason)
	}
	return nil
}

func (g *Gateway) audit(ctx context.Context, p *Principal, typ domain.EventType, name string, args json.RawMessage, outcome string, d time.Duration, err error) {
	if g.Auditor == nil {
		return
	}
	if strings.HasPrefix(name, "mcp.") || name == "delegation.request" {
		args = nil
	}
	data := map[string]any{"tool": name, "outcome": outcome, "durationMs": d.Milliseconds()}
	if len(args) > maxAuditArgBytes {
		data["args"] = truncateUTF8(string(args), maxAuditArgBytes) + "…"
	} else if len(args) > 0 {
		data["args"] = json.RawMessage(args)
	}
	if err != nil {
		data["error"] = err.Error()
		if strings.HasPrefix(name, "mcp.") {
			data["error"] = "MCP invocation " + outcome
		}
	}
	ev := domain.NewEvent(typ, p.GraphID, data).
		WithNode(p.NodeID, p.Replica).
		WithExecution(p.ExecutionID).
		WithTrace(observability.TraceID(ctx))
	if p.Actor != "" {
		ev.WithActor(p.Actor)
	} else if p.NodeID != "" {
		ev.WithActor(domain.ReplicaKey(p.GraphID, p.NodeID, p.Replica))
	}
	g.Auditor.Audit(ctx, ev)
}

// bucket is a token bucket refilling perMinute tokens per minute, burst perMinute.
type bucket struct {
	tokens float64
	last   time.Time
}

func (g *Gateway) allow(key string, perMinute int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.buckets == nil {
		g.buckets = map[string]*bucket{}
	}
	now := g.now()
	b, ok := g.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(perMinute), last: now}
		g.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Minutes() * float64(perMinute)
	if b.tokens > float64(perMinute) {
		b.tokens = float64(perMinute)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n]
}
