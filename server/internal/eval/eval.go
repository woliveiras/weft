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
	"strconv"
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
		Check:   []string{"sh", "-c", "go test ./... && ! grep -rln GetUserName . | grep -q '\\.go$'"}},
	{ID: "bug-from-issue", Title: "Bug described in an issue",
		Request: "Issue #42 from a user: \"Parse(\\\"10m\\\") returns 10 seconds instead of 10 minutes, so our timeouts are far too short.\" Fix the bug, add a regression test, and make sure `go test ./...` passes.",
		Check:   []string{"go", "test", "./..."}},
	// The cases below are harder: a strong model should solve them only
	// sometimes, so models and workflows separate again.
	{ID: "discount-across-packages", Title: "Feature spanning several packages",
		Request: "Add per-order discounts to this shop module. `order.Order` gets a `DiscountPercent int` field: a whole percentage from 0 to 100, and `Validate` must reject any other value. `Total` subtracts the discount from the subtotal; the discount amount is rounded down to whole cents. The invoice (`invoice.Render`) shows a `Discount: <n>%` line between the `Subtotal:` and `Total:` lines, only when the discount is not zero. The JSON API (`api.Encode` and `api.Decode`) carries it as `discount_percent`. Add tests and make sure `go test ./...` passes.",
		Check:   []string{"go", "test", "./..."}},
	{ID: "stale-cache", Title: "Bug found only by an integration test",
		Request: "Bug report from support: \"Users who change their theme, or reset it to the default, keep seeing their old theme until the server restarts. It does not happen the first time a user picks a theme.\" Find the cause and fix it. The cache exists so that repeated reads do not reach the store; keep it doing that. Add a regression test and make sure `go test ./...` passes.",
		Check:   []string{"go", "test", "./..."}},
	{ID: "misleading-issue", Title: "Issue with a misleading suggestion",
		Request: "Issue #118 from a user: \"`Latest` offers v1.9.0 as our newest release although v1.10.0 is out. I think `Latest` should compare the tags by length first and then alphabetically.\" A maintainer added: \"Our tags look like `v1.2.3`; older ones look like `1.4`, without the `v` or the patch number. A missing number counts as 0, so `1.4` and `v1.4.0` are the same version.\" Fix the root cause, add a regression test, and make sure `go test ./...` passes.",
		Check:   []string{"go", "test", "./..."}},
	{ID: "printf-logger-vet", Title: "Refactor that go vet and gofmt judge",
		Request: "Refactor the logger in package logx to printf style: replace `Log(args ...any)` with `Logf(format string, args ...any)`, which writes the prefix, a space, the formatted message and a newline. Convert every caller so the log output stays exactly the same, and remove `Log`. The code must stay gofmt-formatted and `go vet ./...` must pass, as well as `go test ./...`.",
		Check:   []string{"sh", "-c", "test -z \"$(gofmt -l .)\" && go vet ./... && go test ./... && ! grep -nF -e ') Log(' -e '.Log(' $(find . -name '*.go' ! -name '*_test.go')"}},
	{ID: "truncate-shared-helper", Title: "Obvious fix breaks another caller",
		Request: "Bug report: \"Note titles in Japanese or with emoji end with a broken character (\uFFFD) in the notes list.\" The notes list shows at most `preview.MaxTitle` characters of a title. Fix the bug. `text.Truncate` has other callers: keep them working. Add a regression test and make sure `go test ./...` passes.",
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

// Repeat parses the number of runs per case (WEFT_EVAL_REPEAT): empty means
// 1, anything else must be a positive whole number.
func Repeat(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("eval: repeat %q is not a positive whole number", v)
	}
	return n, nil
}

// Summary aggregates the runs of one model in one workflow.
type Summary struct {
	Provider, Model, Workflow string
	// Runs counts every attempt (cases times repetitions); Solved counts
	// the attempts that passed.
	Runs, Solved              int
	ToolCalls, ToolErrors     int
	InputTokens, OutputTokens int
	Duration                  time.Duration
}

