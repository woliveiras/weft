package native

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/tools"
)

// scriptedEnv answers model calls from a script and records tool calls.
type scriptedEnv struct {
	replies []*llm.ChatResponse
	asked   []*llm.ChatRequest
	tools   []string
	outputs []string
	emits   []runtime.Emit
}

func (e *scriptedEnv) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	e.asked = append(e.asked, req)
	if len(e.replies) == 0 {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: "done"}, StopReason: "stop"}, nil
	}
	r := e.replies[0]
	e.replies = e.replies[1:]
	return r, nil
}

func (e *scriptedEnv) InvokeTool(_ context.Context, name string, args json.RawMessage) (*tools.Result, error) {
	e.tools = append(e.tools, name+" "+string(args))
	return &tools.Result{Content: "ok"}, nil
}
func (e *scriptedEnv) Emit(_ context.Context, em runtime.Emit) error {
	e.emits = append(e.emits, em)
	return nil
}
func (e *scriptedEnv) StateGet(context.Context, string) ([]byte, error) {
	return nil, errors.New("unset")
}
func (e *scriptedEnv) StatePut(context.Context, string, []byte) error { return nil }
func (e *scriptedEnv) Output(_ context.Context, t string)             { e.outputs = append(e.outputs, t) }
func (e *scriptedEnv) AskHuman(context.Context, string) (string, error) {
	return "yes", nil
}

func text(s string) *llm.ChatResponse {
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: s}, StopReason: "stop"}
}

func toolCall(name, args string) *llm.ChatResponse {
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "c1", Name: name, Arguments: json.RawMessage(args)}}}, StopReason: "tool_calls"}
}

var granted = []llm.ToolSchema{{Name: "git.branch"}, {Name: "shell.exec"}}

func turn(t *testing.T, env *scriptedEnv, in *runtime.TurnInput) (*runtime.TurnResult, error) {
	t.Helper()
	if in == nil {
		in = &runtime.TurnInput{Prompt: "update dependencies", Tools: granted}
	}
	return New().Turn(context.Background(), in, env)
}

func TestStructuredToolCallsRunUntilTheAnswer(t *testing.T) {
	env := &scriptedEnv{replies: []*llm.ChatResponse{toolCall("git.branch", `{"name":"deps/update"}`), text("Branch created.")}}
	res, err := turn(t, env, nil)
	if err != nil || res.Summary != "Branch created." || len(env.tools) != 1 {
		t.Fatalf("%+v %v %v", res, err, env.tools)
	}
}

func TestToolCallWrittenAsTextIsRecovered(t *testing.T) {
	for name, written := range map[string]string{
		"fenced":    "```json\n{\n  \"name\": \"git__branch\",\n  \"arguments\": {\"name\": \"deps/update\", \"checkout\": true}\n}\n```",
		"bare":      `{"name": "git.branch", "arguments": {"name": "deps/update"}}`,
		"string":    `{"name": "git__branch", "arguments": "{\"name\": \"deps/update\"}"}`,
		"array":     `[{"name": "git__branch", "arguments": {"name": "a"}}, {"name": "shell__exec", "parameters": {"argv": ["go", "version"]}}]`,
		"no fields": "```\n{\"name\": \"git__branch\"}\n```",
	} {
		t.Run(name, func(t *testing.T) {
			env := &scriptedEnv{replies: []*llm.ChatResponse{text(written), text("Done.")}}
			res, err := turn(t, env, nil)
			if err != nil || res.Summary != "Done." {
				t.Fatalf("%+v %v", res, err)
			}
			if len(env.tools) == 0 || !strings.HasPrefix(env.tools[0], "git.branch ") {
				t.Fatalf("recovered calls: %v", env.tools)
			}
			if !strings.Contains(strings.Join(env.outputs, "\n"), "wrote") {
				t.Errorf("the recovery must be visible in the output: %v", env.outputs)
			}
			// The history sent back to the model pairs the result with a call.
			second := env.asked[1].Messages
			if a := second[1]; a.Role != llm.RoleAssistant || len(a.ToolCalls) == 0 || a.Content != "" {
				t.Fatalf("assistant turn in history: %+v", a)
			}
			if second[2].Role != llm.RoleTool || second[2].ToolCallID != second[1].ToolCalls[0].ID {
				t.Fatalf("tool result in history: %+v", second[2])
			}
		})
	}
}

