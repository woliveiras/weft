package dur

import (
	"testing"
	"time"
)

func TestParseHidden(t *testing.T) {
	for in, want := range map[string]time.Duration{"10m": 10 * time.Minute, "1m": time.Minute, "2h": 2 * time.Hour, "45s": 45 * time.Second} {
		if d, err := Parse(in); err != nil || d != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, d, err, want)
		}
	}
	if _, err := Parse("5x"); err == nil {
		t.Error("unknown unit must fail")
	}
}
