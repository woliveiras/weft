package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

// Uses only uniquely named resources inside the dedicated test daemon.
func TestBuildCapacityRealSandbox(t *testing.T) {
	bin := os.Getenv("WEFT_TEST_ENV_DOCKER_BIN")
	if bin == "" {
		t.Skip("run make test-environment-context")
	}
	ctx := context.Background()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("build-capacity-test-%d", time.Now().UnixNano())
	ws := reuseWorkspaces{root: t.TempDir(), prefix: id + "-workspace-"}
	budget := &sandboxcapacity.Manager{Store: store, Scope: id, Policy: sandboxcapacity.Policy{DefaultCPU: "0.5", MaxActive: 1, TotalMemory: "256m", DefaultMemory: "256m"}}
	m, err := New(Options{Store: store, Workspaces: ws, DockerBin: bin, Capacity: budget, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	g := graphWith(nil)
	g.Metadata.ID = id
	dir, err := ws.Ensure(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, dir, "payload.txt", "bounded-context")
	writeDiscoveryFile(t, dir, "Dockerfile", "FROM alpine:3.22 AS selected\nCOPY payload.txt /payload.txt\nRUN sleep 5\nFROM alpine:3.22 AS unused\nRUN exit 19\n")
	setup := []string{"test \"$(id -u)\" = 10001", "test \"$(cat /payload.txt)\" = bounded-context", "echo setup-memory=$(cat /sys/fs/cgroup/memory.max)", "test \"$(cat /sys/fs/cgroup/cpu.max)\" = \"50000 100000\"", "sleep 3"}
	definition, err := json.Marshal(map[string]any{"dockerfile": "Dockerfile", "context": ".", "target": "selected", "setup": setup, "network": map[string]string{"setup": "none"}})
	if err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, dir, ".weft/environment.yaml", string(definition))
	selection := discoverSelection(t, dir, "")
	env, err := Resolve(ctx, dir, selection)
	if err != nil {
		t.Fatal(err)
	}
	g.Spec.Environment = env
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := m.Reset(cleanup, id, "test"); err != nil {
			t.Error(err)
		}
		var out bytes.Buffer
		if err := m.docker(cleanup, nil, &out, "volume", "ls", "-q", "--filter", "name=^"+ws.prefix+id+"$"); err != nil {
			t.Error(err)
		} else if strings.TrimSpace(out.String()) != "" {
			if err := m.docker(cleanup, nil, io.Discard, "volume", "rm", ws.prefix+id); err != nil {
				t.Error(err)
			}
		}
	})
	if err := budget.Reserve(ctx, id+"-waiting-fixture", 256<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Build(ctx, g, false, "test"); err != nil {
		t.Fatal(err)
	}
	waitingSeen := false
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		view, err := m.Get(ctx, g)
		if err != nil {
			t.Fatal(err)
		}
		if view.Phase == "waiting_capacity" {
			waitingSeen = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := budget.Release(ctx, id+"-waiting-fixture"); err != nil {
		t.Fatal(err)
	}
	if !waitingSeen {
		t.Fatal("real build did not expose persisted capacity wait")
	}
	deadline := time.Now().Add(4 * time.Minute)
	builderSeen, setupSeen := false, false
	var v *View
	for time.Now().Before(deadline) {
		v, err = m.Get(ctx, g)
		if err != nil {
			t.Fatal(err)
		}
		if v.State == StateFailed {
			t.Fatalf("%s\n%s", v.Error, v.Log)
		}
		if v.State == StateReady {
			break
		}
		for _, worker := range []string{builderContainer(id), buildContainer(id)} {
			var out bytes.Buffer
			// A missing worker is expected between sequential phases.
			if e := m.docker(ctx, nil, &out, "inspect", "--format", "{{.State.Running}} {{.HostConfig.Memory}} {{.HostConfig.MemorySwap}} {{.HostConfig.NanoCpus}} {{.HostConfig.CpuQuota}} {{.HostConfig.CpuPeriod}}", worker); e == nil && strings.HasPrefix(out.String(), "true ") {
				expected := "true 268435456 268435456 500000000 0 0"
				if worker == builderContainer(id) {
					expected = "true 268435456 268435456 0 50000 100000"
				}
				if strings.TrimSpace(out.String()) != expected {
					t.Fatalf("worker cap: %s", out.String())
				}
				if worker == builderContainer(id) {
					builderSeen = true
				} else {
					setupSeen = true
				}
			}
		}
		if builderSeen && !setupSeen {
			// An execution request against another manager cannot overlap this build.
			other := *budget
			wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			e := other.Reserve(wait, id+"-execution", 256<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil })
			cancel()
			if e == nil {
				t.Fatal("execution admitted while build holds the only slot")
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if v == nil || v.Phase != "" || v.State != StateReady || !builderSeen || !setupSeen {
		t.Fatalf("incomplete validation builder=%v setup=%v status=%+v", builderSeen, setupSeen, v)
	}
	if !strings.Contains(v.Log, "setup-memory=268435456") {
		t.Fatalf("setup cgroup: %s", v.Log)
	}
	if err := budget.Reserve(ctx, id+"-execution", 256<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := budget.Release(ctx, id+"-execution"); err != nil {
		t.Fatal(err)
	}
	t.Log("real BuildKit worker and setup capped at 0.5 CPU and 256 MiB with no extra swap; COPY context/selected stage and uid 10001 preserved; execution admission blocked until build cleanup")

	// Dependency failures must also free the budget and leave no running builder.
	g.Spec.Environment = &domain.EnvironmentSpec{Dockerfile: "FROM alpine:3.22\nRUN exit 17\n"}
	if _, err := m.Build(ctx, g, true, "test"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		v, err = m.Get(ctx, g)
		if err != nil {
			t.Fatal(err)
		}
		if v.State != StateBuilding {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if v.State != StateFailed {
		t.Fatalf("failed RUN: %+v", v)
	}
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := budget.Reserve(wait, id+"-after-failure", 256<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := budget.Release(ctx, id+"-after-failure"); err != nil {
		t.Fatal(err)
	}
}
