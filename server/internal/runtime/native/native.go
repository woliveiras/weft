// Package native is the built-in agent runtime: a tool-calling loop over
// any configured model provider. It needs no framework and no external
// process - a node with a prompt, a model and tool grants is a complete
// agent. Tools execute in the platform's gateway under the node's grants;
// the model never touches the host.
//
// Beyond granted tools the model gets five platform tools that need no
// grant because they only touch the agent's own turn: emit, state_get,
// state_set, plan.update and finish. For models without native tool calling the runtime
// also honours text sentinels (NO FURTHER WORK:, CONFIRM STOP:, BROADCAST:,
// and EMIT {...} lines), and runs a tool call the model wrote as a JSON
// answer instead of a structured call when it names an offered tool.
//
// A turn fails rather than "succeeding" without work: an empty answer with
// no tool call, an answer cut off by the output limit, a tool call written as
// text that cannot be run, and running out of model rounds are errors.
package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/tools"
)

type Runtime struct {
	// MaxRounds bounds model round-trips per turn (default 40). A node's
	// limits.maxToolCalls raises it: every round but the last calls a tool.
	MaxRounds int
}

// Turn errors, so operators can tell model problems from platform ones.
var (
	ErrEmptyAnswer    = errors.New("the model ended the turn without an answer or a tool call")
	ErrAnswerCutOff   = errors.New("the model's answer was cut off by its output limit")
	ErrToolCallAsText = errors.New("the model wrote a tool call as text instead of calling the tool; choose a model with tool calling (Integrations → Test connection)")
	ErrOutOfRounds    = errors.New("the turn used every model round without finishing")
)

func New() *Runtime { return &Runtime{MaxRounds: 40} }

func (r *Runtime) Kind() domain.RuntimeKind { return domain.RuntimeNative }

func (r *Runtime) Prepare(context.Context, runtime.ReplicaContext) error { return nil }
func (r *Runtime) Release(context.Context, runtime.ReplicaContext) error { return nil }

var platformTools = []llm.ToolSchema{
	{
		Name:        "emit",
		Description: "Send a message to downstream agents. The platform routes it by type, target and channel conditions.",
		Parameters: json.RawMessage(`{"type":"object","properties":{
			"type":{"type":"string","description":"message type, e.g. task.ready"},
			"payload":{"type":"object","description":"JSON object payload"},
			"target":{"type":"string","description":"optional node to send toward"},
			"channel":{"type":"string","description":"optional channel to force"}},
			"required":["type","payload"]}`),
	},
	{
		Name:        "state_get",
		Description: "Read a value from your durable memory (shared by replicas of this node).",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`),
	},
	{
		Name:        "state_set",
		Description: "Write a value to your durable memory for future turns.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"},"value":{"type":"string"}},"required":["key","value"]}`),
	},
	{
		Name:        "plan.update",
		Description: "Replace your working plan: a short checklist shown live to people following the task. Send every step each time.",
		Parameters: json.RawMessage(`{"type":"object","properties":{
			"items":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{
				"step":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","done"]}},
				"required":["step","status"]}}},"required":["items"]}`),
	},
	{
		Name:        "finish",
		Description: "End this turn. signal: \"\" (default), \"no_further_work\" (declare the job complete) or \"confirm_stop\" (confirm a previous declaration). broadcast: one line for ring neighbours.",
		Parameters: json.RawMessage(`{"type":"object","properties":{
			"summary":{"type":"string"},"signal":{"type":"string","enum":["","no_further_work","confirm_stop"]},
			"broadcast":{"type":"string"}}}`),
	},
}

