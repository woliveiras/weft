package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
)

// smallWindowProvider has a 4k context window. The agent reads a real
// workspace file through the tool gateway many times with long notes, so
// the native runtime must elide and compact to stay inside the window.
type smallWindowProvider struct {
	mu        sync.Mutex
	rounds    int
	compacts  []*llm.ChatRequest
	largest   int
	lostTask  bool
	sawSumary bool
}

const smallWindow = 4096

func (*smallWindowProvider) Name() string                                    { return "small" }
func (*smallWindowProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (*smallWindowProvider) ContextWindow() int                              { return smallWindow }
func (p *smallWindowProvider) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if est := llm.EstimateTokens(r); est > p.largest {
		p.largest = est
	}
	if r.Metadata["purpose"] == "context_compaction" {
		p.compacts = append(p.compacts, r)
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Read docs/big.txt several times; nothing changed yet."}, Usage: llm.Usage{InputTokens: 1000, OutputTokens: 20}}, nil
	}
	if r.Messages[0].Content == "" || !strings.Contains(r.Messages[0].Content, "Read docs/big.txt") {
		p.lostTask = true
	}
	for _, m := range r.Messages {
		p.sawSumary = p.sawSumary || strings.Contains(m.Content, "Read docs/big.txt several times")
	}
	p.rounds++
	if p.rounds > 16 {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: "done reading"}, Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}}, nil
	}
	args, _ := json.Marshal(map[string]any{"path": "docs/big.txt", "limit": 30})
	return &llm.ChatResponse{Message: llm.ChatMessage{
		Content:   strings.Repeat("Still checking the file for the setting. ", 20),
		ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("r%d", p.rounds), Name: "filesystem.read", Arguments: args}},
	}, Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}}, nil
}

func TestNativeTurnStaysInsideASmallContextWindow(t *testing.T) {
	dir := t.TempDir()
	p := &smallWindowProvider{}
	r := newRig(t, openStore(t, dir), memory.New(), dir, "context-instance", func(o *engine.Options) {
		o.Models.(*llm.Registry).Add(llm.ProviderConfig{Name: "small"}, p)
	})
	ws := filepath.Join(r.dir, "ws", "context")
	if err := os.MkdirAll(filepath.Join(ws, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("line %03d: setting value for the context window test", i))
	}
	if err := os.WriteFile(filepath.Join(ws, "docs", "big.txt"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	r.deploy("metadata: {name: context}\nspec:\n  nodes:\n    - id: reader\n      runtime: {kind: native}\n      prompt: Read docs/big.txt and report the setting\n      model: {provider: small, name: fixture}\n      loop: {mode: once}\n      isolation: {mode: process}\n      tools: [filesystem.read]\n      limits: {turnTimeout: 20s, maxToolCalls: 40}\n")

	var x *domain.Execution
	eventually(t, 15*time.Second, "reader turn finished", func() bool {
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "context", NodeID: "reader", Limit: 1})
		if len(xs) == 1 && xs[0].Status.Terminal() {
			x = xs[0]
			return true
		}
		return false
	})
	if x.Status != domain.ExecSucceeded {
		t.Fatalf("turn %s: %s", x.Status, x.Error)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lostTask {
		t.Fatal("a request lost the task message")
	}
	if p.largest >= smallWindow {
		t.Fatalf("largest request ~%d tokens does not fit the %d window", p.largest, smallWindow)
	}
	if len(p.compacts) == 0 || !p.sawSumary {
		t.Fatalf("compactions=%d summary seen=%v", len(p.compacts), p.sawSumary)
	}
	if c := p.compacts[0]; c.Metadata["execution"] != x.ID || len(c.Tools) != 0 {
		t.Fatalf("compaction did not go through the turn's gateway: %+v", c.Metadata)
	}
	// The compaction call is charged to the execution like any model call.
	if x.Usage.InputTokens < 1000 {
		t.Fatalf("compaction usage not recorded: %+v", x.Usage)
	}
	evs, err := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "context", Types: []domain.EventType{domain.EvAgentContext}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]bool{}
	for _, ev := range evs {
		var ce runtime.ContextEvent
		if err := json.Unmarshal(ev.Data, &ce); err != nil {
			t.Fatal(err)
		}
		if ce.Window != smallWindow || ce.Outcome != "ok" || ev.ExecutionID != x.ID {
			t.Fatalf("event %+v", ce)
		}
		ops[ce.Operation] = true
	}
	if !ops["elide"] || !ops["compact"] {
		t.Fatalf("expected elide and compact events, got %v", ops)
	}
}
