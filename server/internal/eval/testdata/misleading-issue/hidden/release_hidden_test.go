package release

import "testing"

func TestHiddenCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v1.10.0", "v1.9.0", 1},
		{"v1.9.0", "v1.10.0", -1},
		{"v1.2.10", "v1.2.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"1.4", "v1.4.0", 0},
		{"v1.4.0", "1.4", 0},
		{"1.4", "v1.4.1", -1},
		{"v1.5", "v1.4.9", 1},
		{"v10.0.0", "v9.0.0", 1},
		{"v1.0.0", "v1.0.0", 0},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestHiddenLatest(t *testing.T) {
	if got := Latest([]string{"1.4", "v1.9.0", "v1.10.0", "v1.2.12"}); got != "v1.10.0" {
		t.Fatalf("Latest = %q, want v1.10.0", got)
	}
	if got := Latest([]string{"v1.10.0", "v2.0", "v1.99.1"}); got != "v2.0" {
		t.Fatalf("Latest = %q, want v2.0", got)
	}
}
