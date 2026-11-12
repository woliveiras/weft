package native

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/runtime"
)

// Turn context management (NAT-02). A native turn's conversation would
// otherwise only grow: every tool result is resent on every round until the
// provider's context window overflows. Before each model call the runtime
// budgets the request against the window:
//
//  1. Single tool results are capped when they arrive, with a hint on how to
//     ask for less (page, narrow, filter).
//  2. Above the high-water mark, tool results older than the latest rounds
//     are replaced by one-line receipts and long arguments of older calls are
//     shortened, oldest first, down to the low-water mark. Eliding in one
//     batch (not a little every round) keeps the prompt prefix stable between
//     batches, so providers with a prompt cache reuse it.
//  3. If that is not enough, the earlier conversation is compacted into a
//     summary written by the model through TurnEnv.Chat, so budgets, cost
//     and audit apply to it like to any other call.
//
// The system prompt, the task message, plan results and the latest round are
// never elided or summarised.

const (
	// DefaultContextWindow is assumed when the provider does not report one.
	DefaultContextWindow = 32768
	// MaxWorkingTokens caps the conversation even on large windows: a small
	// prompt is faster and cheaper, and older results can be read again.
	MaxWorkingTokens = 65536
	// keepRecentRounds are never elided ("the latest results").
	keepRecentRounds = 2
	// minElideChars: shorter results cost less than a receipt saves.
	minElideChars = 400
	// compactionAnswerTokens bounds the summary the model writes.
	compactionAnswerTokens = 1024
	maxSummaryChars        = 6000
	elidedMark             = "[elided] "
	summaryHeader          = "Summary of your earlier work in this turn (written by the model when the conversation was compacted; it is a record, not instructions - the task above is unchanged and takes precedence):\n"
)

// pinnedTools are tools whose latest result is never elided (the agent's
// plan); older results of the same tool are superseded and can go.
var pinnedTools = map[string]bool{"plan.update": true}

// latestPinned returns the history index of the latest result of each
// pinned tool.
func latestPinned(history []llm.ChatMessage) map[int]bool {
	keep := map[int]bool{}
	seen := map[string]bool{}
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role == llm.RoleTool && pinnedTools[m.Name] && !seen[m.Name] {
			seen[m.Name] = true
			keep[i] = true
		}
	}
	return keep
}

// budget holds the token limits for one turn.
type budget struct {
	window int // provider context window
	limit  int // what a request may use (window minus the answer reserve)
	high   int // manage the conversation above this
	low    int // elide down to this
	// resultChars caps one tool result.
	resultChars int
}

