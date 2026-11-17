package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/tools"
)

// windowEnv is a TurnEnv with a context window: the agent reads files of a
// given size for a number of rounds, then answers. Compaction requests are
// answered by compact (or fail with compactErr).
type windowEnv struct {
	window     int
	reads      int    // tool rounds before the answer
	resultSize int    // characters per tool result
	thoughts   string // assistant text written with each call
	compactErr error
	agentReqs  []*llm.ChatRequest
	compacts   []*llm.ChatRequest
	events     []runtime.ContextEvent
	outputs    []string
	toolArgs   json.RawMessage
	toolName   string
}

func (e *windowEnv) ContextWindow(context.Context) int { return e.window }
func (e *windowEnv) RecordContext(_ context.Context, ev runtime.ContextEvent) {
	e.events = append(e.events, ev)
}

func (e *windowEnv) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if req.System == compactionSystem {
		e.compacts = append(e.compacts, req)
		if e.compactErr != nil {
			return nil, e.compactErr
		}
		return text("Read a.go: Add subtracts. Remaining: fix Add."), nil
	}
	cp := *req
	cp.Messages = append([]llm.ChatMessage(nil), req.Messages...)
	e.agentReqs = append(e.agentReqs, &cp)
	if len(e.agentReqs) > e.reads {
		return text("All read."), nil
	}
	name, args := "filesystem.read", json.RawMessage(fmt.Sprintf(`{"path":"src/f%d.go"}`, len(e.agentReqs)))
	if e.toolName != "" {
		name, args = e.toolName, e.toolArgs
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: e.thoughts, ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("c%d", len(e.agentReqs)), Name: name, Arguments: args}}}, StopReason: "tool_calls"}, nil
}

func (e *windowEnv) InvokeTool(_ context.Context, name string, args json.RawMessage) (*tools.Result, error) {
	return &tools.Result{Content: strings.Repeat("x", e.resultSize-1) + "\n"}, nil
}
func (e *windowEnv) Emit(context.Context, runtime.Emit) error { return nil }
func (e *windowEnv) StateGet(context.Context, string) ([]byte, error) {
	return nil, errors.New("unset")
}
func (e *windowEnv) StatePut(context.Context, string, []byte) error { return nil }
func (e *windowEnv) Output(_ context.Context, t string)             { e.outputs = append(e.outputs, t) }
func (e *windowEnv) AskHuman(context.Context, string) (string, error) {
	return "", nil
}

var readTool = []llm.ToolSchema{{Name: "filesystem.read", Parameters: json.RawMessage(`{"type":"object"}`)}}

func runWindow(t *testing.T, env *windowEnv) (*runtime.TurnResult, error) {
	t.Helper()
	return New().Turn(context.Background(), &runtime.TurnInput{Prompt: "TASK: fix the bug in src", System: "SYSTEM", Tools: readTool, MaxToolCalls: 60}, env)
}

func TestBudgetFollowsTheWindow(t *testing.T) {
	b := newBudget(32768)
	if b.window != 32768 || b.limit != 32768-4096 || b.high >= b.limit || b.low >= b.high || b.resultChars <= 0 {
		t.Fatalf("%+v", b)
	}
	if d := newBudget(0); d.window != DefaultContextWindow {
		t.Fatalf("unknown window: %+v", d)
	}
	if big := newBudget(1 << 20); big.limit != MaxWorkingTokens {
		t.Fatalf("large windows are capped: %+v", big)
	}
}

func TestOlderResultsAreElidedAndEveryRequestFits(t *testing.T) {
	env := &windowEnv{window: 4096, reads: 12, resultSize: 2000}
	res, err := runWindow(t, env)
	if err != nil || res.Summary != "All read." {
		t.Fatalf("%+v %v", res, err)
	}
	b := newBudget(env.window)
	for i, req := range env.agentReqs {
		if est := llm.EstimateTokens(req); est > b.limit {
			t.Fatalf("request %d: ~%d tokens over the limit %d", i, est, b.limit)
		}
		if req.Messages[0].Content != "TASK: fix the bug in src" || req.System != "SYSTEM" {
			t.Fatalf("request %d lost the task or system prompt: %+v", i, req.Messages[0])
		}
	}
	last := env.agentReqs[len(env.agentReqs)-1].Messages
	if latest := last[len(last)-1]; latest.Role != llm.RoleTool || strings.HasPrefix(latest.Content, elidedMark) {
		t.Fatalf("latest result must stay whole: %q", truncate(latest.Content, 80))
	}
	receipts := 0
	for _, m := range last {
		if strings.HasPrefix(m.Content, elidedMark) {
			receipts++
			if !strings.Contains(m.Content, "filesystem.read path=src/f") || !strings.Contains(m.Content, "1 lines (2000 characters)") {
				t.Fatalf("receipt does not say what was elided: %q", m.Content)
			}
		}
	}
	if receipts == 0 || len(env.events) == 0 || env.events[0].Operation != "elide" || env.events[0].TokensAfter >= env.events[0].TokensBefore {
		t.Fatalf("receipts=%d events=%+v", receipts, env.events)
	}
	if len(env.compacts) != 0 {
		t.Fatalf("elision was enough; no compaction expected, got %d", len(env.compacts))
	}
}

