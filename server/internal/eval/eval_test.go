package eval_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/eval"
	"github.com/woliveiras/weft/server/internal/llm"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
	"github.com/woliveiras/weft/server/internal/storage"
)

// newApp starts a disposable installation whose workspaces live in the
// returned directory.
func newApp(t *testing.T) (*app.App, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Instance = "eval"
	cfg.Storage = config.Storage{Type: "sqlite", Dir: dir}
	cfg.Queue = config.Queue{Type: "memory"}
	cfg.Workspaces.Root = filepath.Join(dir, "ws")
	cfg.Secrets = config.Secrets{KeyFile: filepath.Join(dir, "secret.key"), GenerateKeyFile: true}
	cfg.Auth.Disabled = true
	cfg.Observability.LogLevel = "error"
	cfg.Engine.ReconcileInterval = 100 * time.Millisecond
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		a.Engine.Shutdown(sctx)
		_ = a.Close(sctx)
	})
	return a, cfg.Workspaces.Root
}

type step struct {
	tool string
	args any
}

func exec(argv ...string) step { return step{"shell.exec", map[string]any{"argv": argv}} }
func edit(path, old, new string, all bool) step {
	return step{"filesystem.edit", map[string]any{"path": path, "old": old, "new": new, "replaceAll": all}}
}
func write(path, content string) step {
	return step{"filesystem.write", map[string]any{"path": path, "content": content}}
}