// SolveRate is Solved over Runs, from 0 to 1.
func (s Summary) SolveRate() float64 {
	if s.Runs == 0 {
		return 0
	}
	return float64(s.Solved) / float64(s.Runs)
}

// CaseSummary aggregates the repetitions of one case for one model in one
// workflow.
type CaseSummary struct {
	Provider, Model, Workflow, Case string
	Attempts, Passes                int
	ToolCalls, ToolErrors           int
	ContextOps                      int
	InputTokens, OutputTokens       int
	Duration                        time.Duration
	// Statuses are the attempts' final statuses, in run order.
	Statuses []string
}

// Summarize groups results by provider, model and workflow.
func Summarize(rs []Result) []Summary {
	by := map[string]*Summary{}
	var order []string
	for _, r := range rs {
		k := r.Provider + "\x00" + r.Model + "\x00" + r.Workflow
		s := by[k]
		if s == nil {
			s = &Summary{Provider: r.Provider, Model: r.Model, Workflow: r.Workflow}
			by[k] = s
			order = append(order, k)
		}
		s.Runs++
		if r.Success {
			s.Solved++
		}
		s.ToolCalls += r.ToolCalls
		s.ToolErrors += r.ToolErrors
		s.InputTokens += r.InputTokens
		s.OutputTokens += r.OutputTokens
		s.Duration += r.Duration
	}
	sort.Strings(order)
	out := make([]Summary, 0, len(by))
	for _, k := range order {
		out = append(out, *by[k])
	}
	return out
}

// SummarizeCases groups results by provider, model, workflow and case, in
// the order the cases first ran.
func SummarizeCases(rs []Result) []CaseSummary {
	by := map[string]*CaseSummary{}
	var order []string
	for _, r := range rs {
		k := r.Provider + "\x00" + r.Model + "\x00" + r.Workflow + "\x00" + r.Case
		c := by[k]
		if c == nil {
			c = &CaseSummary{Provider: r.Provider, Model: r.Model, Workflow: r.Workflow, Case: r.Case}
			by[k] = c
			order = append(order, k)
		}
		c.Attempts++
		if r.Success {
			c.Passes++
		}
		c.ToolCalls += r.ToolCalls
		c.ToolErrors += r.ToolErrors
		c.ContextOps += r.ContextOps
		c.InputTokens += r.InputTokens
		c.OutputTokens += r.OutputTokens
		c.Duration += r.Duration
		c.Statuses = append(c.Statuses, r.Status)
	}
	out := make([]CaseSummary, 0, len(order))
	for _, k := range order {
		out = append(out, *by[k])
	}
	return out
}

// Markdown renders results as tables for the design page: the solve rate
// over every run per model and workflow, then passes over attempts per
// case. Counts, tokens and times are totals over the attempts.
func Markdown(rs []Result) string {
	var b strings.Builder
	b.WriteString("| Provider | Model | Workflow | Solved | Solve rate | Tool calls | Tool errors | Tokens in / out | Time |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, s := range Summarize(rs) {
		fmt.Fprintf(&b, "| %s | %s | %s | %d/%d | %.0f%% | %d | %d | %d / %d | %s |\n", s.Provider, s.Model, s.Workflow, s.Solved, s.Runs, 100*s.SolveRate(), s.ToolCalls, s.ToolErrors, s.InputTokens, s.OutputTokens, s.Duration.Round(time.Second))
	}
	b.WriteString("\n| Provider | Model | Workflow | Case | Passed | Tool calls | Tool errors | Context ops | Tokens in / out | Time | Status |\n|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range SummarizeCases(rs) {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d/%d | %d | %d | %d | %d / %d | %s | %s |\n", c.Provider, c.Model, c.Workflow, c.Case, c.Passes, c.Attempts, c.ToolCalls, c.ToolErrors, c.ContextOps, c.InputTokens, c.OutputTokens, c.Duration.Round(time.Second), strings.Join(c.Statuses, ", "))
	}
	return b.String()
}
