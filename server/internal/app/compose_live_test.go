package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestComposeSandboxRealDaemon(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-compose")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	docker := func(ctx context.Context, args ...string) (string, error) {
		out, err := tools.DockerCommand(ctx, bin, "", args...).CombinedOutput()
		return string(out), err
	}
	// No pull or auth changes: use the dedicated daemon's already cached fixture.
	if out, err := docker(ctx, "image", "inspect", "alpine:3.22", "--format", "{{.Id}}"); err != nil {
		t.Fatalf("cached alpine:3.22 required: %s %v", out, err)
	}
	dir := t.TempDir()
	source := `services:
  dev: {image: 'alpine:3.22', depends_on: [web]}
  web:
    image: alpine:3.22
    command: [sh, -c, 'test -f /tmp/index.html || echo helper > /tmp/index.html; while true; do { printf "HTTP/1.0 200 OK\r\n\r\n"; cat /tmp/index.html; } | nc -l -p 8080 -w 1; done']
  ignored: {image: unused, privileged: true}
`
	if err := os.WriteFile(filepath.Join(dir, "compose.dev.yaml"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	discovery, err := environment.Discover(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	c := discovery.Candidates[0]
	env, err := environment.Resolve(ctx, dir, environment.Selection{Source: discovery.Source, Path: c.Path, SHA256: c.SHA256, Service: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.ValidateAccepted(ctx, dir, env); err != nil {
		t.Fatal(err)
	}
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("weft-sbx-compose-test-%d", time.Now().UnixNano())
	active, err := tools.DockerCapacityInventory(ctx, docker)
	if err != nil {
		t.Fatal(err)
	}
	memory, cpu, count := int64(256<<20), int64(1000), 4
	for _, c := range active {
		if c.Memory <= 0 || c.MilliCPU <= 0 {
			t.Fatal("unbounded existing sandbox prevents validation")
		}
		memory += c.Memory
		cpu += c.MilliCPU
		count += max(c.Count, 1)
	}
	budget := &sandboxcapacity.Manager{Store: store, Scope: prefix, Policy: sandboxcapacity.Policy{MaxActive: count, TotalMemory: strconv.FormatInt(memory, 10), DefaultMemory: "64m", TotalCPU: sandboxcapacity.FormatCPU(cpu), DefaultCPU: "0.25"}}
	create := func(suffix string) *tools.ComposeSandbox {
		sb, err := tools.NewComposeSandbox(tools.DockerSandboxConfig{Name: prefix + suffix, Image: env.Image, HostDir: prefix + suffix + "-workspace", User: "10001:10001", DockerBin: bin, Capacity: budget}, env.Companions)
		if err != nil {
			t.Fatal(err)
		}
		return sb
	}
	a, b := create("-a"), create("-b")
	t.Cleanup(func() {
		for _, sb := range []*tools.ComposeSandbox{a, b} {
			if err := sb.Close(); err != nil {
				t.Error(err)
			}
		}
		for _, suffix := range []string{"-a", "-b"} {
			name := prefix + suffix + "-workspace"
			if out, err := docker(context.Background(), "volume", "inspect", name, "--format", "{{.Name}}"); err == nil {
				if out, err := docker(context.Background(), "volume", "rm", name); err != nil {
					t.Error(out, err)
				}
			} else if !strings.Contains(out, "no such volume") {
				t.Error(out, err)
			}
		}
	})
	if err := a.Start(ctx); err != nil {
		out, _ := docker(ctx, "logs", prefix+"-a-svc-web")
		t.Log("test helper output:", out)
		t.Fatal(err)
	}
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	request := func(sb *tools.ComposeSandbox, want string) {
		t.Helper()
		for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
			out, err := sb.Exec(ctx, tools.ExecRequest{Shell: `test "$(id -u)" = 10001; test "$(cat /sys/fs/cgroup/cpu.max)" = "25000 100000"; ! awk '$2 == "00000000" { found=1 } END { exit !found }' /proc/net/route; wget -qO- -T 1 http://web:8080`})
			if err == nil && out.ExitCode == 0 && strings.TrimSpace(out.Stdout) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("internal service/limits check failed", want)
	}
	request(a, "helper")
	request(b, "helper")
	if out, err := docker(ctx, "exec", prefix+"-a-svc-web", "sh", "-c", "echo private-a > /tmp/index.html"); err != nil {
		t.Fatal(out, err)
	}
	request(a, "private-a")
	request(b, "helper")
	for _, suffix := range []string{"-a", "-b"} {
		if out, err := docker(ctx, "network", "inspect", prefix+suffix+"-services", "--format", "{{.Internal}}"); err != nil || strings.TrimSpace(out) != "true" {
			t.Fatal(out, err)
		}
		if out, err := docker(ctx, "inspect", prefix+suffix+"-svc-web", "--format", "{{.HostConfig.NanoCpus}} {{.HostConfig.Memory}} {{len .HostConfig.PortBindings}}"); err != nil || strings.TrimSpace(out) != "250000000 67108864 0" {
			t.Fatal(out, err)
		}
	}
	third := create("-c")
	wait, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	if err := third.Start(wait); err == nil {
		t.Fatal("partial group overbooked")
	}
	stop()
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	a = create("-a")
	request(a, "private-a")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	request(b, "helper")
	t.Log("reviewed Compose -> private internal services; helper isolation, no published ports/default route, CPU/memory caps, atomic group wait, suspend/resume preservation and independent teardown verified")
}