// solutions solve each case the way a good agent would.
var solutions = map[string][]step{
	"fix-failing-test": {
		{"filesystem.read", map[string]any{"path": "calc.go"}},
		edit("calc.go", "return a - b", "return a + b", false),
		exec("go", "test", "./..."),
	},
	"implement-function": {
		edit("strutil.go", "\t// TODO: implement\n\treturn \"\"", "\tr := []rune(s)\n\tfor i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {\n\t\tr[i], r[j] = r[j], r[i]\n\t}\n\treturn string(r)", false),
		write("strutil_test.go", "package strutil\n\nimport \"testing\"\n\nfunc TestReverse(t *testing.T) {\n\tif Reverse(\"ab\") != \"ba\" {\n\t\tt.Fatal(Reverse(\"ab\"))\n\t}\n}\n"),
		exec("go", "test", "./..."),
	},
	"rename-across-files": {
		{"filesystem.search", map[string]any{"pattern": "GetUserName"}},
		edit("user.go", "GetUserName", "UserName", true),
		edit("greet.go", "GetUserName", "UserName", true),
		edit("report.go", "GetUserName", "UserName", true),
		exec("go", "test", "./..."),
	},
	"bug-from-issue": {
		edit("dur.go", "\tcase 's', 'm':\n\t\treturn time.Duration(n) * time.Second, nil", "\tcase 's':\n\t\treturn time.Duration(n) * time.Second, nil\n\tcase 'm':\n\t\treturn time.Duration(n) * time.Minute, nil", false),
		write("dur_minutes_test.go", "package dur\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestParseMinutes(t *testing.T) {\n\tif d, err := Parse(\"10m\"); err != nil || d != 10*time.Minute {\n\t\tt.Fatal(d, err)\n\t}\n}\n"),
		exec("go", "test", "./..."),
	},
	"discount-across-packages": {
		{"filesystem.search", map[string]any{"pattern": "Total"}},
		edit("order/order.go", "\tID    string\n\tLines []Line\n}", "\tID              string\n\tLines           []Line\n\tDiscountPercent int\n}", false),
		edit("order/order.go", "\treturn o.Subtotal()\n", "\tsub := o.Subtotal()\n\treturn sub - sub*o.DiscountPercent/100\n", false),
		edit("order/order.go", "\t\treturn errors.New(\"order: missing id\")\n\t}\n", "\t\treturn errors.New(\"order: missing id\")\n\t}\n\tif o.DiscountPercent < 0 || o.DiscountPercent > 100 {\n\t\treturn fmt.Errorf(\"order %s: discount %d%% is outside 0-100\", o.ID, o.DiscountPercent)\n\t}\n", false),
		edit("invoice/invoice.go", "\t"+`fmt.Fprintf(&b, "Total: `, "\tif o.DiscountPercent != 0 {\n\t\t"+`fmt.Fprintf(&b, "Discount: %d%%\n", o.DiscountPercent)`+"\n\t}\n\t"+`fmt.Fprintf(&b, "Total: `, false),
		edit("api/api.go", "\tTotalCents int        `json:\"total_cents\"`\n}", "\tTotalCents int        `json:\"total_cents\"`\n\tDiscount   int        `json:\"discount_percent\"`\n}", false),
		edit("api/api.go", "TotalCents: o.Total()}", "TotalCents: o.Total(), Discount: o.DiscountPercent}", false),
		edit("api/api.go", "o := order.Order{ID: w.ID}", "o := order.Order{ID: w.ID, DiscountPercent: w.Discount}", false),
		write("order/discount_test.go", "package order\n\nimport \"testing\"\n\nfunc TestDiscount(t *testing.T) {\n\to := Order{ID: \"A\", Lines: []Line{{SKU: \"x\", Quantity: 1, UnitCents: 999}}, DiscountPercent: 10}\n\tif o.Total() != 900 {\n\t\tt.Fatal(o.Total())\n\t}\n}\n"),
		exec("go", "test", "./..."),
	},
	"stale-cache": {
		{"filesystem.read", map[string]any{"path": "service.go"}},
		edit("cache.go", "\tc.m[key] = value\n}\n", "\tc.m[key] = value\n}\n\n// Delete removes key from the Store and from memory.\nfunc (c *Cache) Delete(key string) {\n\tc.mu.Lock()\n\tdefer c.mu.Unlock()\n\tc.next.Delete(key)\n\tdelete(c.m, key)\n}\n", false),
		edit("service.go", "s.store.Set(themeKey(user), theme)", "s.cache.Set(themeKey(user), theme)", false),
		edit("service.go", "s.store.Delete(themeKey(user))", "s.cache.Delete(themeKey(user))", false),
		write("regression_test.go", "package prefs\n\nimport \"testing\"\n\nfunc TestThemeChangeAfterRead(t *testing.T) {\n\tsvc := NewService(NewMemStore())\n\t_ = svc.SetTheme(\"ada\", \"dark\")\n\t_ = svc.Theme(\"ada\")\n\t_ = svc.SetTheme(\"ada\", \"contrast\")\n\tif got := svc.Theme(\"ada\"); got != \"contrast\" {\n\t\tt.Fatal(got)\n\t}\n\tsvc.ResetTheme(\"ada\")\n\tif got := svc.Theme(\"ada\"); got != DefaultTheme {\n\t\tt.Fatal(got)\n\t}\n}\n"),
		exec("go", "test", "./..."),
	},
	"misleading-issue": {
		{"filesystem.read", map[string]any{"path": "release.go"}},
		write("release.go", releaseFixed),
		write("regression_test.go", "package release\n\nimport \"testing\"\n\nfunc TestCompareNumeric(t *testing.T) {\n\tif Compare(\"v1.10.0\", \"v1.9.0\") != 1 || Compare(\"1.4\", \"v1.4.0\") != 0 {\n\t\tt.Fatal(\"numeric comparison\")\n\t}\n}\n"),
		exec("go", "test", "./..."),
	},
	"printf-logger-vet": {
		{"filesystem.search", map[string]any{"pattern": "Log(", "literal": true}},
		write("logx/logx.go", logxFixed),
		write("runner/runner.go", runnerFixed),
		edit("logx/logx_test.go", `l.Log("hello", 3)`, `l.Logf("hello %d", 3)`, false),
		exec("go", "vet", "./..."),
		exec("go", "test", "./..."),
	},
	"truncate-shared-helper": {
		{"filesystem.search", map[string]any{"pattern": "Truncate(", "literal": true}},
		write("preview/preview.go", previewFixed),
		write("preview/regression_test.go", "package preview\n\nimport \"testing\"\n\nfunc TestTitleRunes(t *testing.T) {\n\tif got := Title(\"\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\"); len([]rune(got)) != MaxTitle {\n\t\tt.Fatal(got)\n\t}\n}\n"),
		exec("go", "test", "./..."),
	},
}

