//go:build e2e

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerFixtureLifecycleAndTransientFailure(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "docker")
	if err := os.WriteFile(bin, []byte(dockerFixture), 0700); err != nil {
		t.Fatal(err)
	}
	call := func(input string, args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := call("", "inspect", "-f", "{{.State.Running}}", "missing"); err == nil {
		t.Fatal("missing container appeared to exist")
	}
	if _, err := call("", "start", "missing"); err == nil {
		t.Fatal("missing container resumed")
	}
	if _, err := call("", "run", "-d", "--name", "worker", "alpine"); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ command, state string }{{"stop", "false"}, {"start", "true"}} {
		if _, err := call("", step.command, "worker"); err != nil {
			t.Fatal(err)
		}
		if got, err := call("", "inspect", "-f", "{{.State.Running}}", "worker"); err != nil || got != step.state {
			t.Fatalf("%s: %q %v", step.command, got, err)
		}
	}
	if _, err := call("", "inspect", "--format", `{{index .Config.Labels "weft.compose"}}`, "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := call("", "rm", "-f", "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := call("", "inspect", "-f", "{{.State.Running}}", "worker"); err == nil {
		t.Fatal("removed container still exists")
	}
	for attempt := 0; attempt < 2; attempt++ {
		_, err := call("FROM alpine\nRUN E2E_FAIL_ONCE\n", "buildx", "build", "-")
		if (err != nil) != (attempt == 0) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if _, err := call("E2E_BUILD_FAIL", "build", "-"); err == nil {
			t.Fatal("permanent build failure succeeded")
		}
	}
}
