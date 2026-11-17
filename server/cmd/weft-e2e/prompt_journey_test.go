//go:build e2e

package main

import (
	"github.com/woliveiras/weft/server/internal/llm"
	"testing"
)

func TestPromptJourneyFixture(t *testing.T) {
	for _, tc := range []struct{ prompt, tool string }{{"prompt-journey-fixture", "filesystem.write"}, {"prompt-journey-fixture deny-tool", "shell.exec"}} {
		r := &llm.ChatRequest{Messages: []llm.ChatMessage{{Content: tc.prompt}}}
		got := promptJourneyResponse(r)
		if len(got.Message.ToolCalls) != 1 || got.Message.ToolCalls[0].Name != tc.tool {
			t.Fatal(got)
		}
		r.Messages = append(r.Messages, llm.ChatMessage{Role: llm.RoleTool, Content: "denied by policy"})
		got = promptJourneyResponse(r)
		if got.Message.Content != "Fixture tool outcome: denied by policy" || len(got.Message.ToolCalls) != 0 {
			t.Fatal(got)
		}
	}
}
