package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llm/llmtest"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/storage"
)

const liveGraph = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: live}
spec:
  nodes:
    - id: writer
      prompt: Say hello.
  channels:
    - {id: in, to: [writer]}
`

// wholeOnly hides a provider's streaming, so the same backend answers the
// way every provider did before NAT-09.
type wholeOnly struct{ llm.Provider }

// liveRun runs one native turn against a fake Ollama that streams chunks,
// and returns the writer's first finished execution, its agent.output
// events and the calls the gateway observed (accounting).
type liveRun struct {
	exec   *domain.Execution
	output []map[string]any
	calls  []llm.Call
}

func runLive(t *testing.T, chunks []string, stream bool, interval time.Duration) liveRun {
	t.Helper()
	srv := llmtest.NewStream(t, "application/x-ndjson", chunks...)
	p, err := llm.New(llm.ProviderConfig{Name: "ol", Type: "ollama", BaseURL: srv.URL, DefaultModel: "llama3", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !stream {
		p = wholeOnly{p}
	}
	models, _ := llm.NewRegistry(nil, "")
	models.Add(llm.ProviderConfig{Name: "ol", Type: "ollama", DefaultModel: "llama3"}, p)
	var mu sync.Mutex
	var calls []llm.Call
	models.SetObserver(func(_ context.Context, c llm.Call) {
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
	})
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "i1", func(o *engine.Options) {
		o.Models = models
		o.LiveOutputInterval = interval
	})
	r.deploy(liveGraph)
	r.inject("live", "in", "doc", map[string]string{"text": "hi"})
	var exec *domain.Execution
	eventually(t, 15*time.Second, "the writer's turn finished", func() bool {
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "live", NodeID: "writer", Limit: 10})
		for _, x := range xs {
			if x.Status == domain.ExecSucceeded || x.Status == domain.ExecFailed {
				exec = x
				return true
			}
		}
		return false
	})
	evs, err := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "live", Types: []domain.EventType{domain.EvAgentOutput}, Ascending: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	out := liveRun{exec: exec}
	for _, ev := range evs {
		if ev.ExecutionID != exec.ID {
			continue
		}
		var d map[string]any
		_ = json.Unmarshal(ev.Data, &d)
		out.output = append(out.output, d)
	}
	mu.Lock()
	out.calls = append(out.calls, calls...)
	mu.Unlock()
	if len(out.calls) == 0 {
		t.Fatal("the gateway observed no model call")
	}
	return out
}

func ndjson(pieces ...string) []string {
	var out []string
	for _, p := range pieces {
		b, _ := json.Marshal(map[string]any{"model": "llama3", "message": map[string]any{"role": "assistant", "content": p}, "done": false})
		out = append(out, string(b)+"\n")
	}
	return append(out, `{"model":"llama3","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":42,"eval_count":7}`+"\n")
}

// NAT-09: a native turn shows the model's text while it streams, as
// agent.output events that the final output completes, and its accounting
// is the same as without streaming.
func TestNativeTurnStreamsLiveOutputWithUnchangedAccounting(t *testing.T) {
	chunks := ndjson("\n", "Hello", ", wor", "ld!")
	live := runLive(t, chunks, true, time.Nanosecond)
	// The same answer, whole, from a backend that does not stream.
	whole := runLive(t, []string{`{"model":"llama3","message":{"role":"assistant","content":"\nHello, world!"},"done":true,"done_reason":"stop","prompt_eval_count":42,"eval_count":7}`}, false, time.Nanosecond)

	for name, run := range map[string]liveRun{"streamed": live, "whole": whole} {
		if run.exec.Status != domain.ExecSucceeded {
			t.Fatalf("%s turn: %s %s", name, run.exec.Status, run.exec.Error)
		}
	}
	// Live: one partial event per visible piece, in order, then the final
	// text tagged with the same stream id.
	if len(live.output) != 4 {
		t.Fatalf("streamed output events = %+v", live.output)
	}
	stream, _ := live.output[0]["stream"].(string)
	var joined string
	for _, d := range live.output[:3] {
		if d["partial"] != true || d["stream"] != stream {
			t.Fatalf("live event = %+v", d)
		}
		joined += d["text"].(string)
	}
	final := live.output[3]
	if stream == "" || joined != "Hello, world!" || final["text"] != "Hello, world!" || final["stream"] != stream || final["partial"] != nil {
		t.Fatalf("stream %q joined %q final %+v", stream, joined, final)
	}
	// Whole: the one output event of before.
	if len(whole.output) != 1 || whole.output[0]["text"] != "Hello, world!" || whole.output[0]["stream"] != nil {
		t.Fatalf("whole output events = %+v", whole.output)
	}
	// Accounting: the same usage on the execution and the same observed call.
	if live.exec.Usage != whole.exec.Usage || live.exec.Usage.InputTokens != 42 || live.exec.Usage.OutputTokens != 7 {
		t.Fatalf("execution usage streamed %+v whole %+v", live.exec.Usage, whole.exec.Usage)
	}
	lc, wc := live.calls[0], whole.calls[0]
	if lc.Usage != wc.Usage || lc.Estimated || wc.Estimated || lc.Model != wc.Model || lc.Provider != wc.Provider ||
		lc.Metadata["execution"] != live.exec.ID || lc.Err != nil {
		t.Fatalf("observed calls streamed %+v whole %+v", lc, wc)
	}
}

// Live events are throttled: with a long interval only the first piece is
// shown before the final output.
func TestLiveOutputIsThrottled(t *testing.T) {
	run := runLive(t, ndjson("one ", "two ", "three"), true, time.Hour)
	if len(run.output) != 2 || run.output[0]["text"] != "one " || run.output[0]["partial"] != true ||
		run.output[1]["text"] != "one two three" || run.output[1]["stream"] != run.output[0]["stream"] {
		t.Fatalf("output events = %+v", run.output)
	}
}

// A stream that fails midway fails the turn: the text shown live is never
// completed as an answer, and the call is accounted as a failure.
func TestFailedStreamFailsTheTurn(t *testing.T) {
	chunks := []string{
		`{"model":"llama3","message":{"role":"assistant","content":"Hello"},"done":false}` + "\n",
		`{"error":"model runner crashed"}` + "\n",
	}
	run := runLive(t, chunks, true, time.Nanosecond)
	if run.exec.Status != domain.ExecFailed || !strings.Contains(run.exec.Error, "model runner crashed") {
		t.Fatalf("execution %s %q", run.exec.Status, run.exec.Error)
	}
	for _, d := range run.output {
		if d["partial"] != true {
			t.Fatalf("a failed stream must not produce a final output: %+v", run.output)
		}
	}
	if c := run.calls[0]; c.Err == nil || !c.Estimated {
		t.Fatalf("observed call %+v", c)
	}
}
