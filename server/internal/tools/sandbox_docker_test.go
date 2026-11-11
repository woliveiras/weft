//go:build !windows

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDocker writes a docker CLI stand-in that appends "DOCKER_HOST|args"
// to a log and answers like a daemon. exitCode != 0 makes every call fail.
func fakeDocker(t *testing.T, exitCode string) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "docker"), filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "${DOCKER_HOST:-}|$*" >> ` + log + `
if [ "` + exitCode + `" != "0" ]; then echo "Cannot connect to the Docker daemon at tcp://nowhere:2376" >&2; echo "second line" >&2; exit ` + exitCode + `; fi
case "$1" in
  inspect) echo "Error: No such object" >&2; exit 1 ;;
  version) echo "28.5.1" ;;
  exec) echo "ran: $*" ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestDockerSandboxUsesDedicatedDaemon(t *testing.T) {
	bin, log := fakeDocker(t, "0")
	s, err := NewDockerSandbox(DockerSandboxConfig{
		Name: "weft-sbx-g-dev-0", Image: "weft/sandbox:latest", HostDir: "/var/lib/weft/workspaces/g",
		DockerBin: bin, DockerHost: "tcp://sandbox-daemon:2376",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Exec(context.Background(), ExecRequest{Argv: []string{"go", "version"}})
	if err != nil || res.ExitCode != 0 || !strings.Contains(res.Stdout, "go version") {
		t.Fatalf("exec: %+v %v", res, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	if len(got) != 4 {
		t.Fatalf("calls: %q", got)
	}
	for i, prefix := range []string{"inspect ", "run -d --name weft-sbx-g-dev-0", "exec -w /workspace weft-sbx-g-dev-0 go version", "rm -f weft-sbx-g-dev-0"} {
		host, args, _ := strings.Cut(got[i], "|")
		if host != "tcp://sandbox-daemon:2376" {
			t.Errorf("call %d went to %q, want the sandbox daemon", i, host)
		}
		if !strings.HasPrefix(args, prefix) {
			t.Errorf("call %d = %q, want prefix %q", i, args, prefix)
		}
	}
}

func TestDockerSandboxDefaultsToCLIEnvironment(t *testing.T) {
	bin, log := fakeDocker(t, "0")
	t.Setenv("DOCKER_HOST", "unix:///from/env.sock")
	s, _ := NewDockerSandbox(DockerSandboxConfig{Name: "n", Image: "i", HostDir: "/w", DockerBin: bin})
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host, _, _ := strings.Cut(calls(t, log)[0], "|"); host != "unix:///from/env.sock" {
		t.Fatalf("without DockerHost the CLI environment applies, got %q", host)
	}
}

func TestPingDocker(t *testing.T) {
	bin, _ := fakeDocker(t, "0")
	v, err := PingDocker(context.Background(), bin, "tcp://sandbox-daemon:2376")
	if err != nil || v != "28.5.1" {
		t.Fatalf("ping: %q %v", v, err)
	}
	bad, _ := fakeDocker(t, "1")
	_, err = PingDocker(context.Background(), bad, "tcp://sandbox-daemon:2376")
	if err == nil || err.Error() != "docker daemon tcp://sandbox-daemon:2376 unreachable: Cannot connect to the Docker daemon at tcp://nowhere:2376" {
		t.Fatalf("ping failure: %v", err)
	}
	if _, err := PingDocker(context.Background(), filepath.Join(t.TempDir(), "missing"), ""); err == nil || !strings.Contains(err.Error(), "(default) unreachable") {
		t.Fatalf("missing CLI: %v", err)
	}
	// A failing daemon must also fail sandbox start with the daemon's message.
	s, _ := NewDockerSandbox(DockerSandboxConfig{Name: "n", Image: "i", HostDir: "/w", DockerBin: bad, DockerHost: "tcp://x"})
	if err := s.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "Cannot connect") {
		t.Fatalf("start against a down daemon: %v", err)
	}
}

func TestPreviewUsesExplicitEntrypointWithBoundedLogs(t *testing.T) {
	sb, err := NewDockerSandbox(DockerSandboxConfig{Name: "preview", Image: "fixture", HostDir: "/workspace", CPU: "0.5", Memory: "128m", Command: []string{"/weft-preview/weft-preview", "serve", "/weft-preview/config.json"}})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(sb.RunArgs(), " ")
	for _, want := range []string{"--network none", "--cap-drop ALL", "--cpus 0.5", "--memory 128m", "--log-opt max-size=1m", "--entrypoint /weft-preview/weft-preview fixture serve /weft-preview/config.json"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q: %s", want, args)
		}
	}
}