func TestToolCallTextThatCannotRunIsCorrectedThenFailsTheTurn(t *testing.T) {
	for name, written := range map[string]string{
		"unknown tool":  `{"name": "filesystem__delete", "arguments": {"path": "."}}`,
		"broken json":   "```json\n{\"name\": \"git__branch\", \"arguments\": {\"name\": \n```",
		"non-json args": `{"name": "git__branch", "arguments": "create deps/update"}`,
	} {
		t.Run(name, func(t *testing.T) {
			env := &scriptedEnv{replies: []*llm.ChatResponse{text(written), text(written), text(written)}}
			_, err := turn(t, env, nil)
			if !errors.Is(err, ErrToolCallAsText) {
				t.Fatalf("want ErrToolCallAsText, got %v", err)
			}
			if len(env.tools) != 0 || len(env.asked) != maxCorrections+1 {
				t.Fatalf("nothing may run: %v after %d model calls", env.tools, len(env.asked))
			}
		})
	}
}

func TestOrdinaryJSONAnswersAreNotToolCalls(t *testing.T) {
	env := &scriptedEnv{replies: []*llm.ChatResponse{text(`{"verdict": "approved", "notes": []}`)}}
	res, err := turn(t, env, nil)
	if err != nil || !strings.Contains(res.Summary, "approved") || len(env.tools) != 0 {
		t.Fatalf("%+v %v %v", res, err, env.tools)
	}
}

func TestEmptyOrCutOffAnswersFail(t *testing.T) {
	env := &scriptedEnv{replies: []*llm.ChatResponse{text("  ")}}
	if _, err := turn(t, env, nil); !errors.Is(err, ErrEmptyAnswer) {
		t.Fatalf("empty: %v", err)
	}
	env = &scriptedEnv{replies: []*llm.ChatResponse{{Message: llm.ChatMessage{}, StopReason: "length"}}}
	if _, err := turn(t, env, nil); !errors.Is(err, ErrAnswerCutOff) {
		t.Fatalf("cut off: %v", err)
	}
	// A runaway answer stopped by the output limit is not a finished turn.
	env = &scriptedEnv{replies: []*llm.ChatResponse{{Message: llm.ChatMessage{Content: strings.Repeat("loop ", 100)}, StopReason: "length"}}}
	if _, err := turn(t, env, nil); !errors.Is(err, ErrAnswerCutOff) || !strings.Contains(err.Error(), "loop loop") {
		t.Fatalf("cut off with text: %v", err)
	}
	// An empty final answer after doing work is a silent finish, not an error.
	env = &scriptedEnv{replies: []*llm.ChatResponse{toolCall("git.branch", `{"name":"x"}`), text("")}}
	if _, err := turn(t, env, nil); err != nil {
		t.Fatalf("empty after work: %v", err)
	}
}

func TestRoundsFollowTheToolCallBudget(t *testing.T) {
	loop := make([]*llm.ChatResponse, 0, 200)
	for i := 0; i < 60; i++ {
		loop = append(loop, toolCall("shell.exec", `{"argv":["go","version"]}`))
	}
	loop = append(loop, text("finished after 60 calls"))
	env := &scriptedEnv{replies: loop}
	res, err := turn(t, env, &runtime.TurnInput{Prompt: "p", Tools: granted, MaxToolCalls: 300})
	if err != nil || res.Summary != "finished after 60 calls" {
		t.Fatalf("a 300-call budget must allow 60 rounds: %+v %v", res, err)
	}

	never := make([]*llm.ChatResponse, 0, 50)
	for i := 0; i < 50; i++ {
		never = append(never, toolCall("shell.exec", `{"argv":["go","version"]}`))
	}
	env = &scriptedEnv{replies: never}
	_, err = turn(t, env, &runtime.TurnInput{Prompt: "p", Tools: granted, MaxToolCalls: 5})
	if !errors.Is(err, ErrOutOfRounds) || !strings.Contains(err.Error(), "40 rounds") {
		t.Fatalf("out of rounds must fail the turn: %v", err)
	}
}

type yieldingEnv struct{ scriptedEnv }