const releaseFixed = `// Package release picks releases by their version tags.
package release

import (
	"strconv"
	"strings"
)

// Compare compares two version tags such as "v1.2.3" or "1.4" and returns
// -1 when a is older than b, 0 when they are the same version and +1 when a
// is newer. Numbers compare numerically and a missing number counts as 0.
func Compare(a, b string) int {
	pa, pb := numbers(a), numbers(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := at(pa, i), at(pb, i)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

func numbers(tag string) []int {
	fields := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	out := make([]int, len(fields))
	for i, f := range fields {
		out[i], _ = strconv.Atoi(f) // a malformed number counts as 0
	}
	return out
}

func at(ns []int, i int) int {
	if i < len(ns) {
		return ns[i]
	}
	return 0
}

// Latest returns the newest of tags, or "" when there are none.
func Latest(tags []string) string {
	latest := ""
	for _, t := range tags {
		if latest == "" || Compare(t, latest) > 0 {
			latest = t
		}
	}
	return latest
}
`

const logxFixed = `// Package logx is the job runner's small line logger.
package logx

import (
	"fmt"
	"io"
	"sync"
)

// Logger writes prefixed lines to a writer. Lines from concurrent callers
// never interleave.
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	prefix string
}

// New returns a Logger that writes to w and starts every line with prefix.
func New(w io.Writer, prefix string) *Logger {
	return &Logger{w: w, prefix: prefix}
}

// Logf writes the prefix, a space and the formatted message, as one line.
func (l *Logger) Logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s %s\n", l.prefix, fmt.Sprintf(format, args...))
}
`

const runnerFixed = `// Package runner runs jobs and logs their outcome.
package runner

import "example.com/jobs/logx"

// Job is a named unit of work.
type Job struct {
	Name string
	Fn   func() error
}

// Runner runs jobs one after another.
type Runner struct {
	log *logx.Logger
}

// New returns a Runner that logs to log.
func New(log *logx.Logger) *Runner {
	return &Runner{log: log}
}

// Run runs every job, logs each outcome and returns how many failed.
func (r *Runner) Run(jobs []Job) int {
	r.log.Logf("starting %d jobs", len(jobs))
	failed := 0
	for _, j := range jobs {
		if err := j.Fn(); err != nil {
			r.log.Logf("job %s failed: %v", j.Name, err)
			failed++
			continue
		}
		r.log.Logf("job %s done", j.Name)
	}
	r.log.Logf("finished, %d failed", failed)
	return failed
}
`

const previewFixed = `// Package preview renders note titles for the notes list.
package preview

import "strings"

// MaxTitle is how many characters of a title the notes list shows.
const MaxTitle = 20

// Title returns the part of a note title the notes list shows: at most
// MaxTitle characters, never a partial one.
func Title(title string) string {
	r := []rune(strings.TrimSpace(title))
	if len(r) > MaxTitle {
		r = r[:MaxTitle]
	}
	return string(r)
}
`

