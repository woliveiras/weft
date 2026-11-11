package mock

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestMockBehaviours(t *testing.T) {
	p := New(llm.ProviderConfig{})
	ctx := context.Background()
	user := []llm.ChatMessage{{Role: llm.RoleUser, Content: "build feature X"}}
	tools := []llm.ToolSchema{{Name: llm.WireToolName("emit")}, {Name: "finish"}}

	r, _ := p.Chat(ctx, &llm.ChatRequest{Messages: user, Metadata: map[string]string{"mock_response": "scripted"}})
	if r.Message.Content != "scripted" {
		t.Fatalf("scripted: %+v", r)
	}
	r, _ = p.Chat(ctx, &llm.ChatRequest{Messages: user, Tools: tools})
	if len(r.Message.ToolCalls) != 1 || r.Message.ToolCalls[0].Name != "emit" || r.StopReason != "tool_calls" {
		t.Fatalf("emit: %+v", r)
	}
	var args map[string]any
	_ = json.Unmarshal(r.Message.ToolCalls[0].Arguments, &args)
	if args["type"] != "mock.output" || args["payload"].(map[string]any)["summary"] != "build feature X" {
		t.Fatalf("emit args %v", args)
	}
	withResult := append(append([]llm.ChatMessage{}, user...), r.Message, llm.ChatMessage{Role: llm.RoleTool, ToolCallID: r.Message.ToolCalls[0].ID, Content: "ok"})
	r, _ = p.Chat(ctx, &llm.ChatRequest{Messages: withResult, Tools: tools})
	if len(r.Message.ToolCalls) != 1 || r.Message.ToolCalls[0].Name != "finish" {
		t.Fatalf("finish: %+v", r)
	}
	r, _ = p.Chat(ctx, &llm.ChatRequest{Messages: user, JSON: true})
	var obj map[string]any
	if err := json.Unmarshal([]byte(r.Message.Content), &obj); err != nil || obj["mock"] != true {
		t.Fatalf("json: %q", r.Message.Content)
	}
	r, _ = p.Chat(ctx, &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: strings.Repeat("a", 500)}}})
	if r.Message.Content != "MOCK: "+strings.Repeat("a", 300) || r.Usage.InputTokens != 125 {
		t.Fatalf("text: %d %+v", len(r.Message.Content), r.Usage)
	}
	if p.Calls() != 5 {
		t.Fatalf("calls %d", p.Calls())
	}
}