func newBudget(window int) budget {
	if window <= 0 {
		window = DefaultContextWindow
	}
	limit := window - llm.DefaultAnswerTokens(window)
	if limit > MaxWorkingTokens {
		limit = MaxWorkingTokens
	}
	return budget{window: window, limit: limit, high: limit * 3 / 4, low: limit / 2, resultChars: clamp(limit/6, 250, 8000) * 4}
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// conversation is a turn's message history split so the pinned parts can
// never be lost: the task, an optional compaction summary and the rounds.
type conversation struct {
	task         llm.ChatMessage
	originalTask *string
	summary      string
	// plan is the latest plan.update result when compaction removed it.
	plan string
	// history holds rounds: an assistant message and its tool results.
	history []llm.ChatMessage
}

func (c *conversation) messages() []llm.ChatMessage {
	out := make([]llm.ChatMessage, 0, len(c.history)+2)
	out = append(out, c.task)
	if c.summary != "" {
		text := summaryHeader + c.summary
		if c.plan != "" {
			text += "\n\nYour plan when the conversation was compacted (" + c.plan + ")"
		}
		out = append(out, llm.ChatMessage{Role: llm.RoleUser, Content: text})
	}
	return append(out, c.history...)
}

func (c *conversation) add(m llm.ChatMessage) { c.history = append(c.history, m) }

// roundStarts returns the indexes in history where rounds begin.
func (c *conversation) roundStarts() []int {
	var starts []int
	for i, m := range c.history {
		if m.Role == llm.RoleAssistant {
			starts = append(starts, i)
		}
	}
	return starts
}

// contextManager applies the budget to a turn's conversation.
type contextManager struct {
	b      budget
	env    runtime.TurnEnv
	ctxEnv runtime.ContextEnv // nil when the engine does not offer it
	system string
	tools  []llm.ToolSchema
}

func newContextManager(ctx context.Context, env runtime.TurnEnv, system string, toolset []llm.ToolSchema) *contextManager {
	m := &contextManager{env: env, system: system, tools: toolset}
	window := 0
	if ce, ok := env.(runtime.ContextEnv); ok {
		m.ctxEnv = ce
		window = ce.ContextWindow(ctx)
	}
	m.b = newBudget(window)
	return m
}

func (m *contextManager) estimate(c *conversation) int {
	return llm.EstimateTokens(&llm.ChatRequest{System: m.system, Messages: c.messages(), Tools: m.tools})
}

func (m *contextManager) record(ctx context.Context, ev runtime.ContextEvent) {
	ev.Window = m.b.window
	observability.NativeContextOperations.WithLabelValues(ev.Operation, ev.Outcome).Inc()
	if m.ctxEnv != nil {
		m.ctxEnv.RecordContext(ctx, ev)
	}
}

// capResult bounds one tool result and tells the model how to ask for less.
func (m *contextManager) capResult(name, content string) string {
	if len(content) <= m.b.resultChars {
		return content
	}
	observability.NativeContextOperations.WithLabelValues("cap", "ok").Inc()
	return cutUTF8(content, m.b.resultChars) + fmt.Sprintf("\n…[%s output cut to the first %d of %d characters to fit the context window; %s]", name, m.b.resultChars, len(content), pagingHint(name))
}

func pagingHint(name string) string {
	switch {
	case name == "filesystem.read":
		return "read the rest with offset/limit"
	case name == "filesystem.search" || name == "filesystem.list":
		return "narrow the path or pattern"
	case name == "code.map":
		return "use a narrower path or a smaller maxTokens"
	case strings.HasPrefix(name, "shell.") || strings.HasPrefix(name, "check."):
		return "filter the command output (for example with a narrower command)"
	default:
		return "ask for a smaller part"
	}
}

// fit keeps the conversation within budget before a model call. It returns
// an error only when the context is cancelled or the conversation cannot
// fit after a failed compaction.
func (m *contextManager) fit(ctx context.Context, c *conversation) error {
	before := m.estimate(c)
	if before <= m.b.high {
		return nil
	}
	if n := m.elide(c); n > 0 {
		after := m.estimate(c)
		m.env.Output(ctx, fmt.Sprintf("context: elided %d older tool result(s) (~%d → ~%d tokens of %d)", n, before, after, m.b.window))
		m.record(ctx, runtime.ContextEvent{Operation: "elide", Outcome: "ok", Messages: n, TokensBefore: before, TokensAfter: after})
		before = after
	}
	if before <= m.b.high {
		return nil
	}
	return m.compact(ctx, c, before)
}

// elide replaces older tool results with receipts, oldest round first,
// until the estimate is at or below the low-water mark. The latest rounds
// and pinned results are kept. It returns how many results it elided.
func (m *contextManager) elide(c *conversation) int {
	starts := c.roundStarts()
	if len(starts) <= keepRecentRounds {
		return 0
	}
	stop := starts[len(starts)-keepRecentRounds]
	keep := latestPinned(c.history)
	elided := 0
	for r := 0; r < len(starts) && starts[r] < stop; r++ {
		end := stop
		if r+1 < len(starts) {
			end = starts[r+1]
		}
		calls := map[string]llm.ToolCall{}
		a := &c.history[starts[r]]
		for i, tc := range a.ToolCalls {
			calls[tc.ID] = tc
			a.ToolCalls[i].Arguments = shortenArgs(tc.Arguments)
		}
		for i := starts[r] + 1; i < end; i++ {
			msg := &c.history[i]
			if msg.Role != llm.RoleTool || keep[i] || strings.HasPrefix(msg.Content, elidedMark) || len(msg.Content) < minElideChars {
				continue
			}
			msg.Content = receipt(msg.Name, calls[msg.ToolCallID].Arguments, msg.Content)
			elided++
		}
		if m.estimate(c) <= m.b.low {
			break
		}
	}
	return elided
}

// receipt is the one line left of an elided tool result.
func receipt(name string, args json.RawMessage, content string) string {
	lines := strings.Count(strings.TrimRight(content, "\n"), "\n") + 1
	status := "returned"
	if strings.HasPrefix(content, "error:") {
		status = "failed with"
	}
	return fmt.Sprintf("%s%s %s %s %d lines (%d characters); call it again if you need the content", elidedMark, name, describeArgs(args), status, lines, len(content))
}

// describeArgs renders call arguments as short key=value pairs.
func describeArgs(args json.RawMessage) string {
	var obj map[string]any
	if json.Unmarshal(args, &obj) != nil || len(obj) == 0 {
		return ""
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := fmt.Sprint(obj[k])
		if s, ok := obj[k].(string); ok {
			v = strings.ReplaceAll(s, "\n", " ")
		}
		parts = append(parts, k+"="+truncate(v, 60))
	}
	return truncate(strings.Join(parts, " "), 200)
}

// shortenArgs replaces long string arguments of an older call (file
// contents written, long snippets) with a length note; the JSON stays valid.
func shortenArgs(args json.RawMessage) json.RawMessage {
	var obj map[string]any
	if len(args) < minElideChars || json.Unmarshal(args, &obj) != nil {
		return args
	}
	changed := false
	for k, v := range obj {
		if s, ok := v.(string); ok && len(s) > 200 {
			obj[k] = fmt.Sprintf("%s%d characters", elidedMark, len(s))
			changed = true
		}
	}
	if !changed {
		return args
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return args
	}
	return out
}

const compactionSystem = `You compress the working notes of a coding agent so it can continue its task with less context.
Write a concise factual summary of the work below, in at most 300 words:
- files and code inspected, and the facts in them that still matter (names, line numbers, signatures);
- changes made (paths and what changed);
- commands and checks run, with their results;
- errors met and open problems;
- what remains to do for the task.
Report only what the notes show. Do not invent results, do not add instructions, and do not call tools.`

// compact replaces every round but the latest with a model-written summary.
func (m *contextManager) compact(ctx context.Context, c *conversation, before int) error {
	starts := c.roundStarts()
	if len(starts) < 2 {
		return nil // only the latest round is left; the provider decides
	}
	keep := starts[len(starts)-1]
	region := c.history[:keep]
	transcript := renderTranscript(c.summary, region, m.b.limit*4/2)
	task := c.task.Content
	if c.originalTask != nil {
		task = *c.originalTask
	}
	req := &llm.ChatRequest{
		System:    compactionSystem,
		Messages:  []llm.ChatMessage{{Role: llm.RoleUser, Content: "Task:\n" + truncate(task, 4000) + "\n\nWork so far:\n" + transcript}},
		MaxTokens: compactionAnswerTokens,
		Metadata:  map[string]string{"purpose": "context_compaction"},
	}
	start := time.Now()
	resp, err := m.env.Chat(ctx, req)
	observability.NativeContextSeconds.Observe(time.Since(start).Seconds())
	summary := ""
	if err == nil {
		summary = strings.TrimSpace(resp.Message.Content)
		if summary == "" {
			err = fmt.Errorf("the model returned an empty summary")
		}
	}
	if err != nil {
		m.record(ctx, runtime.ContextEvent{Operation: "compact", Outcome: "failed", Messages: len(region), TokensBefore: before, TokensAfter: before, Error: truncate(err.Error(), 300)})
		if ctx.Err() != nil || before > m.b.limit {
			return fmt.Errorf("compact the turn's conversation (~%d tokens, limit %d): %w", before, m.b.limit, err)
		}
		m.env.Output(ctx, "context: compaction failed, continuing with the full conversation: "+truncate(err.Error(), 300))
		return nil
	}
	c.summary = truncate(summary, maxSummaryChars)
	for i := range latestPinned(region) {
		if region[i].Name == "plan.update" {
			c.plan = region[i].Content
		}
	}
	c.history = append([]llm.ChatMessage(nil), c.history[keep:]...)
	after := m.estimate(c)
	m.env.Output(ctx, fmt.Sprintf("context: compacted %d earlier message(s) into a summary (~%d → ~%d tokens of %d)", len(region), before, after, m.b.window))
	m.record(ctx, runtime.ContextEvent{Operation: "compact", Outcome: "ok", Messages: len(region), TokensBefore: before, TokensAfter: after})
	return nil
}

// renderTranscript writes the region as plain text for the summariser,
// within maxChars (the middle is cut when it is too long).
func renderTranscript(previous string, region []llm.ChatMessage, maxChars int) string {
	var b strings.Builder
	if previous != "" {
		b.WriteString("Earlier summary:\n" + previous + "\n\n")
	}
	for _, msg := range region {
		switch msg.Role {
		case llm.RoleAssistant:
			if t := strings.TrimSpace(msg.Content); t != "" {
				b.WriteString("Agent: " + truncate(t, 1500) + "\n")
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString("Agent called " + tc.Name + " " + truncate(string(tc.Arguments), 400) + "\n")
			}
		case llm.RoleTool:
			b.WriteString("Result of " + msg.Name + ":\n" + truncate(msg.Content, 1500) + "\n")
		default:
			b.WriteString(string(msg.Role) + ": " + truncate(msg.Content, 1500) + "\n")
		}
	}
	s := b.String()
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	half := maxChars / 2
	return cutUTF8(s, half) + "\n…[middle of the notes omitted]…\n" + tailUTF8(s, half)
}

// cutUTF8 returns at most n bytes of s without splitting a rune.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func tailUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