func (r *Runtime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	res := &runtime.TurnResult{}
	toolset := append(append([]llm.ToolSchema(nil), in.Tools...), platformTools...)
	small := in.ModelGuidance == llm.GuidanceSmall
	if small {
		toolset = CompactSchemas(toolset)
	}
	conv := &conversation{task: llm.ChatMessage{Role: llm.RoleUser, Content: in.Prompt}}
	cm := newContextManager(ctx, env, in.System, toolset)
	var lastText string
	rounds := r.MaxRounds
	if rounds <= 0 {
		rounds = 40
	}
	if in.MaxToolCalls+1 > rounds {
		rounds = in.MaxToolCalls + 1
	}
	calls, corrections := 0, 0
	finished := false
	for round := 0; round < rounds && !finished; round++ {
		if err := cm.fit(ctx, conv); err != nil {
			return nil, err
		}
		resp, err := env.Chat(ctx, &llm.ChatRequest{System: in.System, Messages: conv.messages(), Tools: toolset})
		if err != nil {
			return nil, fmt.Errorf("model call: %w", err)
		}
		// TurnEnv.Chat accounts for gateway model usage, including delegated children.
		msg := resp.Message
		msg.Role = llm.RoleAssistant
		text := strings.TrimSpace(msg.Content)
		if len(msg.ToolCalls) == 0 && text != "" {
			recovered, looksLikeCall := TextToolCalls(text, toolset, round)
			switch {
			case len(recovered) > 0:
				msg.ToolCalls, msg.Content, text = recovered, "", ""
				env.Output(ctx, fmt.Sprintf("note: the model wrote %d tool call(s) as text instead of calling them; Weft ran them", len(recovered)))
			case looksLikeCall && corrections < maxCorrections:
				// Small models invent tool names; say what exists and let
				// them retry instead of failing the turn on the first slip.
				corrections++
				conv.add(msg)
				conv.add(llm.ChatMessage{Role: llm.RoleUser, Content: "That reply looks like a tool call but it cannot be run: the tool name is not offered or the arguments are not a JSON object. Call one of the offered tools with a JSON object of arguments: " + toolNames(toolset) + "."})
				env.Output(ctx, "note: the model wrote an invalid tool call as text; Weft asked it to use an offered tool")
				continue
			case looksLikeCall:
				return nil, fmt.Errorf("%w: %s", ErrToolCallAsText, truncate(text, 300))
			}
		}
		conv.add(msg)
		if text != "" {
			lastText = text
			env.Output(ctx, text)
		}
		if len(msg.ToolCalls) == 0 {
			switch {
			case resp.StopReason == "length":
				// A cut answer is incomplete (or a runaway generation).
				return nil, fmt.Errorf("%w (%s)", ErrAnswerCutOff, truncate(text, 200))
			case text == "" && calls == 0:
				return nil, ErrEmptyAnswer
			}
			finished = true
			break
		}
		for i, call := range msg.ToolCalls {
			if small && i > 0 {
				// Small models chain calls that depend on each other's
				// results; run one and say so, so the model waits.
				conv.add(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: "not run: call one tool per reply and wait for its result"})
				continue
			}
			out, done := r.call(ctx, env, res, call)
			calls++
			conv.add(llm.ChatMessage{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: cm.capResult(call.Name, out)})
			finished = finished || done
			if done {
				break
			}
		}
	}
	if !finished {
		return nil, fmt.Errorf("%w (%d rounds, %d tool calls); raise the agent's Max tool calls or split the task", ErrOutOfRounds, rounds, calls)
	}
	applySentinels(ctx, env, res, lastText)
	if res.Summary == "" {
		res.Summary = truncate(lastText, 1000)
	}
	return res, nil
}

