package preview

import "testing"

func TestTitle(t *testing.T) {
	if got := Title("  Shopping list  "); got != "Shopping list" {
		t.Fatalf("%q", got)
	}
	if got := Title("Meeting notes for the quarterly review"); got != "Meeting notes for th" {
		t.Fatalf("%q", got)
	}
}
