package main

import (
	"bytes"
	"encoding/json"
	"github.com/woliveiras/weft/server/internal/tools"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandIdentityAndResults(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name, uid, input string
		valid            bool
	}{
		{"replacement", "new", `{"Argv":["touch","marker"]}`, false}, {"empty", "", "{}", false},
		{"bad-request", "old", `{"Argv":["true"],"Shell":"true"}`, false},
		{"exit", "old", `{"Argv":["sh","-c","echo out; echo err >&2; exit 7"]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(t.Context(), root, tc.uid, "old", strings.NewReader(tc.input), &out)
			if !tc.valid {
				if err == nil || out.Len() != 0 {
					t.Fatal("accepted invalid invocation")
				}
				return
			}
			var r tools.ExecResult
			if err != nil || json.Unmarshal(out.Bytes(), &r) != nil || r.ExitCode != 7 || r.Stdout != "out\n" || r.Stderr != "err\n" {
				t.Fatal(r, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatal("command ran in replacement pod")
	}
	var out bytes.Buffer
	body, _ := json.Marshal(tools.ExecRequest{Argv: []string{"sleep", "10"}, Timeout: 20 * time.Millisecond})
	if err := run(t.Context(), root, "old", "old", bytes.NewReader(body), &out); err != nil {
		t.Fatal(err)
	}
	var r tools.ExecResult
	if json.Unmarshal(out.Bytes(), &r) != nil || !r.TimedOut || r.ExitCode == 0 {
		t.Fatal(r)
	}
}

func TestFingerprintLimitAndOutputTruncation(t *testing.T) {
	for _, tc := range []struct {
		limit     int
		truncated bool
	}{{8 << 20, false}, {4, true}} {
		body, err := json.Marshal(tools.ExecRequest{Argv: []string{"printf", "abcdefghij"}, MaxOutput: tc.limit, TailOutput: 2})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := run(t.Context(), t.TempDir(), "same", "same", bytes.NewReader(body), &out); err != nil {
			t.Fatal(err)
		}
		var res tools.ExecResult
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 0 || res.Truncated != tc.truncated || !strings.HasPrefix(res.Stdout, "abcd") || !strings.HasSuffix(res.Stdout, "ij") {
			t.Fatal(res)
		}
	}
}
