package llm

import "testing"

func TestDefaultAnswerTokensIsBounded(t *testing.T) {
	for window, want := range map[int]int{0: 256, 1024: 256, 32768: 4096, 131072: 8192, 1 << 20: 8192} {
		if got := DefaultAnswerTokens(window); got != want {
			t.Fatalf("window %d: %d, want %d", window, got, want)
		}
	}
}
