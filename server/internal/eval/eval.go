// Package eval measures the native runtime as a coding agent. Each case is a
// small repository with a request and hidden tests; a case runs through the
// real engine, native runtime, tool gateway and sandbox, and succeeds only
// when the hidden tests and the case's check pass afterwards. The suite runs
// with a scripted model in CI (plumbing) and with real local models on
// demand (the numbers every runtime change is judged by).
package eval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

//go:embed testdata
var fixtures embed.FS

// Case is one coding task.
type Case struct {
	ID      string
	Title   string
	Request string
	// Check runs in the workspace after the hidden tests are copied in;
	// exit status 0 is success.
	Check []string
}

// Cases is the suite, in a stable order.
var Cases = []Case{
	{ID: "fix-failing-test", Title: "Fix a failing test",
		Request: "The tests of this Go module fail. Find the bug in the code (not in the tests), fix it, and make sure `go test ./...` passes.",
		Check:   []string{"go", "test", "./..."}},
	{ID: "implement-function", Title: "Implement a function",
		Request: "Implement Reverse in strutil.go as its comment describes. It must handle Unicode correctly. Add a test for it and make sure `go test ./...` passes.",
		Check:   []string{"go", "test", "./..."}},
	{ID: "rename-across-files", Title: "Rename across files",
		Request: "Rename the function GetUserName to UserName everywhere in this module, including its doc comment and every caller. Make sure `go test ./...` passes.",
		Check:   []string{"sh", "-c", "go test ./... && ! grep -rn GetUserName --include=*.go ."}},
	{ID: "bug-from-issue", Title: "Bug described in an issue",
		Request: "Issue #42 from a user: \"Parse(\\\"10m\\\") returns 10 seconds instead of 10 minutes, so our timeouts are far too short.\" Fix the bug, add a regression test, and make sure `go test ./...` passes.",
		Check:   []string{"go", "test", "./..."}},
}

