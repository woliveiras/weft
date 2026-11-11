//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestAnalysisFixture(t *testing.T) {
	p := analysisProvider{}
	response, err := p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Content: "package.json: ordinary project"}}})
	if err != nil {
		t.Fatal(err)
	}
	var proposal environment.Analysis
	if err := json.Unmarshal([]byte(response.Message.Content), &proposal); err != nil {
		t.Fatal(err)
	}
	if err := environment.ValidateAnalysis(&proposal); err != nil {
		t.Fatal(err)
	}
	if proposal.Environment.Image != "alpine:3.22" || proposal.Rationale == "" {
		t.Fatalf("invalid fixture: %+v", proposal)
	}
	response, err = p.Chat(context.Background(), &llm.ChatRequest{Messages: []llm.ChatMessage{{Content: "package.json: analysis-failure"}}})
	if response != nil || err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("failure fixture: %v %v", response, err)
	}
}

func TestBrowserAddress(t *testing.T) {
	for _, tc := range []struct{ port, want string }{{"", "127.0.0.1:18089"}, {"18101", "127.0.0.1:18101"}, {"1", "127.0.0.1:1"}, {"65535", "127.0.0.1:65535"}} {
		got, err := browserAddress(tc.port)
		if err != nil || got != tc.want {
			t.Fatalf("browserAddress(%q) = %q, %v", tc.port, got, err)
		}
	}
	for _, port := range []string{"0", "-1", "65536", "host:18101", "abc"} {
		if _, err := browserAddress(port); err == nil {
			t.Fatalf("accepted invalid port %q", port)
		}
	}
}