func TestSmallConversationsAreUntouched(t *testing.T) {
	env := &windowEnv{window: 32768, reads: 3, resultSize: 500}
	if _, err := runWindow(t, env); err != nil {
		t.Fatal(err)
	}
	if len(env.events) != 0 || len(env.compacts) != 0 {
		t.Fatalf("%+v", env.events)
	}
}

func TestCompactionSummarisesEarlierRoundsThroughTheModel(t *testing.T) {
	// Long assistant notes cannot be elided, so only a summary helps.
	env := &windowEnv{window: 4096, reads: 14, resultSize: 600, thoughts: strings.Repeat("thinking about the code. ", 40)}
	res, err := runWindow(t, env)
	if err != nil || res.Summary != "All read." {
		t.Fatalf("%+v %v", res, err)
	}
	if len(env.compacts) == 0 {
		t.Fatal("expected a compaction call")
	}
	c := env.compacts[0]
	if len(c.Tools) != 0 || c.MaxTokens != compactionAnswerTokens || c.Metadata["purpose"] != "context_compaction" ||
		!strings.Contains(c.Messages[0].Content, "TASK: fix the bug") || !strings.Contains(c.Messages[0].Content, "Agent called filesystem.read") {
		t.Fatalf("compaction request: %+v", c)
	}
	var compacted bool
	for _, ev := range env.events {
		compacted = compacted || (ev.Operation == "compact" && ev.Outcome == "ok" && ev.TokensAfter < ev.TokensBefore && ev.Window == 4096)
	}
	if !compacted {
		t.Fatalf("events: %+v", env.events)
	}
	last := env.agentReqs[len(env.agentReqs)-1]
	if last.Messages[0].Content != "TASK: fix the bug in src" || !strings.HasPrefix(last.Messages[1].Content, summaryHeader) ||
		!strings.Contains(last.Messages[1].Content, "Remaining: fix Add.") {
		t.Fatalf("summary not placed after the task: %+v", last.Messages[:2])
	}
	if est := llm.EstimateTokens(last); est > newBudget(4096).limit {
		t.Fatalf("~%d tokens after compaction", est)
	}
}

func TestFailedCompactionContinuesOnlyWhileTheConversationFits(t *testing.T) {
	env := &windowEnv{window: 4096, reads: 20, resultSize: 600, thoughts: strings.Repeat("thinking about the code. ", 40), compactErr: errors.New("model offline")}
	_, err := runWindow(t, env)
	if err == nil || !strings.Contains(err.Error(), "model offline") || !strings.Contains(err.Error(), "compact the turn's conversation") {
		t.Fatalf("expected the compaction failure once over the limit, got %v", err)
	}
	var failed bool
	for _, ev := range env.events {
		failed = failed || (ev.Operation == "compact" && ev.Outcome == "failed" && strings.Contains(ev.Error, "model offline"))
	}
	if !failed {
		t.Fatalf("events: %+v", env.events)
	}
	var noted bool
	for _, o := range env.outputs {
		noted = noted || strings.Contains(o, "compaction failed, continuing")
	}
	if !noted {
		t.Fatalf("a failed compaction under the hard limit must be reported and the turn continue: %v", env.outputs)
	}
}

func TestCancelledCompactionStopsTheTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &contextManager{b: newBudget(4096), env: &windowEnv{compactErr: context.Canceled}}
	c := &conversation{task: llm.ChatMessage{Role: llm.RoleUser, Content: "t"}}
	for i := 0; i < 3; i++ {
		c.add(llm.ChatMessage{Role: llm.RoleAssistant, Content: "x"})
	}
	if err := m.compact(ctx, c, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}

