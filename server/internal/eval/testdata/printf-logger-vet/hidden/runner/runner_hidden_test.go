package runner

import (
	"bytes"
	"errors"
	"testing"

	"example.com/jobs/logx"
)

func TestHiddenRunOutput(t *testing.T) {
	var buf bytes.Buffer
	r := New(logx.New(&buf, "[jobs]"))
	fail := func() error { return errors.New("disk full") }
	n := r.Run([]Job{{"backup", fail}, {"index", func() error { return nil }}, {"mail", fail}})
	want := "[jobs] starting 3 jobs\n[jobs] job backup failed: disk full\n[jobs] job index done\n[jobs] job mail failed: disk full\n[jobs] finished, 2 failed\n"
	if n != 2 || buf.String() != want {
		t.Fatalf("%d %q", n, buf.String())
	}
}