// traps are plausible but wrong fixes. Each must fail, which shows the
// hidden tests and checks judge the whole request and not only its most
// obvious part.
var traps = map[string][]step{
	// Every caller renamed, but the doc comment keeps the old name.
	"rename-across-files": {
		edit("user.go", "func GetUserName", "func UserName", false),
		edit("greet.go", "GetUserName", "UserName", true),
		edit("report.go", "GetUserName", "UserName", true),
	},
	// Discount in the model and invoice, but the API was forgotten.
	"discount-across-packages": solutions["discount-across-packages"][:5],
	// SetTheme writes through the cache, ResetTheme still bypasses it.
	"stale-cache": {edit("service.go", "s.store.Set(themeKey(user), theme)", "s.cache.Set(themeKey(user), theme)", false)},
	// The issue's suggestion: compare by length first.
	"misleading-issue": {edit("release.go", "Compare(t, latest) > 0", "len(t) > len(latest) || (len(t) == len(latest) && t > latest)", false)},
	// Printf style, but the Logger is still copied with its mutex.
	"printf-logger-vet": {
		write("logx/logx.go", strings.NewReplacer("*Logger", "Logger", "&Logger", "Logger").Replace(logxFixed)),
		write("runner/runner.go", strings.ReplaceAll(runnerFixed, "*logx.Logger", "logx.Logger")),
		edit("logx/logx_test.go", `l.Log("hello", 3)`, `l.Logf("hello %d", 3)`, false),
	},
	// The shared helper counts runes now, so storage keys outgrow the backend.
	"truncate-shared-helper": {write("text/text.go", "// Package text has string helpers shared by the notes service.\npackage text\n\n// Truncate shortens s to at most n characters.\nfunc Truncate(s string, n int) string {\n\tr := []rune(s)\n\tif len(r) <= n {\n\t\treturn s\n\t}\n\treturn string(r[:n])\n}\n")},
}

// scripted replays a solution per case (none when lazy, the case's trap
// when trap) with fixed usage.
type scripted struct{ lazy, trap bool }

func (s scripted) Name() string {
	switch {
	case s.lazy:
		return "lazy"
	case s.trap:
		return "trap"
	}
	return "scripted"
}
func (scripted) Models(context.Context) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "script"}}, nil
}
func (s scripted) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	graph := req.Metadata["graph"]
	caseID := strings.TrimPrefix(graph[:strings.LastIndex(graph, "-")], "eval-")
	round := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			round++
		}
	}
	usage := llm.Usage{InputTokens: 100, OutputTokens: 20}
	steps := solutions[caseID]
	if s.lazy {
		steps = nil
	}
	if s.trap {
		steps = traps[caseID]
	}
	if round < len(steps) {
		args, _ := json.Marshal(steps[round].args)
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("c%d", round), Name: steps[round].tool, Arguments: args}}}, StopReason: "tool_calls", Usage: usage}, nil
	}
	args, _ := json.Marshal(map[string]string{"summary": "done"})
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "fin", Name: "finish", Arguments: args}}}, StopReason: "tool_calls", Usage: usage}, nil
}

// factoryScript plays every role of Software Factory v2 for a case: the
// planner and reviewer hand on, the implementer applies the solution and
// runs the declared test check, the verifier runs it again.
type factoryScript struct{}

func (factoryScript) Name() string { return "factory-scripted" }
func (factoryScript) Models(context.Context) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "script"}}, nil
}

func emitStep(typ string) step {
	return step{"emit", map[string]any{"type": typ, "payload": map[string]string{"request": "the case request"}}}
}

var checkTest = step{"check.run", map[string]any{"name": "test"}}

func (factoryScript) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	graph := req.Metadata["graph"]
	caseID := strings.TrimPrefix(graph[:strings.LastIndex(graph, "-")], "eval-")
	var steps []step
	switch req.Metadata["node"] {
	case "planner":
		steps = []step{emitStep("task.ready")}
	case "implementer":
		for _, s := range solutions[caseID] {
			if s.tool == "shell.exec" {
				s = checkTest
			}
			steps = append(steps, s)
		}
		steps = append(steps, emitStep("code.ready"))
	case "reviewer":
		steps = []step{emitStep("review.approved")}
	case "verifier":
		steps = []step{checkTest, emitStep("verify.passed")}
	}
	round := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			round++
		}
	}
	usage := llm.Usage{InputTokens: 100, OutputTokens: 20}
	if round < len(steps) {
		args, _ := json.Marshal(steps[round].args)
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("c%d", round), Name: steps[round].tool, Arguments: args}}}, StopReason: "tool_calls", Usage: usage}, nil
	}
	args, _ := json.Marshal(map[string]string{"summary": "done"})
	return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "fin", Name: "finish", Arguments: args}}}, StopReason: "tool_calls", Usage: usage}, nil
}

