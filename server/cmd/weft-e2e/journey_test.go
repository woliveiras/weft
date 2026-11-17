//go:build e2e

package main

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestJourneyFixtureOrderAndFailure(t *testing.T) {
	for _, request := range []*llm.ChatRequest{nil, {}} {
		if _, err := journeyResponse(request); err == nil {
			t.Fatal("empty request accepted")
		}
	}
	for _, mode := range []string{"required", "automatic"} {
		request := &llm.ChatRequest{Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: "release-one-journey journey-" + mode}}}
		for _, name := range []string{"mcp.journey-" + mode + ".search", "delegation.request", "filesystem.write"} {
			response, err := journeyResponse(request)
			if err != nil || len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != name {
				t.Fatal(response, err)
			}
			request.Messages = append(request.Messages, llm.ChatMessage{Role: llm.RoleTool, ToolCallID: response.Message.ToolCalls[0].ID, Content: "fixture result"})
		}
		response, err := journeyResponse(request)
		if err != nil || response.Message.Content != "Integrated journey completed" {
			t.Fatal(response, err)
		}
	}
	for _, message := range []llm.ChatMessage{{Role: llm.RoleTool, ToolCallID: "wrong"}, {Role: llm.RoleTool, ToolCallID: "journey-mcp", Content: `{"error":"denied"}`}} {
		if _, err := journeyResponse(&llm.ChatRequest{Messages: []llm.ChatMessage{{Content: "release-one-journey"}, message}}); err == nil {
			t.Fatal("invalid journey accepted")
		}
	}
}