func TestLargeResultsAreCappedWithAPagingHint(t *testing.T) {
	m := &contextManager{b: newBudget(4096)}
	big := strings.Repeat("日本語", 4000)
	out := m.capResult("filesystem.read", big)
	if len(out) >= len(big) || !strings.Contains(out, "read the rest with offset/limit") || !strings.Contains(out, fmt.Sprintf("of %d characters", len(big))) {
		t.Fatalf("%q", truncate(out, 120))
	}
	if !utf8.ValidString(out) {
		t.Fatal("cap split a rune")
	}
	if small := m.capResult("shell.exec", "ok"); small != "ok" {
		t.Fatal(small)
	}
	if !strings.Contains(m.capResult("code.map", big), "smaller maxTokens") {
		t.Fatal("code.map results must suggest a narrower map")
	}
	if !strings.Contains(m.capResult("shell.exec", big), "filter the command output") {
		t.Fatal("shell hint")
	}
}

func TestLatestPlanAndShortResultsAreNeverElided(t *testing.T) {
	m := &contextManager{b: newBudget(1024), system: "s"}
	c := &conversation{task: llm.ChatMessage{Role: llm.RoleUser, Content: "task"}}
	long := strings.Repeat("y", 3000)
	for i := 0; i < 5; i++ {
		id := fmt.Sprint(i)
		c.add(llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "filesystem.write", Arguments: json.RawMessage(`{"path":"a","content":"` + long + `"}`)}}})
		c.add(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: id, Name: "filesystem.stat", Content: "short"})
		if i == 1 || i == 0 {
			c.add(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: id, Name: "plan.update", Content: fmt.Sprintf("plan %d %s", i, long)})
		}
	}
	m.elide(c)
	var plans []string
	for _, msg := range c.history {
		if msg.Name == "filesystem.stat" && msg.Content != "short" {
			t.Fatalf("short result elided: %q", msg.Content)
		}
		if msg.Name == "plan.update" {
			plans = append(plans, truncate(msg.Content, 20))
		}
		for _, tc := range msg.ToolCalls {
			if !json.Valid(tc.Arguments) {
				t.Fatalf("invalid args %s", tc.Arguments)
			}
		}
	}
	if len(plans) != 2 || !strings.HasPrefix(plans[0], elidedMark) || !strings.HasPrefix(plans[1], "plan 1") {
		t.Fatalf("only the latest plan must stay: %v", plans)
	}
	// Long arguments of older calls are shortened, the latest kept.
	if strings.Contains(string(c.history[0].ToolCalls[0].Arguments), long) {
		t.Fatal("old long arguments were kept")
	}
	if !strings.Contains(string(c.history[len(c.history)-2].ToolCalls[0].Arguments), long) {
		t.Fatal("latest arguments were shortened")
	}
}

func TestCompactionKeepsTheLatestPlan(t *testing.T) {
	env := &windowEnv{}
	m := &contextManager{b: newBudget(4096), env: env, system: "s"}
	c := &conversation{task: llm.ChatMessage{Role: llm.RoleUser, Content: "task"}}
	c.add(llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "p", Name: "plan.update"}}})
	c.add(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: "p", Name: "plan.update", Content: "plan updated (0/2 done):\n[>] fix Add\n[ ] test"})
	c.add(llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "r", Name: "filesystem.read"}}})
	c.add(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: "r", Name: "filesystem.read", Content: "code"})
	if err := m.compact(context.Background(), c, 100); err != nil {
		t.Fatal(err)
	}
	msgs := c.messages()
	if len(msgs) != 4 || !strings.Contains(msgs[1].Content, "[>] fix Add") || msgs[2].ToolCalls[0].ID != "r" {
		t.Fatalf("%+v", msgs)
	}
}

func TestRuntimesWithoutContextEnvUseTheDefaultWindow(t *testing.T) {
	env := &scriptedEnv{replies: []*llm.ChatResponse{toolCall("git.branch", `{"name":"x"}`), text("ok")}}
	if _, err := turn(t, env, nil); err != nil {
		t.Fatal(err)
	}
	m := newContextManager(context.Background(), env, "", nil)
	if m.b.window != DefaultContextWindow || m.ctxEnv != nil {
		t.Fatalf("%+v", m.b)
	}
}

func TestTranscriptKeepsItsEndsWhenTooLong(t *testing.T) {
	region := []llm.ChatMessage{
		{Role: llm.RoleAssistant, Content: "FIRST"},
		{Role: llm.RoleTool, Name: "filesystem.read", Content: strings.Repeat("m", 5000)},
		{Role: llm.RoleAssistant, Content: "LAST"},
	}
	s := renderTranscript("previous", region, 1000)
	if len(s) > 1100 || !strings.Contains(s, "Earlier summary:\nprevious") || !strings.Contains(s, "LAST") || !strings.Contains(s, "middle of the notes omitted") {
		t.Fatalf("%q", s)
	}
}
