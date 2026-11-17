package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestSandboxIdleRealDaemon(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-sandbox-capacity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("weft-idle-test-%d", time.Now().UnixNano())
	budget := &sandboxcapacity.Manager{Store: store, Scope: id, Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "64m", DefaultMemory: "64m"}}
	cfg := DockerSandboxConfig{Name: id, Image: "alpine:3.22", HostDir: id + "-workspace", Volumes: []string{id + "-cache:/cache"}, DockerBin: bin, Capacity: budget}
	s, err := NewDockerSandbox(cfg)
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	total := int64(64 << 20)
	totalCPU := int64(1000)
	for _, c := range active {
		if c.Memory <= 0 || c.MilliCPU <= 0 {
			t.Fatal("existing unbounded sandbox prevents this test")
		}
		total += c.Memory
		totalCPU += c.MilliCPU
	}
	budget.Policy.TotalCPU = sandboxcapacity.FormatCPU(totalCPU)
	budget.Policy.MaxActive = len(active) + 1
	budget.Policy.TotalMemory = strconv.FormatInt(total, 10)
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, v := range []string{id + "-workspace", id + "-cache"} {
			if out, err := DockerCommand(cleanup, bin, "", "volume", "rm", v).CombinedOutput(); err != nil {
				t.Errorf("test volume cleanup: %s %v", out, err)
			}
		}
	})
	run := func(s *DockerSandbox, script string) {
		t.Helper()
		result, err := s.Exec(ctx, ExecRequest{Shell: script})
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("sandbox command: %+v %v", result, err)
		}
	}
	run(s, "echo workspace > /workspace/marker; echo cache > /cache/marker; echo layer > /tmp/marker")
	before, err := s.docker(ctx, "inspect", "-f", "{{.Id}}", id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := s.docker(ctx, "inspect", "-f", "{{.State.Running}}", id)
	if err != nil || strings.TrimSpace(state) != "false" {
		t.Fatalf("not stopped: %s %v", state, err)
	}
	// A build reservation can take the freed slot; the same task queues to resume.
	inventory := s.inventory
	if err := budget.Reserve(ctx, id+"-build", 64<<20, inventory); err != nil {
		t.Fatal(err)
	}
	recreated, err := NewDockerSandbox(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(ctx, 750*time.Millisecond)
	err = recreated.Start(wait)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resume skipped capacity: %v", err)
	}
	if err := budget.Release(ctx, id+"-build"); err != nil {
		t.Fatal(err)
	}
	run(recreated, `test "$(cat /sys/fs/cgroup/cpu.max)" = "100000 100000"; test "$(cat /workspace/marker)" = workspace; test "$(cat /cache/marker)" = cache; test "$(cat /tmp/marker)" = layer; test "$(cat /sys/fs/cgroup/memory.max)" = 67108864; test "$(wc -l < /proc/net/route)" -eq 1`)
	after, err := recreated.docker(ctx, "inspect", "-f", "{{.Id}}", id)
	if err != nil || before != after {
		t.Fatalf("container recreated: %s / %s %v", before, after, err)
	}
	if err := recreated.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	if err := recreated.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("stop/start preserved container ID, writable layer, workspace and cache; released slot admitted a build; resume waited for capacity; controller recreation, repeated suspension and 64 MiB/network-none limits verified")
}