func TestSoftwareFactoryV2SolvesTheScriptedCases(t *testing.T) {
	a, ws := newApp(t)
	a.Models.Add(llm.ProviderConfig{Name: "factory-scripted", Type: "mock", DefaultModel: "script"}, factoryScript{})
	for i, c := range eval.Cases {
		r := eval.RunWorkflow(t.Context(), a, ws, eval.Factory, c, "factory-scripted", "script", 100+i, 20, 2*time.Minute)
		if !r.Success || r.Status != "succeeded" || r.Workflow != "factory" || r.ToolErrors != 0 {
			t.Fatalf("%s: %+v\n%s", c.ID, r, r.CheckOutput)
		}
		// The implementer's edits and check, plus the verifier's check.
		if want := len(solutions[c.ID]) + 1; r.ToolCalls != want {
			t.Fatalf("%s: %d gateway calls, want %d", c.ID, r.ToolCalls, want)
		}
		evs, err := a.Store.ListEvents(t.Context(), storage.EventFilter{GraphID: fmt.Sprintf("eval-%s-%d", c.ID, 100+i), Types: []domain.EventType{domain.EvCheckCompleted}})
		// One check per scripted shell step, plus the verifier's.
		checks := 1
		for _, s := range solutions[c.ID] {
			if s.tool == "shell.exec" {
				checks++
			}
		}
		if err != nil || len(evs) != checks {
			t.Fatalf("%s: check evidence %d %v", c.ID, len(evs), err)
		}
	}
}

func TestFactoryGraphKeepsTheGuardrails(t *testing.T) {
	g, err := eval.FactoryGraph("eval-x-1", "p", "m", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Spec.Checks) != 1 || g.Spec.Checks[0].Command != "go test ./..." || g.Channel("release").Approval {
		t.Fatalf("%+v", g.Spec.Checks)
	}
	for _, n := range g.Spec.Nodes {
		if n.Model.Provider != "p" || n.Limits.MaxToolCalls != 25 {
			t.Fatalf("%s: %+v", n.ID, n.Model)
		}
		for _, grant := range n.Tools {
			if grant.Name == "shell.exec" && strings.Join(grant.Commands, ",") != "go *,gofmt *" {
				t.Fatalf("%s: shell grant %v", n.ID, grant.Commands)
			}
		}
	}
}

func TestScriptedSolutionsPassAndLazyAnswersFail(t *testing.T) {
	a, ws := newApp(t)
	a.Models.Add(llm.ProviderConfig{Name: "scripted", Type: "mock", DefaultModel: "script"}, scripted{})
	a.Models.Add(llm.ProviderConfig{Name: "lazy", Type: "mock", DefaultModel: "script"}, scripted{lazy: true})
	var results []eval.Result
	for i, c := range eval.Cases {
		for j, p := range []string{"scripted", "lazy"} {
			r := eval.Run(t.Context(), a, ws, c, p, "script", i*2+j, 20, time.Minute)
			results = append(results, r)
			if r.Status != "succeeded" {
				t.Fatalf("%s/%s: the turn did not succeed: %+v", c.ID, p, r)
			}
			if want := p == "scripted"; r.Success != want {
				t.Fatalf("%s/%s: success %v, want %v\n%s", c.ID, p, r.Success, want, r.CheckOutput)
			}
		}
	}
	for _, r := range results {
		steps := len(solutions[r.Case])
		if r.Provider == "lazy" {
			steps = 0
		}
		if r.ToolCalls != steps || r.InputTokens != 100*(steps+1) || r.OutputTokens != 20*(steps+1) || r.ToolErrors != 0 || r.Duration <= 0 {
			t.Fatalf("metrics %+v (steps %d)", r, steps)
		}
	}
	sums := eval.Summarize(results)
	if len(sums) != 2 || sums[0].Provider != "lazy" || sums[0].Solved != 0 || sums[1].Solved != len(eval.Cases) || sums[1].Runs != len(eval.Cases) || sums[1].Workflow != "single" {
		t.Fatalf("summary = %+v", sums)
	}
	md := eval.Markdown(results)
	all := fmt.Sprintf("| scripted | script | single | %d/%d | 100%% |", len(eval.Cases), len(eval.Cases))
	if !strings.Contains(md, all) || !strings.Contains(md, "| scripted | script | single | stale-cache | 1/1 |") || !strings.Contains(md, "| lazy | script | single | fix-failing-test | 0/1 |") {
		t.Fatalf("markdown:\n%s", md)
	}
}

