package dur

import (
	"testing"
	"time"
)

func TestParseSeconds(t *testing.T) {
	if d, err := Parse("30s"); err != nil || d != 30*time.Second {
		t.Fatal(d, err)
	}
}
