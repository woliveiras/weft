package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/observability"
)

// CheckToolName runs one declared check. It needs the shell.exec
// capability: a check is a named shell call under the agent's existing
// command grants, not a new privilege.
const CheckToolName = "check.run"

const (
	defaultCheckTimeout = 600 * time.Second
	// checkOutputHead and checkOutputTail bound what is captured for
	// parsing (the start and the end of each stream, where test runners
	// print failures and summaries); the model sees the parsed summary.
	checkOutputHead = 128 << 10
	checkOutputTail = 128 << 10
	// dockerExecFailed is the exit status of `docker exec` itself failing.
	dockerExecFailed = 125
)

// CheckRun is the structured result of one check.run, recorded as task
// evidence (event check.completed) and returned as Result.Data.
type CheckRun struct {
	Check      string         `json:"check"`
	Command    string         `json:"command"`
	Passed     bool           `json:"passed"`
	ExitCode   int            `json:"exitCode"`
	TimedOut   bool           `json:"timedOut,omitempty"`
	DurationMs int64          `json:"durationMs"`
	Format     string         `json:"format,omitempty"`
	Failures   []CheckFailure `json:"failures,omitempty"`
	// FailureCount is how many failures were parsed before bounding.
	FailureCount int  `json:"failureCount,omitempty"`
	Truncated    bool `json:"truncated,omitempty"`
	// InfraError marks a run the sandbox could not carry out (for example
	// the container runtime failed): neither a pass nor a test failure.
	InfraError bool `json:"infraError,omitempty"`
	// Workspace is the code state the check ran on.
	Workspace CheckWorkspace `json:"workspace"`
}

type checkTool struct{}

func checkTools() []Tool { return []Tool{checkTool{}} }

func (checkTool) capabilityOnly()    {}
func (checkTool) Name() string       { return CheckToolName }
func (checkTool) Capability() string { return capShell }
func (checkTool) Description() string {
	return "Run a declared project check (test, lint, build or custom) by name. " +
		"Returns pass/fail, the exit code and the parsed failures (file:line and message). " +
		"Prefer it over shell.exec for verification: results are recorded as task evidence."
}
func (checkTool) Schema() json.RawMessage { return checkSchema(nil) }

// SchemaFor shows the model the checks this principal can run, and hides
// the tool when none are declared.
func (t checkTool) SchemaFor(p *Principal) (llm.ToolSchema, bool) {
	if p == nil || len(p.Checks) == 0 {
		return llm.ToolSchema{}, false
	}
	names := make([]string, 0, len(p.Checks))
	for _, c := range p.Checks {
		names = append(names, c.Name)
	}
	return llm.ToolSchema{Name: t.Name(), Description: t.Description(), Parameters: checkSchema(names)}, true
}

// DescribeArgs resolves the check so approvals and the audit show the
// command that will run, not just its name.
func (checkTool) DescribeArgs(p *Principal, args json.RawMessage) json.RawMessage {
	var a struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(args, &a) != nil || p == nil {
		return nil
	}
	c, ok := findCheck(p.Checks, a.Name)
	if !ok {
		return nil
	}
	b, err := json.Marshal(map[string]any{"name": c.Name, "command": c.Command, "cwd": c.Cwd, "timeoutSec": int(checkTimeout(c) / time.Second)})
	if err != nil {
		return nil
	}
	return b
}

func checkTimeout(c domain.Check) time.Duration {
	if c.TimeoutSec > 0 {
		return time.Duration(c.TimeoutSec) * time.Second
	}
	return defaultCheckTimeout
}

func checkSchema(names []string) json.RawMessage {
	name := map[string]any{"type": "string", "description": "Check name, e.g. test"}
	if len(names) > 0 {
		name["enum"] = names
	}
	b, _ := json.Marshal(map[string]any{
		"type": "object", "required": []string{"name"}, "additionalProperties": false,
		"properties": map[string]any{"name": name},
	})
	return b
}