// TestPlausibleWrongFixesFail runs a tempting but incomplete fix of the
// harder cases (and of the rename): the checks must reject every one.
func TestPlausibleWrongFixesFail(t *testing.T) {
	a, ws := newApp(t)
	a.Models.Add(llm.ProviderConfig{Name: "trap", Type: "mock", DefaultModel: "script"}, scripted{trap: true})
	n := 0
	for _, c := range eval.Cases {
		if traps[c.ID] == nil {
			continue
		}
		r := eval.Run(t.Context(), a, ws, c, "trap", "script", 200+n, 20, time.Minute)
		n++
		if r.Status != "succeeded" || r.ToolErrors != 0 || r.ToolCalls != len(traps[c.ID]) {
			t.Fatalf("%s: the trap did not run as scripted: %+v", c.ID, r)
		}
		if r.Success {
			t.Fatalf("%s: a wrong fix passed the hidden checks\n%s", c.ID, r.CheckOutput)
		}
		t.Logf("%s: rejected as expected:\n%s", c.ID, r.CheckOutput)
	}
	if n != len(traps) {
		t.Fatalf("%d traps ran, want %d", n, len(traps))
	}
}

func TestRepeat(t *testing.T) {
	for in, want := range map[string]int{"": 1, "1": 1, " 3 ": 3} {
		if got, err := eval.Repeat(in); err != nil || got != want {
			t.Errorf("Repeat(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "-2", "two", "1.5"} {
		if _, err := eval.Repeat(in); err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("Repeat(%q): %v, want an error naming the value", in, err)
		}
	}
}

func TestSummariesCountRepetitionsPerWorkflow(t *testing.T) {
	r := func(c, w string, ok bool, status string) eval.Result {
		return eval.Result{Case: c, Provider: "p", Model: "m", Workflow: w, Success: ok, Status: status, ToolCalls: 2, InputTokens: 10, OutputTokens: 1, Duration: time.Second}
	}
	rs := []eval.Result{
		r("a", "single", true, "succeeded"), r("b", "single", false, "failed"),
		r("a", "single", false, "succeeded"), r("b", "single", true, "succeeded"),
		r("a", "factory", true, "succeeded"),
	}
	sums := eval.Summarize(rs)
	if len(sums) != 2 || sums[0].Workflow != "factory" || sums[0].Runs != 1 || sums[1].Workflow != "single" || sums[1].Runs != 4 || sums[1].Solved != 2 || sums[1].SolveRate() != 0.5 || sums[1].ToolCalls != 8 {
		t.Fatalf("summary = %+v", sums)
	}
	if (eval.Summary{}).SolveRate() != 0 {
		t.Fatal("an empty summary has no solve rate")
	}
	cases := eval.SummarizeCases(rs)
	if len(cases) != 3 || cases[0].Case != "a" || cases[0].Attempts != 2 || cases[0].Passes != 1 || strings.Join(cases[1].Statuses, ",") != "failed,succeeded" {
		t.Fatalf("cases = %+v", cases)
	}
	md := eval.Markdown(rs)
	for _, want := range []string{"| p | m | single | 2/4 | 50% | 8 |", "| p | m | factory | 1/1 | 100% |", "| p | m | single | b | 1/2 | 4 | 0 | 0 | 20 / 2 | 2s | failed, succeeded |"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in\n%s", want, md)
		}
	}
}

