package calc

import "testing"

func TestTotalHidden(t *testing.T) {
	if got := Total([]int{1, 2, 3, 4}); got != 10 {
		t.Fatalf("Total = %d, want 10", got)
	}
	if got := Add(-2, 2); got != 0 {
		t.Fatalf("Add(-2, 2) = %d", got)
	}
}
