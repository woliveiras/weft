package logx

import (
	"bytes"
	"testing"
)

func TestHiddenLogf(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "[jobs]")
	l.Logf("loaded %d of %s", 3, "five")
	l.Logf("100%% done")
	if got := buf.String(); got != "[jobs] loaded 3 of five\n[jobs] 100% done\n" {
		t.Fatalf("%q", got)
	}
}
