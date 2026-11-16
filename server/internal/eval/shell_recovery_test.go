package eval_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/eval"
	"github.com/woliveiras/weft/server/internal/llm"
)

type recoveringScript struct {
	scripted
	command string
}

func (recoveringScript) Name() string { return "recovering" }
func (s recoveringScript) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	round := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			round++
		}
	}
	if round == 0 {
		args, err := json.Marshal(map[string]string{"command": s.command})
		if err != nil {
			return nil, err
		}
		return &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "denied", Name: "shell.exec", Arguments: args}}}, StopReason: "tool_calls"}, nil
	}
	if round == 1 {
		result := req.Messages[len(req.Messages)-1].Content
		for _, want := range []string{"shell.exec is granted", "cwd", "argv"} {
			if !strings.Contains(result, want) {
				return nil, fmt.Errorf("missing recovery guidance %q: %s", want, result)
			}
		}
	}
	// Replay the ordinary solution after the denied call; the gateway executes
	// its test command directly with argv, without increasing the grant.
	copyReq := *req
	copyReq.Messages = nil
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "denied" {
			continue
		}
		copyReq.Messages = append(copyReq.Messages, m)
	}
	return s.scripted.Chat(ctx, &copyReq)
}
func TestScriptedAgentRecoversAfterRestrictedShellDenial(t *testing.T) {
	for _, command := range []string{"cd . && go test ./...", "go test './..."} {
		t.Run(command, func(t *testing.T) {
			a, ws := newApp(t)
			a.Models.Add(llm.ProviderConfig{Name: "recovering", Type: "mock", DefaultModel: "script"}, recoveringScript{command: command})
			result := eval.Run(t.Context(), a, ws, eval.Cases[0], "recovering", "script", 901, 20, time.Minute)
			if !result.Success || result.ToolErrors != 1 {
				t.Fatalf("recovery failed: %+v", result)
			}
		})
	}
}