func (checkTool) Invoke(ctx context.Context, inv *Invocation) (res *Result, err error) {
	var a struct {
		Name string `json:"name"`
	}
	start := time.Now()
	kind, outcome := "custom", "error"
	defer func() {
		if err != nil {
			switch {
			case errors.Is(err, ErrDenied):
				outcome = "denied"
			case errors.Is(err, errUnknownCheck):
				outcome = "unknown"
			}
		}
		observability.CheckRuns.WithLabelValues(kind, outcome).Inc()
		if outcome == "passed" || outcome == "failed" || outcome == "timed_out" || outcome == "infra_error" {
			observability.CheckSeconds.WithLabelValues(kind).Observe(time.Since(start).Seconds())
		}
	}()
	if err = decodeArgs(inv, &a); err != nil {
		return nil, err
	}
	var checks []domain.Check
	if inv.Principal != nil {
		checks = inv.Principal.Checks
	}
	c, ok := findCheck(checks, a.Name)
	if !ok {
		return nil, unknownCheck(a.Name, checks)
	}
	kind = checkKind(c.Name)
	req, err := shellRequest(inv, c.Command, nil, c.Cwd)
	if err != nil {
		return nil, err
	}
	req.Timeout = checkTimeout(c)
	req.MaxOutput, req.TailOutput = checkOutputHead, checkOutputTail
	sb := sandboxOf(inv)
	// Fingerprint before running: the evidence names the tree the check
	// saw, not build output it produced.
	ws := fingerprintWorkspace(ctx, inv)
	out, err := sb.Exec(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("run check %q: %w", c.Name, err)
	}
	run := CheckRun{Check: c.Name, Command: c.Command, ExitCode: out.ExitCode, TimedOut: out.TimedOut,
		DurationMs: out.Duration.Milliseconds(), Truncated: out.Truncated, Workspace: ws}
	run.Passed = out.ExitCode == 0 && !out.TimedOut
	outcome = "passed"
	switch {
	case run.Passed:
	case sb.Kind() == "docker" && out.ExitCode == dockerExecFailed && !out.TimedOut:
		outcome, run.InfraError = "infra_error", true
		run.Failures = []CheckFailure{{Message: "the sandbox could not run the command (docker exit 125): " + clip(strings.TrimSpace(out.Stderr), maxCheckFailureMessage)}}
		run.FailureCount = 1
	default:
		outcome = "failed"
		if out.TimedOut {
			outcome = "timed_out"
		}
		run.Format, run.Failures, run.FailureCount = ParseCheckOutput(out.Stdout + "\n" + out.Stderr)
	}
	data, err := json.Marshal(run)
	if err != nil {
		return nil, fmt.Errorf("encode check result: %w", err)
	}
	// A failing check is a successful tool call: the model reads the
	// failures from the content and the evidence records the outcome.
	return &Result{Content: run.Summary(), Data: data}, nil
}

// Summary is the compact text a model reads.
func (r CheckRun) Summary() string {
	var b strings.Builder
	status := "passed"
	switch {
	case r.InfraError:
		status = "could not run"
	case !r.Passed:
		status = "failed"
	}
	fmt.Fprintf(&b, "check %q %s (exit %d, %s", r.Check, status, r.ExitCode, (time.Duration(r.DurationMs) * time.Millisecond).Round(100*time.Millisecond))
	if r.TimedOut {
		b.WriteString(", timed out")
	}
	b.WriteString(")")
	if r.Passed {
		if len(r.Workspace.CheckFilesChanged) > 0 {
			fmt.Fprintf(&b, "\nnote: test or check-definition files changed in this task: %s", strings.Join(r.Workspace.CheckFilesChanged, ", "))
		}
		return b.String()
	}
	if r.FailureCount > 0 {
		fmt.Fprintf(&b, ": %d failure(s) [%s]", r.FailureCount, r.Format)
	}
	b.WriteString("\n")
	for _, f := range r.Failures {
		b.WriteString("- ")
		if f.Test != "" {
			b.WriteString(f.Test)
			if f.File != "" {
				b.WriteString(" ")
			}
		}
		if f.File != "" {
			b.WriteString(f.File)
			if f.Line > 0 {
				fmt.Fprintf(&b, ":%d", f.Line)
			}
		}
		if f.Message != "" {
			if f.Test != "" || f.File != "" {
				b.WriteString(": ")
			}
			b.WriteString(f.Message)
		}
		b.WriteString("\n")
	}
	if r.FailureCount > len(r.Failures) {
		fmt.Fprintf(&b, "… %d more; fix these first, then rerun the check\n", r.FailureCount-len(r.Failures))
	}
	if r.Truncated {
		b.WriteString("(output was truncated; some failures may be missing)\n")
	}
	return b.String()
}

var errUnknownCheck = errors.New("unknown check")

func findCheck(checks []domain.Check, name string) (domain.Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return domain.Check{}, false
}

func unknownCheck(name string, checks []domain.Check) error {
	if len(checks) == 0 {
		return fmt.Errorf("%w %q: this task declares no checks; use shell.exec instead", errUnknownCheck, name)
	}
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return fmt.Errorf("%w %q: declared checks are %s", errUnknownCheck, name, strings.Join(names, ", "))
}

// checkKind keeps metric labels bounded: custom check names never become
// label values.
func checkKind(name string) string {
	switch name {
	case "test", "lint", "build":
		return name
	}
	return "custom"
}