// TestLocalModels measures real models. It runs only when
// WEFT_EVAL_OLLAMA_URL is set; WEFT_EVAL_MODELS is a comma list
// (default llama3.2:latest), WEFT_EVAL_REPORT a file for the tables and
// WEFT_EVAL_GUIDANCE the providers' model guidance ("" or "small"),
// WEFT_EVAL_WORKFLOW the workflow ("" or "factory") and WEFT_EVAL_REPEAT
// how many times each case runs (default 1).
func TestLocalModels(t *testing.T) {
	url := os.Getenv("WEFT_EVAL_OLLAMA_URL")
	if url == "" {
		t.Skip("set WEFT_EVAL_OLLAMA_URL to measure local models")
	}
	measure(t, "ollama", url, "", "llama3.2:latest", 900)
}

// TestCloudModels measures models behind an OpenAI-compatible endpoint
// (WEFT_EVAL_OPENAI_URL, key in WEFT_EVAL_OPENAI_KEY, comma-separated
// WEFT_EVAL_MODELS). Calls go one at a time with retries, so free-tier
// rate limits slow a run down instead of failing it.
func TestCloudModels(t *testing.T) {
	url := os.Getenv("WEFT_EVAL_OPENAI_URL")
	if url == "" {
		t.Skip("set WEFT_EVAL_OPENAI_URL and WEFT_EVAL_OPENAI_KEY to measure hosted models")
	}
	measure(t, "openai", url, os.Getenv("WEFT_EVAL_OPENAI_KEY"), "", 300)
}

func measure(t *testing.T, typ, url, key, defaultModel string, timeoutSec int) {
	t.Helper()
	models := strings.Split(os.Getenv("WEFT_EVAL_MODELS"), ",")
	if models[0] == "" {
		if defaultModel == "" {
			t.Fatal("set WEFT_EVAL_MODELS")
		}
		models = []string{defaultModel}
	}
	repeat, err := eval.Repeat(os.Getenv("WEFT_EVAL_REPEAT"))
	if err != nil {
		t.Fatal(err)
	}
	w := eval.Single
	if os.Getenv("WEFT_EVAL_WORKFLOW") == string(eval.Factory) {
		w = eval.Factory
	}
	a, ws := newApp(t)
	var results []eval.Result
	n := 0
	for _, m := range models {
		name := typ + "-" + strings.NewReplacer(":", "-", ".", "-", "/", "-").Replace(m)
		cfg := llm.ProviderConfig{Name: name, Type: typ, BaseURL: url, DefaultModel: m, TimeoutSec: timeoutSec, MaxRetries: 4, Concurrency: 1, APIKeyValue: key}
		p, err := llm.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		a.Models.Add(llm.ProviderConfig{Name: name, Type: typ, DefaultModel: m, Concurrency: 1, ModelGuidance: os.Getenv("WEFT_EVAL_GUIDANCE")}, p)
		for rep := 1; rep <= repeat; rep++ {
			for _, c := range eval.Cases {
				r := eval.RunWorkflow(t.Context(), a, ws, w, c, name, m, n, 30, 20*time.Minute)
				n++
				t.Logf("%s %s run %d/%d success=%v status=%s tools=%d errors=%d tokens=%d/%d time=%s %s %v", m, c.ID, rep, repeat, r.Success, r.Status, r.ToolCalls, r.ToolErrors, r.InputTokens, r.OutputTokens, r.Duration.Round(time.Second), r.Error, r.ToolFailures)
				results = append(results, r)
			}
		}
	}
	md := eval.Markdown(results)
	t.Log("\n" + md)
	if out := os.Getenv("WEFT_EVAL_REPORT"); out != "" {
		raw, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(out, append([]byte(md+"\n<!--\n"), append(raw, []byte("\n-->\n")...)...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
