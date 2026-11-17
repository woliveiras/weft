package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func idleDocker(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	script := `#!/bin/sh
d="` + dir + `"
echo "$*" >> "$d/calls"
sub="$1"
for arg do name="$arg"; done
[ ! -f "$d/fail-$sub" ] || { echo "synthetic daemon failure"; exit 1; }
case "$sub" in
inspect) [ -f "$d/$name" ] || { echo "No such object"; exit 1; }; cat "$d/$name";;
run) shift; while [ "$1" != --name ]; do shift; done; echo true > "$d/$2";;
start) echo true > "$d/$name";;
stop) echo false > "$d/$name";;
rm) rm -f "$d/$name";;
exec) touch "$d/executing"; sleep 0.2; rm "$d/executing";;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func TestSandboxSuspendResumeIntegration(t *testing.T) {
	bin, dir := idleDocker(t)
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	budget := &sandboxcapacity.Manager{Store: store, Scope: "idle", Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "64m", DefaultMemory: "64m"}}
	create := func(name string) *DockerSandbox {
		s, err := NewDockerSandbox(DockerSandboxConfig{Name: name, Image: "alpine", HostDir: "/fixture", DockerBin: bin, Capacity: budget})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	ctx := context.Background()
	a, b := create("a"), create("b")
	defer a.Close()
	defer b.Close()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "a"))
	if err != nil || string(data) != "false\n" {
		t.Fatalf("container not preserved: %s %v", data, err)
	}
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// Resume through a newly constructed controller still respects reservations.
	resumed := create("a")
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := resumed.Start(wait); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resume did not wait: %v", err)
	}
	if err := b.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Start(ctx); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), "run -d --name a ") != 1 || !strings.Contains(string(calls), "start a") || strings.Contains(string(calls), "rm -f a") {
		t.Fatalf("recreated task: %s", calls)
	}
}

func TestSandboxSuspensionFailuresRetainState(t *testing.T) {
	for _, failure := range []string{"stop", "inspect", "start"} {
		t.Run(failure, func(t *testing.T) {
			bin, dir := idleDocker(t)
			store, err := fsstore.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			budget := &sandboxcapacity.Manager{Store: store, Scope: "failure", Policy: sandboxcapacity.DefaultPolicy()}
			s, err := NewDockerSandbox(DockerSandboxConfig{Name: "task", Image: "alpine", HostDir: "/fixture", DockerBin: bin, Capacity: budget})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := s.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if failure == "start" {
				if err := s.Suspend(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "fail-"+failure), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if failure == "start" {
				err = s.Start(ctx)
			} else {
				err = s.Suspend(ctx)
			}
			if err == nil {
				t.Fatal("failure hidden")
			}
			if _, err := os.Stat(filepath.Join(dir, "task")); err != nil {
				t.Fatal("container deleted")
			}
			if failure != "start" {
				budget.Policy.MaxActive = 1
				wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
				err = budget.Reserve(wait, "other", 1<<30, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil })
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("released unconfirmed stop: %v", err)
				}
			}
			if err := os.Remove(filepath.Join(dir, "fail-"+failure)); err != nil {
				t.Fatal(err)
			}
			if err := s.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSandboxSuspendWaitsForExec(t *testing.T) {
	bin, dir := idleDocker(t)
	s, err := NewDockerSandbox(DockerSandboxConfig{Name: "task", Image: "alpine", HostDir: "/fixture", DockerBin: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, err := s.Exec(context.Background(), ExecRequest{Shell: "true"}); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "executing")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exec not started")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.Suspend(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "executing")); !os.IsNotExist(err) {
		t.Fatal("stopped during exec")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
