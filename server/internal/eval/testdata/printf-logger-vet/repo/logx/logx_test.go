package logx

import (
	"bytes"
	"testing"
)

func TestLog(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "[jobs]")
	l.Log("hello", 3)
	if got := buf.String(); got != "[jobs] hello 3\n" {
		t.Fatalf("%q", got)
	}
}
