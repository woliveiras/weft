package secrets

import (
	"strings"
	"testing"
)

func TestSafeSummaryRedactsBeforeBoundingAndPreservesEvidence(t *testing.T) {
	summary := SafeSummary("3 checks failed. token=fixture-secret-value https://user:password@example.test/private @maintainer \x1b\u202e" + strings.Repeat("more ", 100) + "\nprivate second line")
	for _, bad := range []string{"fixture-secret-value", "password", "@maintainer", "\x1b", "\u202e", "private second line"} {
		if strings.Contains(summary, bad) {
			t.Fatalf("leaked %q: %q", bad, summary)
		}
	}
	if !strings.HasPrefix(summary, "3 checks failed.") || len([]rune(summary)) > 201 {
		t.Fatal(summary)
	}
	if SafeSummary("echoed 1 message(s)") != "echoed 1 message(s)" {
		t.Fatal("numeric evidence changed")
	}
}
