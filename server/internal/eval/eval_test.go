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
}

// scripted replays a solution per case (or none when lazy) with fixed usage.
type scripted struct{ lazy bool }

func (s scripted) Name() string {
	if s.lazy {
		return "lazy"
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
		if err != nil || len(evs) != 2 {
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
	if len(sums) != 2 || sums[0].Provider != "lazy" || sums[0].Solved != 0 || sums[1].Solved != len(eval.Cases) || sums[1].Cases != len(eval.Cases) {
		t.Fatalf("summary = %+v", sums)
	}
	md := eval.Markdown(results)
	if !strings.Contains(md, "| scripted | script | 4/4 |") || !strings.Contains(md, "| scripted | script | fix-failing-test | pass |") || !strings.Contains(md, "| lazy | script | fix-failing-test | fail |") {
		t.Fatalf("markdown:\n%s", md)
	}
}

// TestLocalModels measures real models. It runs only when
// WEFT_EVAL_OLLAMA_URL is set; WEFT_EVAL_MODELS is a comma list
// (default llama3.2:latest), WEFT_EVAL_REPORT a file for the tables and
// WEFT_EVAL_GUIDANCE the providers' model guidance ("" or "small").
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
		for _, c := range eval.Cases {
			w := eval.Single
			if os.Getenv("WEFT_EVAL_WORKFLOW") == string(eval.Factory) {
				w = eval.Factory
			}
			r := eval.RunWorkflow(t.Context(), a, ws, w, c, name, m, n, 30, 20*time.Minute)
			n++
			t.Logf("%s %s success=%v status=%s tools=%d errors=%d tokens=%d/%d time=%s %s %v", m, c.ID, r.Success, r.Status, r.ToolCalls, r.ToolErrors, r.InputTokens, r.OutputTokens, r.Duration.Round(time.Second), r.Error, r.ToolFailures)
			results = append(results, r)
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
