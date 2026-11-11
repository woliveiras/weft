package runner

import (
	"bytes"
	"errors"
	"testing"

	"example.com/jobs/logx"
)

func TestRun(t *testing.T) {
	var buf bytes.Buffer
	r := New(logx.New(&buf, "[jobs]"))
	ok := func() error { return nil }
	n := r.Run([]Job{{"a", ok}, {"b", func() error { return errors.New("boom") }}, {"c", ok}})
	want := "[jobs] starting 3 jobs\n[jobs] job a done\n[jobs] job b failed: boom\n[jobs] job c done\n[jobs] finished, 1 failed\n"
	if n != 1 || buf.String() != want {
		t.Fatalf("%d %q", n, buf.String())
	}
}
