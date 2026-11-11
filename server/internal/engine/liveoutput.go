package engine

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

// maxOutputText bounds the text of one agent.output event, and the text all
// live events of one model call carry together.
const maxOutputText = 8000

// liveOutput publishes a streamed answer's text as it arrives (NAT-09):
// agent.output events {text, stream, partial: true} whose texts, joined in
// order, are the start of the answer. Events are throttled to one per
// LiveOutputInterval, the first published at once, and stop after
// maxOutputText bytes, so a long answer adds a bounded number of events to
// the log. Text received after the last event is not published: the
// runtime's Output of the whole answer, tagged with the same stream id,
// completes the line.
type liveOutput struct {
	t        *turnEnv
	ctx      context.Context
	id       string
	interval time.Duration
	full     strings.Builder
	pending  strings.Builder
	sent     int
	last     time.Time
}

// streamedText is the text of a live model call that succeeded, so the
// Output that repeats it can carry the stream id.
type streamedText struct {
	id, text string
}

func (t *turnEnv) newLiveOutput(ctx context.Context) *liveOutput {
	return &liveOutput{t: t, ctx: ctx, id: domain.NewID("stm"), interval: t.gr.e.opt.LiveOutputInterval}
}

// add receives a piece of the answer from the provider's stream.
func (l *liveOutput) add(text string) {
	l.full.WriteString(text)
	if l.sent >= maxOutputText {
		return
	}
	l.pending.WriteString(text)
	if !l.last.IsZero() && time.Since(l.last) < l.interval {
		return
	}
	// Leading whitespace waits for the first visible text.
	if l.sent == 0 && strings.TrimSpace(l.pending.String()) == "" {
		return
	}
	l.flush()
}

func (l *liveOutput) flush() {
	chunk := l.pending.String()
	if l.sent == 0 {
		chunk = strings.TrimLeft(chunk, " \t\r\n")
	}
	chunk = cutUTF8(chunk, maxOutputText-l.sent)
	l.pending.Reset()
	if chunk == "" {
		return
	}
	l.sent += len(chunk)
	l.last = time.Now()
	t := l.t
	t.gr.e.bus.Publish(l.ctx, domain.NewEvent(domain.EvAgentOutput, t.gr.g.Metadata.ID,
		map[string]any{"text": chunk, "stream": l.id, "partial": true}).
		WithNode(t.n.ID, t.r.index).WithExecution(t.x.ID))
}

// done ends the call. When it succeeded after showing text, the next Output
// with the answer's text completes the live line.
func (l *liveOutput) done(resp *llm.ChatResponse, err error) {
	t := l.t
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamed = nil
	if err == nil && resp != nil && l.sent > 0 {
		t.streamed = &streamedText{id: l.id, text: strings.TrimSpace(l.full.String())}
	}
}

// cutUTF8 returns at most n bytes of s without splitting a character.
func cutUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