// Result is one case run with one model.
type Result struct {
	Case       string `json:"case"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Workflow   string `json:"workflow,omitempty"`
	Success    bool   `json:"success"`
	Execution  string `json:"execution,omitempty"`
	Status     string `json:"status"`
	ToolCalls  int    `json:"toolCalls"`
	ToolErrors int    `json:"toolErrors"`
	// ToolFailures describes the first failed or denied tool calls.
	ToolFailures []string `json:"toolFailures,omitempty"`
	// ContextOps counts elisions and compactions of the turn's conversation.
	ContextOps   int           `json:"contextOps"`
	InputTokens  int           `json:"inputTokens"`
	OutputTokens int           `json:"outputTokens"`
	Duration     time.Duration `json:"durationNs"`
	CheckOutput  string        `json:"checkOutput,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// Prompt is the coding agent prompt every case runs with; the case request
// arrives as the task message.
const Prompt = `You are a careful software engineer working in the Go module in @workspace.
Complete the task in the message you receive. Inspect the code before changing it,
keep changes minimal, and run the tests with shell.exec (argv ["go","test","./..."])
until they pass. When the task is done, call finish with a one-line summary.`

// Graph builds the graph a case runs in: one native agent with file, Git
// read and Go command grants, fed by an input channel.
func Graph(id, provider, model string, maxToolCalls int) *domain.Graph {
	return &domain.Graph{Metadata: domain.GraphMetadata{ID: id, Name: id},
		Spec: domain.GraphSpec{Workspace: domain.WorkspaceSpec{Git: true},
			Nodes: []domain.Node{{ID: "coder", Prompt: Prompt, Runtime: domain.RuntimeRef{Kind: domain.RuntimeNative},
				Model:  domain.ModelRef{Provider: provider, Name: model},
				Loop:   domain.LoopSpec{Mode: domain.LoopOnce},
				Limits: domain.NodeLimits{MaxToolCalls: maxToolCalls},
				Tools: []domain.ToolGrant{{Name: "filesystem.*"}, {Name: "git.read"},
					{Name: "shell.exec", Commands: []string{"go *", "gofmt *"}}}}},
			Channels: []domain.Channel{{ID: "task", To: []string{"coder"}}}}}
}

// copyFixture writes the case's repository into dir.
func copyFixture(c Case, part, dir string) error {
	root := "testdata/" + c.ID + "/" + part
	return fs.WalkDir(fixtures, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Fixture modules ship go.mod as go.mod.fixture: embed cannot cross
		// a module boundary.
		rel := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(p, root), "/"), ".fixture")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fixtures.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// Run runs one case with one model on a started app whose workspaces live
// under workspaces. The case's graph id is eval-<case>-<n>.
func Run(ctx context.Context, a *app.App, workspaces string, c Case, provider, model string, n, maxToolCalls int, timeout time.Duration) Result {
	return RunWorkflow(ctx, a, workspaces, Single, c, provider, model, n, maxToolCalls, timeout)
}

// RunWorkflow runs one case in the given workflow. For the factory,
// maxToolCalls applies to each agent and the totals cover every agent.
func RunWorkflow(ctx context.Context, a *app.App, workspaces string, w Workflow, c Case, provider, model string, n, maxToolCalls int, timeout time.Duration) (res Result) {
	res = Result{Case: c.ID, Provider: provider, Model: model, Workflow: string(w), Status: "not started"}
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()
	id := fmt.Sprintf("eval-%s-%d", c.ID, n)
	defer func() { _ = a.Engine.GraphAction(context.WithoutCancel(ctx), id, "kill", "eval") }()
	var dir string
	var err error
	if w == Factory {
		dir, err = runFactory(ctx, a, workspaces, id, c, provider, model, maxToolCalls, timeout, &res)
	} else {
		dir, err = runSingle(ctx, a, workspaces, id, c, provider, model, maxToolCalls, timeout, &res)
	}
	if err != nil {
		res.Error = strings.TrimSpace(strings.TrimPrefix(res.Error+"; "+err.Error(), "; "))
		if res.Status == "not started" {
			return res
		}
	}
	res.ToolErrors, res.ToolFailures = toolErrors(ctx, a.Store, id)
	res.ContextOps = contextOps(ctx, a.Store, id)
	// The verdict comes from the repository, never from the agent's claim.
	if err := copyFixture(c, "hidden", dir); err != nil {
		res.Error = strings.TrimSpace(res.Error + "; hidden tests: " + err.Error())
		return res
	}
	passed, out := check(ctx, dir, c.Check)
	res.CheckOutput = out
	// A run counts only when the checks pass and the agent finished its
	// turn: a correct workspace from a turn that never ends is not usable.
	res.Success = passed && res.Status == string(domain.ExecSucceeded)
	return res
}

func runSingle(ctx context.Context, a *app.App, workspaces, id string, c Case, provider, model string, maxToolCalls int, timeout time.Duration, res *Result) (string, error) {
	dir := filepath.Join(workspaces, id)
	if err := copyFixture(c, "repo", dir); err != nil {
		return dir, fmt.Errorf("fixture: %w", err)
	}
	if _, _, err := a.Engine.SaveGraph(ctx, Graph(id, provider, model, maxToolCalls), 0, "eval"); err != nil {
		return dir, fmt.Errorf("save graph: %w", err)
	}
	if err := a.Engine.GraphAction(ctx, id, "start", "eval"); err != nil {
		return dir, fmt.Errorf("start: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"task": c.Request})
	if _, err := a.Engine.Inject(ctx, id, "task", "task.request", payload, "", nil, "eval"); err != nil {
		return dir, fmt.Errorf("inject: %w", err)
	}
	x, err := waitExecution(ctx, a.Store, id, timeout)
	if x != nil {
		res.Execution, res.Status = x.ID, string(x.Status)
		res.ToolCalls, res.InputTokens, res.OutputTokens = x.ToolCalls, x.Usage.InputTokens, x.Usage.OutputTokens
		if x.Error != "" {
			res.Error = x.Error
		}
	}
	return dir, err
}

func waitExecution(ctx context.Context, st storage.Store, graph string, timeout time.Duration) (*domain.Execution, error) {
	deadline := time.Now().Add(timeout)
	var last *domain.Execution
	for time.Now().Before(deadline) {
		xs, err := st.ListExecutions(ctx, storage.ExecutionFilter{GraphID: graph})
		if err == nil && len(xs) > 0 {
			last = xs[0]
			if last.CompletedAt != nil {
				return last, nil
			}
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return last, fmt.Errorf("the turn did not finish within %s", timeout)
}

// contextOps counts the turn's context-management events (0 when the
// event log cannot be read; the report is diagnostic, not a verdict).
func contextOps(ctx context.Context, st storage.Store, graph string) int {
	evs, err := st.ListEvents(ctx, storage.EventFilter{GraphID: graph, Types: []domain.EventType{domain.EvAgentContext}, Limit: 5000})
	if err != nil {
		return 0
	}
	return len(evs)
}

func toolErrors(ctx context.Context, st storage.Store, graph string) (int, []string) {
	evs, err := st.ListEvents(ctx, storage.EventFilter{GraphID: graph, Types: []domain.EventType{domain.EvToolCompleted, domain.EvToolDenied}, Limit: 5000, Ascending: true})
	if err != nil {
		return 0, nil
	}
	n, failures := 0, []string{}
	for _, ev := range evs {
		var d struct{ Tool, Outcome, Error string }
		_ = json.Unmarshal(ev.Data, &d)
		if d.Outcome == "ok" || d.Outcome == "started" {
			continue
		}
		n++
		if len(failures) < 10 {
			f := d.Tool + " " + d.Outcome
			if d.Error != "" {
				f += ": " + d.Error
			}
			if len(f) > 200 {
				f = f[:200]
			}
			failures = append(failures, f)
		}
	}
	return n, failures
}

func check(ctx context.Context, dir string, argv []string) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if len(text) > 4000 {
		text = text[len(text)-4000:]
	}
	return err == nil, text
}

// Summary aggregates results per model.
type Summary struct {
	Provider, Model           string
	Cases, Solved             int
	ToolCalls, ToolErrors     int
	InputTokens, OutputTokens int
	Duration                  time.Duration
}

// Summarize groups results by provider and model.
func Summarize(rs []Result) []Summary {
	by := map[string]*Summary{}
	for _, r := range rs {
		k := r.Provider + "/" + r.Model
		s := by[k]
		if s == nil {
			s = &Summary{Provider: r.Provider, Model: r.Model}
			by[k] = s
		}
		s.Cases++
		if r.Success {
			s.Solved++
		}
		s.ToolCalls += r.ToolCalls
		s.ToolErrors += r.ToolErrors
		s.InputTokens += r.InputTokens
		s.OutputTokens += r.OutputTokens
		s.Duration += r.Duration
	}
	out := make([]Summary, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider+out[i].Model < out[j].Provider+out[j].Model })
	return out
}

// Markdown renders results as tables for the design page.
func Markdown(rs []Result) string {
	var b strings.Builder
	b.WriteString("| Provider | Model | Solved | Tool calls | Tool errors | Tokens in / out | Time |\n|---|---|---|---|---|---|---|\n")
	for _, s := range Summarize(rs) {
		fmt.Fprintf(&b, "| %s | %s | %d/%d | %d | %d | %d / %d | %s |\n", s.Provider, s.Model, s.Solved, s.Cases, s.ToolCalls, s.ToolErrors, s.InputTokens, s.OutputTokens, s.Duration.Round(time.Second))
	}
	b.WriteString("\n| Provider | Model | Case | Result | Tool calls | Tool errors | Context ops | Tokens in / out | Time | Status |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rs {
		verdict := "fail"
		if r.Success {
			verdict = "pass"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %d | %d | %d / %d | %s | %s |\n", r.Provider, r.Model, r.Case, verdict, r.ToolCalls, r.ToolErrors, r.ContextOps, r.InputTokens, r.OutputTokens, r.Duration.Round(time.Second), r.Status)
	}
	return b.String()
}
