package llm

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultAnswerTokensIsBounded(t *testing.T) {
	for window, want := range map[int]int{0: 256, 1024: 256, 32768: 4096, 131072: 8192, 1 << 20: 8192} {
		if got := DefaultAnswerTokens(window); got != want {
			t.Fatalf("window %d: %d, want %d", window, got, want)
		}
	}
}

func TestCheckContext(t *testing.T) {
	req := func(n int) *ChatRequest {
		return &ChatRequest{Messages: []ChatMessage{{Role: RoleUser, Content: strings.Repeat("x", n)}}}
	}
	// Unknown window: nothing is checked, however long the prompt.
	if err := CheckContext("p", 0, req(1<<20)); err != nil {
		t.Fatalf("unknown window: %v", err)
	}
	if err := CheckContext("p", 1024, req(4*1000)); err != nil {
		t.Fatalf("fitting prompt: %v", err)
	}
	// The estimate reaching the window fails, naming provider and window.
	err := CheckContext("gem", 1024, req(4*1024))
	if !errors.Is(err, ErrContextExceeded) || !strings.Contains(err.Error(), "gem's context window of 1024") {
		t.Fatalf("oversized prompt: %v", err)
	}
}