func (e *yieldingEnv) InvokeTool(ctx context.Context, name string, args json.RawMessage) (*tools.Result, error) {
	e.tools = append(e.tools, name)
	return &tools.Result{Content: "Waiting for human review", Yield: true}, nil
}
func TestDurableToolYieldEndsModelTurn(t *testing.T) {
	env := &yieldingEnv{scriptedEnv: scriptedEnv{replies: []*llm.ChatResponse{{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "one", Name: "review.request", Arguments: json.RawMessage(`{}`)}, {ID: "two", Name: "shell.exec", Arguments: json.RawMessage(`{}`)}}}}}}}
	_, err := New().Turn(context.Background(), &runtime.TurnInput{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(env.tools) != 1 || len(env.asked) != 1 {
		t.Fatalf("continued after yield: tools=%v calls=%d", env.tools, len(env.asked))
	}
}

func TestGatewayCallsAreNotReportedAgain(t *testing.T) {
	env := &scriptedEnv{replies: []*llm.ChatResponse{toolCall("git.branch", `{}`), toolCall("shell.exec", `{}`), text("Done")}}
	res, err := turn(t, env, nil)
	if err != nil || len(env.tools) != 2 || res.ToolCalls != 0 {
		t.Fatalf("gateway owns the count: result=%+v calls=%v err=%v", res, env.tools, err)
	}
}

func TestAnInvalidTextToolCallCanBeCorrected(t *testing.T) {
	env := &scriptedEnv{replies: []*llm.ChatResponse{
		text(`{"name": "filesystem__create", "parameters": {"path": "a.go"}}`),
		toolCall("git.branch", `{"name":"x"}`),
		text("Done."),
	}}
	res, err := turn(t, env, nil)
	if err != nil || res.Summary != "Done." || len(env.tools) != 1 {
		t.Fatalf("%+v %v %v", res, err, env.tools)
	}
	correction := env.asked[1].Messages[len(env.asked[1].Messages)-1]
	if correction.Role != llm.RoleUser || !strings.Contains(correction.Content, "git.branch, shell.exec") {
		t.Fatalf("%+v", correction)
	}
}

func TestSmallModelGuidanceCompactsSchemasAndRunsOneCallPerReply(t *testing.T) {
	tools := []llm.ToolSchema{{Name: "git.branch", Description: "Create a branch. Use it before committing anything.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"branch name"},"opts":{"type":"array","items":{"type":"object","description":"x","properties":{"k":{"type":"string","description":"y"}}}}},"required":["name"]}`)}}
	two := &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{
		{ID: "a", Name: "git.branch", Arguments: json.RawMessage(`{"name":"x"}`)},
		{ID: "b", Name: "git.branch", Arguments: json.RawMessage(`{"name":"y"}`)},
	}}, StopReason: "tool_calls"}
	env := &scriptedEnv{replies: []*llm.ChatResponse{two, text("ok")}}
	if _, err := New().Turn(context.Background(), &runtime.TurnInput{Prompt: "p", Tools: tools, ModelGuidance: llm.GuidanceSmall}, env); err != nil {
		t.Fatal(err)
	}
	offered := env.asked[0].Tools[0]
	if offered.Description != "Create a branch." || strings.Contains(string(offered.Parameters), "description") || !strings.Contains(string(offered.Parameters), `"required":["name"]`) {
		t.Fatalf("%+v %s", offered, offered.Parameters)
	}
	if len(env.tools) != 1 {
		t.Fatalf("one call per reply: %v", env.tools)
	}
	msgs := env.asked[1].Messages
	if last := msgs[len(msgs)-1]; last.ToolCallID != "b" || !strings.Contains(last.Content, "not run: call one tool per reply") {
		t.Fatalf("the skipped call must be answered: %+v", last)
	}
	// Standard guidance keeps full schemas and runs every call.
	env = &scriptedEnv{replies: []*llm.ChatResponse{two, text("ok")}}
	if _, err := New().Turn(context.Background(), &runtime.TurnInput{Prompt: "p", Tools: tools}, env); err != nil {
		t.Fatal(err)
	}
	if len(env.tools) != 2 || env.asked[0].Tools[0].Description != tools[0].Description {
		t.Fatalf("standard: %v", env.tools)
	}
}
