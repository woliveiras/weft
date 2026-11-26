//go:build e2e

package main

import (
	"errors"

	"github.com/woliveiras/weft/server/internal/llm"
)

// These two scripted model replies exercise the real API, worker and store.
// An unexpected prompt is a fixture error so a test cannot pass by echoing it.
func consoleFixtureResponse(request *llm.ChatRequest) (*llm.ChatResponse, error) {
	if len(request.Messages) == 0 {
		return nil, errors.New("missing Chat fixture input")
	}
	last := request.Messages[len(request.Messages)-1]
	if last.Role != llm.RoleUser {
		return nil, errors.New("expected a Chat fixture user message")
	}
	var content string
	switch last.Content {
	case "Let me know when the release notes review is complete.":
		content = `{"kind":"task.watch","task":"Release notes for Chat fixture","title":"Follow release notes"}`
	case "Show me the current state of the release notes review.":
		content = `{"kind":"task.inspect","task":"Release notes for Chat fixture","title":"Inspect release notes"}`
	default:
		return nil, errors.New("unknown Chat fixture request")
	}
	return &llm.ChatResponse{
		Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: content},
		Usage:   llm.Usage{InputTokens: 24, OutputTokens: 18},
	}, nil
}
