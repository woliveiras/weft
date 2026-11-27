package native

import (
	"context"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/runtime"
	"testing"
)

func TestFinishPreservesExplicitBlocker(t *testing.T) {
	env := &scriptedEnv{replies: []*llm.ChatResponse{toolCall("finish", `{"signal":"blocked","summary":"The dependency registry is unavailable."}`)}}
	result, err := New().Turn(context.Background(), &runtime.TurnInput{}, env)
	if err != nil || result.Signal != runtime.SignalBlocked || result.Summary != "The dependency registry is unavailable." {
		t.Fatal(result, err)
	}
}

func TestBlockedFinishRequiresAReason(t *testing.T) {
	result := &runtime.TurnResult{}
	message, done := callTool(context.Background(), &scriptedEnv{}, result, llm.ToolCall{Name: "finish", Arguments: []byte(`{"signal":"blocked"}`)})
	if done || result.Signal != "" || message == "" {
		t.Fatal(message, done, result)
	}
}
