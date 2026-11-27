// Package toolchain tests the contributor-facing wrapper with an isolated Docker CLI.
package toolchain_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGoWrapper(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		image      string
		buildImage bool
		exit       int
	}{
		{"test dispatch", []string{"test", "-run", "TestOne|TestTwo", "./..."}, "sha256:test-toolchain", true, 0},
		{"ordinary build", []string{"build", "./cmd/weftd"}, "golang:1.26-alpine", false, 0},
		{"toolchain build failure", []string{"test", "./..."}, "", true, 19},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// One NUL-delimited argument log per invocation retains shell metacharacters.
			cli := `#!/bin/sh
set -eu
printf '%s\n' "$1" >> "$WRAPPER_LOG/calls"
printf '%s\0' "$@" >> "$WRAPPER_LOG/$1"
case "$1" in
  build) test "${WRAPPER_FAIL:-}" != 1 || exit 19; echo sha256:test-toolchain ;;
  run) ;;
  *) exit 30 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(cli), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("WRAPPER_LOG", dir)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GO_IMAGE", "golang:1.26-alpine")
			t.Setenv("WRAPPER_FAIL", "")
			if tc.exit != 0 {
				t.Setenv("WRAPPER_FAIL", "1")
			}
			cmd := exec.CommandContext(t.Context(), "../../../scripts/go.sh", tc.args...)
			out, err := cmd.CombinedOutput()
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.exit {
				t.Fatalf("wrapper exit: %v, %s", err, out)
			}
			expected := []string{"run"}
			if tc.buildImage {
				expected = []string{"build", "run"}
			}
			if tc.exit != 0 {
				expected = []string{"build"}
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil || !slices.Equal(expected, strings.Fields(string(calls))) {
				t.Fatalf("wrapper calls: %q, %v", calls, err)
			}
			read := func(verb string) []string {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, verb))
				if err != nil {
					t.Fatal(err)
				}
				raw := bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0})
				args := make([]string, len(raw))
				for i, arg := range raw {
					args[i] = string(arg)
				}
				return args
			}
			if tc.buildImage && !slices.Contains(read("build"), "GO_IMAGE=golang:1.26-alpine") {
				t.Fatal("missing toolchain build argument")
			}
			if tc.exit == 0 {
				run := read("run")
				want := append([]string{tc.image, "go"}, tc.args...)
				if len(run) < len(want) || !slices.Equal(run[len(run)-len(want):], want) {
					t.Fatalf("unexpected toolchain command: %q", run)
				}
			}
		})
	}
}
