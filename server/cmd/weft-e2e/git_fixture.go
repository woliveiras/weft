//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

func prepareGitInspectionFixture(ctx context.Context, dir string, node *domain.Node) error {
	if err := os.WriteFile(filepath.Join(dir, "result.txt"), []byte("baseline result\n"), 0600); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "inspection baseline"}} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("Git inspection fixture: %w: %s", err, out)
		}
	}
	node.Runtime = domain.RuntimeRef{Kind: domain.RuntimeNative}
	node.Isolation = domain.IsolationSpec{Mode: domain.IsolationProcess}
	node.Model = domain.ModelRef{Provider: "e2e", Name: "fixture"}
	node.Prompt = "git-inspect-fixture"
	node.Tools = []domain.ToolGrant{{Name: "filesystem.write", Paths: []string{"result.txt"}}, {Name: "shell.exec"}}
	return nil
}

func gitInspectionResponse(r *llm.ChatRequest) *llm.ChatResponse {
	toolResults := 0
	for _, message := range r.Messages {
		if message.Role == llm.RoleTool {
			toolResults++
		}
	}
	if toolResults == 1 {
		return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "shell-result", Name: "shell.exec", Arguments: json.RawMessage(`{"command":"printf 'shell change\n' > shell.txt; (sleep 2; printf delayed > background-one.txt; printf one > concurrent.txt) >/dev/null 2>&1 & (sleep 2; printf delayed > background-two.txt; printf two > concurrent.txt) >/dev/null 2>&1 &"}`)}}}, StopReason: "tool_calls"}
	}
	for _, message := range r.Messages {
		if message.Role == llm.RoleTool {
			return &llm.ChatResponse{Message: llm.ChatMessage{Content: "Inspection task finished: " + message.Content}, StopReason: "stop"}
		}
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: []llm.ToolCall{{ID: "write-result", Name: "filesystem.write", Arguments: json.RawMessage(`{"path":"result.txt","content":"updated by fixture task\n"}`)}}}, StopReason: "tool_calls"}
}
