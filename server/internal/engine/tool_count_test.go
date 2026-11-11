package engine_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/storage"
)

type countProvider struct{}

func (countProvider) Name() string                                    { return "count" }
func (countProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (countProvider) Chat(_ context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	for _, m := range r.Messages {
		if m.Role == llm.RoleTool {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "done"}}, nil
		}
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{
		{ID: "one", Name: "filesystem.read", Arguments: json.RawMessage(`{"path":"missing.txt"}`)},
		{ID: "two", Name: "filesystem.read", Arguments: json.RawMessage(`{"path":"missing-again.txt"}`)},
	}}}, nil
}

func TestNativeToolAttemptsCountedOnceThroughGateway(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, openStore(t, dir), memory.New(), dir, "count", func(o *engine.Options) {
		o.Models.(*llm.Registry).Add(llm.ProviderConfig{Name: "count"}, countProvider{})
	})
	r.deploy(`metadata: {name: tool-count}
spec:
  nodes:
    - id: reader
      prompt: Inspect files
      runtime: {kind: native}
      model: {provider: count, name: fixture}
      isolation: {mode: process}
      loop: {mode: once}
      tools: [filesystem.read]
`)
	eventually(t, 10*time.Second, "attempts recorded once even on tool failure", func() bool {
		xs, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "tool-count", Limit: 1})
		if err != nil || len(xs) == 0 || xs[0].CompletedAt == nil {
			return false
		}
		if xs[0].ToolCalls != 2 {
			t.Fatalf("expected two attempts, got %d", xs[0].ToolCalls)
		}
		return true
	})
}
