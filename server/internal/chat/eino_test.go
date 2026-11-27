package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestEinoTurnIsolatesConcurrentReads(t *testing.T) {
	run, err := NewEinoTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	for _, owner := range []string{"alice", "bob"} {
		go func() {
			name := "lookup_" + owner
			tool := ReadTool{Schema: llm.ToolSchema{Name: name}, Read: func(ctx context.Context, _ json.RawMessage) (ToolObservation, error) {
				started <- struct{}{}
				select {
				case <-ctx.Done():
					return ToolObservation{}, ctx.Err()
				case <-release:
					return ToolObservation{Data: json.RawMessage(fmt.Sprintf("%q", owner))}, nil
				}
			}}
			history := []llm.ChatMessage{{Role: llm.RoleUser, Content: owner}}
			result, err := run(ctx, history, []ReadTool{tool}, func(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
				if req.Messages[0].Content != owner || req.Tools[0].Name != name {
					return nil, errors.New("crossed request boundary")
				}
				last := req.Messages[len(req.Messages)-1]
				if last.Role == llm.RoleUser {
					return nativeTestCall(owner, name, "{}"), nil
				}
				if last.ToolCallID != owner || last.Content != fmt.Sprintf("%q", owner) {
					return nil, errors.New("wrong observation")
				}
				return answer(owner), nil
			})
			if err == nil && (!result.Grounded || result.Text != owner) {
				err = errors.New("wrong answer")
			}
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("turns did not overlap")
		}
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestEinoTurnCancellationStopsBeforeReading(t *testing.T) {
	run, err := NewEinoTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	read := false
	tool := ReadTool{Schema: TaskQuerySchema(), Read: func(context.Context, json.RawMessage) (ToolObservation, error) {
		read = true
		return ToolObservation{}, nil
	}}
	got, err := run(ctx, []llm.ChatMessage{{Role: llm.RoleUser, Content: "List tasks"}}, []ReadTool{tool}, func(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
		cancel()
		return taskCall("list", "{}"), nil
	})
	if !errors.Is(err, context.Canceled) || read || got.Grounded || got.Plan.Kind != "" {
		t.Fatalf("read=%t result=%+v err=%v", read, got, err)
	}
}

func BenchmarkChatOrchestration(b *testing.B) {
	eino, err := NewEinoTurn(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	for _, runner := range []struct {
		name string
		run  TurnRunner
	}{{"direct", RunTurn}, {"eino", eino}} {
		b.Run(runner.name, func(b *testing.B) {
			history := []llm.ChatMessage{{Role: llm.RoleUser, Content: "Open workflows"}}
			resp := nativeTestCall("open", "open_console_page", `{"destination":"workflows"}`)
			call := func(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) { return resp, nil }
			b.ReportAllocs()
			for b.Loop() {
				result, err := runner.run(b.Context(), history, nil, call)
				if err != nil || result.Plan.Kind != OpenManual {
					b.Fatal(result, err)
				}
			}
		})
	}
}
