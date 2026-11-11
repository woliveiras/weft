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
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/storage"
)

// planProvider updates the plan twice, then answers.
type planProvider struct {
	mu     sync.Mutex
	system string
	calls  int
}

func (*planProvider) Name() string                                    { return "planner" }
func (*planProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *planProvider) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.system = r.System
	p.calls++
	plans := []string{
		`{"items":[{"step":"inspect","status":"in_progress"},{"step":"verify","status":"pending"}]}`,
		`{"items":[{"step":"inspect","status":"done"},{"step":"verify","status":"done"}]}`,
	}
	if p.calls <= len(plans) {
		return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "p", Name: "plan.update", Arguments: json.RawMessage(plans[p.calls-1])}}}}, nil
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: "planned and done"}}, nil
}

func TestNativePlanIsRecordedOnTheExecution(t *testing.T) {
	dir := t.TempDir()
	p := &planProvider{}
	r := newRig(t, openStore(t, dir), memory.New(), dir, "plan-instance", func(o *engine.Options) {
		o.Models.(*llm.Registry).Add(llm.ProviderConfig{Name: "planner"}, p)
	})
	r.deploy("metadata: {name: plan}\nspec:\n  nodes:\n    - id: worker\n      prompt: Do the work\n      model: {provider: planner, name: fixture}\n      loop: {mode: once}\n      isolation: {mode: process}\n      limits: {turnTimeout: 10s}\n")
	var x *domain.Execution
	eventually(t, 10*time.Second, "worker finished", func() bool {
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "plan", Limit: 1})
		if len(xs) == 1 && xs[0].Status.Terminal() {
			x = xs[0]
			return true
		}
		return false
	})
	if x.Status != domain.ExecSucceeded {
		t.Fatalf("%s %s", x.Status, x.Error)
	}
	evs, err := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "plan", Types: []domain.EventType{domain.EvAgentPlan}, Ascending: true})
	if err != nil || len(evs) != 2 {
		t.Fatalf("%d %v", len(evs), err)
	}
	var last struct {
		Items []struct{ Step, Status string }
	}
	if err := json.Unmarshal(evs[1].Data, &last); err != nil {
		t.Fatal(err)
	}
	if evs[1].ExecutionID != x.ID || evs[1].NodeID != "worker" || len(last.Items) != 2 || last.Items[1].Status != "done" {
		t.Fatalf("%+v %+v", evs[1], last)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !strings.Contains(p.system, "## Working method") || !strings.Contains(p.system, "plan.update") {
		t.Fatal("the native system prompt does not ask for a plan")
	}
}

// smallProvider records what a small-model provider receives.
type smallProvider struct {
	mu  sync.Mutex
	req *llm.ChatRequest
}

func (*smallProvider) Name() string                                    { return "small-model" }
func (*smallProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *smallProvider) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.req == nil {
		p.req = r
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{Content: "done"}}, nil
}

func TestProviderModelGuidanceReachesTheNativeTurn(t *testing.T) {
	dir := t.TempDir()
	p := &smallProvider{}
	r := newRig(t, openStore(t, dir), memory.New(), dir, "small-instance", func(o *engine.Options) {
		o.Models.(*llm.Registry).Add(llm.ProviderConfig{Name: "small-model", ModelGuidance: llm.GuidanceSmall}, p)
	})
	r.deploy("metadata: {name: small}\nspec:\n  nodes:\n    - id: worker\n      prompt: Do the work\n      model: {provider: small-model, name: fixture}\n      loop: {mode: once}\n      isolation: {mode: process}\n      tools: [filesystem.read]\n      limits: {turnTimeout: 10s}\n")
	eventually(t, 10*time.Second, "small model asked", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.req != nil
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if !strings.Contains(p.req.System, "## How to call tools") || !strings.Contains(p.req.System, `filesystem.read {"path":"<file>"`) {
		t.Fatalf("system prompt without small-model guidance:\n%s", p.req.System)
	}
	for _, tool := range p.req.Tools {
		if tool.Name == "filesystem.read" && (strings.Contains(string(tool.Parameters), "description") || strings.Contains(tool.Description, "offset/limit")) {
			t.Fatalf("schema not compacted: %+v %s", tool, tool.Parameters)
		}
	}
}