// CompactSchemas shortens tool schemas for small models: the description
// keeps its first sentence and parameter descriptions are dropped (names,
// types, enums and required fields stay).
func CompactSchemas(ts []llm.ToolSchema) []llm.ToolSchema {
	out := make([]llm.ToolSchema, len(ts))
	for i, t := range ts {
		t.Description = firstSentence(t.Description)
		var schema map[string]any
		if json.Unmarshal(t.Parameters, &schema) == nil {
			stripDescriptions(schema)
			if b, err := json.Marshal(schema); err == nil {
				t.Parameters = b
			}
		}
		out[i] = t
	}
	return out
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

func stripDescriptions(v any) {
	switch x := v.(type) {
	case map[string]any:
		if props, ok := x["properties"].(map[string]any); ok {
			for _, p := range props {
				if pm, ok := p.(map[string]any); ok {
					delete(pm, "description")
					stripDescriptions(pm)
				}
			}
		}
		if items, ok := x["items"]; ok {
			if im, ok := items.(map[string]any); ok {
				delete(im, "description")
			}
			stripDescriptions(items)
		}
	case []any:
		for _, e := range x {
			stripDescriptions(e)
		}
	}
}

// maxCorrections bounds how often one turn is told that a tool call
// written as text could not be run before the turn fails.
const maxCorrections = 2

func toolNames(ts []llm.ToolSchema) string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// call executes one tool call; it returns the text for the model and
// whether the call ended the turn.
func (r *Runtime) call(ctx context.Context, env runtime.TurnEnv, res *runtime.TurnResult, call llm.ToolCall) (string, bool) {
	args := call.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	switch call.Name {
	case "emit":
		var em runtime.Emit
		if err := json.Unmarshal(args, &em); err != nil {
			return "error: " + err.Error(), false
		}
		if err := env.Emit(ctx, em); err != nil {
			return "error: " + err.Error(), false
		}
		env.Output(ctx, "→ emit "+em.Type)
		return "queued for delivery when the turn succeeds", false
	case "state_get":
		var a struct{ Key string }
		_ = json.Unmarshal(args, &a)
		v, err := env.StateGet(ctx, a.Key)
		if err != nil {
			return "(not set)", false
		}
		return string(v), false
	case "state_set":
		var a struct{ Key, Value string }
		_ = json.Unmarshal(args, &a)
		if err := env.StatePut(ctx, a.Key, []byte(a.Value)); err != nil {
			return "error: " + err.Error(), false
		}
		return "ok", false
	case "plan.update":
		return updatePlan(ctx, env, args), false
	case "finish":
		var a struct {
			Summary, Signal, Broadcast string
		}
		_ = json.Unmarshal(args, &a)
		res.Summary = a.Summary
		res.Signal = runtime.Signal(a.Signal)
		res.Broadcast = a.Broadcast
		return "turn finished", true
	}
	// InvokeTool records this attempt in the engine; do not report it twice.
	env.Output(ctx, "→ "+call.Name+" "+truncate(string(args), 300))
	out, err := env.InvokeTool(ctx, call.Name, args)
	switch {
	case errors.Is(err, tools.ErrDenied), errors.Is(err, tools.ErrUnknownTool):
		return "permission denied: " + call.Name + " is not granted to you", false
	case errors.Is(err, tools.ErrRejected):
		return "a human rejected this call: " + err.Error(), false
	case err != nil:
		return "error: " + err.Error(), false
	}
	content := out.Content
	if out.IsError {
		content = "error: " + content
	}
	if out.Truncated {
		content += "\n…(output truncated)"
	}
	return content, out.Yield
}

// updatePlan validates and records the agent's plan and returns it
// rendered, so the latest plan stays in the conversation (it is never elided).
func updatePlan(ctx context.Context, env runtime.TurnEnv, args json.RawMessage) string {
	var a struct {
		Items []runtime.PlanItem `json:"items"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "error: invalid plan: " + err.Error()
	}
	if len(a.Items) == 0 || len(a.Items) > 20 {
		return "error: a plan has 1 to 20 steps"
	}
	done, active := 0, 0
	var b strings.Builder
	for i := range a.Items {
		it := &a.Items[i]
		it.Step = strings.TrimSpace(it.Step)
		if it.Step == "" {
			return fmt.Sprintf("error: step %d is empty", i+1)
		}
		if len([]rune(it.Step)) > 200 {
			it.Step = string([]rune(it.Step)[:200]) + "…"
		}
		mark := "[ ]"
		switch it.Status {
		case "done":
			done++
			mark = "[x]"
		case "in_progress":
			active++
			mark = "[>]"
		case "pending":
		default:
			return fmt.Sprintf("error: step %d has status %q; use pending, in_progress or done", i+1, it.Status)
		}
		fmt.Fprintf(&b, "\n%s %s", mark, it.Step)
	}
	if active > 1 {
		return "error: only one step can be in_progress at a time"
	}
	if pe, ok := env.(runtime.PlanEnv); ok {
		pe.RecordPlan(ctx, a.Items)
	}
	summary := fmt.Sprintf("plan updated (%d/%d done)", done, len(a.Items))
	env.Output(ctx, summary)
	return summary + ":" + b.String()
}

var (
	reNoMore    = regexp.MustCompile(`(?im)^[\s*_#>]*NO FURTHER WORK:`)
	reConfirm   = regexp.MustCompile(`(?im)^[\s*_#>]*CONFIRM STOP:`)
	reBroadcast = regexp.MustCompile(`(?im)^[\s*_#>]*BROADCAST:\s*(.+)$`)
	reEmit      = regexp.MustCompile(`(?m)^EMIT\s+(\{.*\})\s*$`)
)

// applySentinels supports models without tool calling.
func applySentinels(ctx context.Context, env runtime.TurnEnv, res *runtime.TurnResult, text string) {
	if res.Signal == "" {
		switch {
		case reConfirm.MatchString(text):
			res.Signal = runtime.SignalConfirmStop
		case reNoMore.MatchString(text):
			res.Signal = runtime.SignalNoFurtherWork
		}
	}
	if res.Broadcast == "" {
		if m := reBroadcast.FindStringSubmatch(text); m != nil {
			res.Broadcast = strings.TrimSpace(m[1])
		}
	}
	for _, m := range reEmit.FindAllStringSubmatch(text, -1) {
		var em runtime.Emit
		if json.Unmarshal([]byte(m[1]), &em) == nil && em.Type != "" {
			_ = env.Emit(ctx, em)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TextToolCalls recognises a tool call a model wrote as its answer: a JSON
// object {"name": ..., "arguments": {...}} (or an array of them), optionally
// in a Markdown code fence, naming tools that were offered. It returns the
// calls to run, or looksLikeCall when the text has that shape but cannot be
// run (unknown tool, malformed arguments), so the turn fails explicitly.
func TextToolCalls(text string, offered []llm.ToolSchema, round int) (calls []llm.ToolCall, looksLikeCall bool) {
	body := strings.TrimSpace(text)
	if strings.HasPrefix(body, "```") {
		body = strings.TrimPrefix(body, "```")
		if i := strings.IndexByte(body, '\n'); i >= 0 {
			body = body[i+1:] // drop the language tag
		}
		body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "```"))
	}
	if !strings.HasPrefix(body, "{") && !strings.HasPrefix(body, "[") {
		return nil, false
	}
	type written struct {
		Name       string          `json:"name"`
		Arguments  json.RawMessage `json:"arguments"`
		Parameters json.RawMessage `json:"parameters"`
	}
	var list []written
	if strings.HasPrefix(body, "[") {
		if json.Unmarshal([]byte(body), &list) != nil {
			return nil, strings.Contains(body, `"name"`)
		}
	} else {
		var one written
		if json.Unmarshal([]byte(body), &one) != nil {
			return nil, strings.Contains(body, `"name"`) && strings.Contains(body, `"arguments"`)
		}
		list = []written{one}
	}
	names := map[string]bool{}
	for _, t := range offered {
		names[t.Name] = true
	}
	for i, w := range list {
		if w.Name == "" {
			return nil, len(w.Arguments) > 0 || len(w.Parameters) > 0
		}
		name := llm.PlatformToolName(w.Name)
		if !names[name] {
			return nil, true
		}
		args := w.Arguments
		if len(args) == 0 {
			args = w.Parameters
		}
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		args = llm.NormalizeArgs(args)
		var obj map[string]any
		if json.Unmarshal(args, &obj) != nil {
			return nil, true
		}
		if _, junk := obj["_raw"]; junk && len(obj) == 1 {
			return nil, true // arguments were not JSON
		}
		calls = append(calls, llm.ToolCall{ID: fmt.Sprintf("text_%d_%d", round, i), Name: name, Arguments: args})
	}
	return calls, true
}
