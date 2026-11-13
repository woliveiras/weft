package environment

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

func TestCombinedBuildfile(t *testing.T) {
	for _, tc := range []struct {
		name, source, target, want string
		invalid                    bool
	}{
		{"last", "FROM alpine AS first\nRUN echo first\nFROM busybox\n", "", "FROM weft-selected\n", false},
		{"target", "FROM alpine AS selected\nFROM busybox AS other\n", "SELECTED", "FROM selected\n", false},
		{"collision", "FROM alpine AS weft-selected\nFROM busybox\n", "", "FROM weft-selectedx\n", false},
		{"external", "FROM alpine\nCOPY --from=busybox:1.37 /bin/busybox /tmp/tool\n", "", "COPY --from=busybox:1.37", false},
		{"missing", "", "", "", true},
		{"bad target", "FROM alpine\n", "missing", "", true},
		{"bad source", "FROM alpine AS a AS b", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := combinedBuildfile(tc.source, tc.target, []string{"/home/sandbox/.cache/pip"})
			if tc.invalid {
				if err == nil {
					t.Fatal("accepted invalid Dockerfile")
				}
				return
			}
			if err != nil || !strings.Contains(got, tc.want) || !strings.Contains(got, "USER 10001") {
				t.Fatalf("%s %v", got, err)
			}
		})
	}
}

func TestBoundedRunArgs(t *testing.T) {
	ctx := context.WithValue(context.Background(), buildBudgetKey{}, buildBudget{"test", 64 << 20, 1000})
	got := strings.Join(boundedRunArgs(ctx, []string{"run", "--rm", "--memory", "1g", "--cpus", "9", "alpine", "true"}), " ")
	if !strings.Contains(got, "--name weft-envbuild-test-copy") || !strings.Contains(got, "--memory 67108864 --memory-swap 67108864") || strings.Contains(got, "1g") || !strings.Contains(got, "--cpus 1.000") || strings.Contains(got, "--cpus 9") {
		t.Fatal(got)
	}
	got = strings.Join(boundedRunArgs(ctx, []string{"run", "--name", "setup", "alpine"}), " ")
	if strings.Count(got, "--name") != 1 {
		t.Fatal(got)
	}
	if got := boundedRunArgs(ctx, []string{"buildx", "build"}); len(got) != 2 {
		t.Fatal(got)
	}
}

func TestWrappedContext(t *testing.T) {
	var input bytes.Buffer
	tw := tar.NewWriter(&input)
	for name, body := range map[string]string{"context/source": "original", contextDockerfile: "FROM alpine"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := wrappedContext(&input, "FROM alpine\nUSER 10001\n")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	tr := tar.NewReader(f)
	bodies := map[string]string{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(tr)
		if e != nil {
			t.Fatal(e)
		}
		bodies[h.Name] = string(body)
	}
	if bodies["context/source"] != "original" || bodies[contextDockerfile] != "FROM alpine\nUSER 10001\n" {
		t.Fatal(bodies)
	}
	if _, err := wrappedContext(strings.NewReader("broken"), "x"); err == nil {
		t.Fatal("accepted corrupt context")
	}
	if _, err := wrappedContext(bytes.NewReader(make([]byte, 1024)), "x"); err == nil {
		t.Fatal("accepted missing Dockerfile")
	}
}

func capacityHarness(t *testing.T) *harness {
	h := setup(t)
	h.m.opt.Capacity = &sandboxcapacity.Manager{Store: h.store, Scope: "test-daemon", Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "128m", DefaultMemory: "64m"}}
	return h
}
func TestBuildCapacityIntegration(t *testing.T) {
	h := capacityHarness(t)
	ctx := context.Background()
	inventory := func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }
	if err := h.m.opt.Capacity.Reserve(ctx, "execution", 64<<20, inventory); err != nil {
		t.Fatal(err)
	}
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22"})
	waiting, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	called := false
	_, _, err := h.m.withBuildCapacity(waiting, g, io.Discard, func(context.Context) (string, []CacheMount, error) { called = true; return "", nil, nil })
	if !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("queued build: called=%v err=%v", called, err)
	}
	if err := h.m.opt.Capacity.Release(ctx, "execution"); err != nil {
		t.Fatal(err)
	}
	_, _, err = h.m.withBuildCapacity(ctx, g, io.Discard, func(ctx context.Context) (string, []CacheMount, error) {
		other := *h.m.opt.Capacity // another control-plane instance, same persisted budget
		bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		if err := other.Reserve(bounded, "execution", 64<<20, inventory); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("overbooked: %v", err)
		}
		return "ready", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.m.opt.Capacity.Reserve(ctx, "execution", 64<<20, inventory); err != nil {
		t.Fatal(err)
	}
}

func TestBoundedBuildPipelineIntegration(t *testing.T) {
	for _, fail := range []string{"", "buildx-create", "buildx-build", "buildx-rm", "run", "ps"} {
		t.Run(fail, func(t *testing.T) {
			h := capacityHarness(t)
			if fail != "" {
				h.docker.set(t, "fail-"+fail, "")
			}
			g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22", Setup: []string{"echo setup"}, Memory: "64m"})
			if _, err := h.m.Build(context.Background(), g, false, "test"); err != nil {
				t.Fatal(err)
			}
			v := h.wait(t, g)
			if fail != "" && fail != "buildx-rm" {
				if v.State != StateFailed || v.Error == "" {
					t.Fatalf("%+v", v)
				}
				return
			}
			if v.State != StateReady {
				t.Fatalf("%+v", v)
			}
			if fail == "buildx-rm" && !strings.Contains(v.Log, "Warning: could not remove bounded builder") {
				t.Fatalf("missing cleanup warning: %+v", v)
			}
			calls := strings.Join(h.docker.calls(t), "\n")
			for _, want := range []string{"cpu-period=100000,cpu-quota=100000", "--cpus 1.000", "buildx create", "memory=67108864,memory-swap=67108864,restart-policy=no", "buildx build", "--load", "run --memory 67108864 --memory-swap 67108864", "buildx rm --force"} {
				if !strings.Contains(calls, want) {
					t.Fatalf("missing %s in %s", want, calls)
				}
			}
			if strings.Index(calls, "buildx rm --force") > strings.Index(calls, "run --memory") {
				t.Fatal("builder overlapped setup")
			}
		})
	}
}

func TestBuildCapacityInvalidAndCleanupFailure(t *testing.T) {
	h := capacityHarness(t)
	ctx := context.Background()
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine", Memory: "256m"})
	called := false
	run := func(context.Context) (string, []CacheMount, error) { called = true; return "", nil, nil }
	if _, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, run); err == nil || called {
		t.Fatal("invalid memory admitted")
	}
	g.Spec.Environment.Memory = ""
	_, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, func(context.Context) (string, []CacheMount, error) {
		// Docker becomes unavailable during teardown: retain the durable slot.
		h.docker.set(t, "fail-ps", "")
		return "image", nil, nil
	})
	if err == nil {
		t.Fatal("cleanup failure hidden")
	}
	inventory := func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }
	bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := h.m.opt.Capacity.Reserve(bounded, "another", 64<<20, inventory); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost reservation: %v", err)
	}
	if err := os.Remove(filepath.Join(h.docker.dir, "fail-ps")); err != nil {
		t.Fatal(err)
	}
	// A fresh manager recovers the graph's stale reservation before retrying.
	m, err := New(h.m.opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.withBuildCapacity(ctx, g, io.Discard, run); err != nil {
		t.Fatal(err)
	}
	if err := h.m.opt.Capacity.Reserve(ctx, "another", 64<<20, inventory); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCapacityMemoryBudgetIntegration(t *testing.T) {
	h := capacityHarness(t)
	h.m.opt.Capacity.Policy.MaxActive = 4
	h.m.opt.Capacity.Policy.TotalMemory = "64m"
	ctx := context.Background()
	inventory := func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }
	if err := h.m.opt.Capacity.Reserve(ctx, "execution", 64<<20, inventory); err != nil {
		t.Fatal(err)
	}
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine"})
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, _, err := h.m.withBuildCapacity(wait, g, io.Discard, func(context.Context) (string, []CacheMount, error) {
		t.Error("memory overbooked despite three available slots")
		return "", nil, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestBuildCapacityRemovalFailureIntegration(t *testing.T) {
	h := capacityHarness(t)
	ctx := context.Background()
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine"})
	_, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, func(context.Context) (string, []CacheMount, error) {
		h.docker.set(t, "containers", "owned-worker\n")
		h.docker.set(t, "fail-rm", "")
		return "", nil, errors.New("setup failed")
	})
	if err == nil || !strings.Contains(err.Error(), "remove build worker") || !strings.Contains(err.Error(), "setup failed") {
		t.Fatalf("lost failure context: %v", err)
	}
	inventory := func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := h.m.opt.Capacity.Reserve(wait, "another", 64<<20, inventory); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("released live worker reservation: %v", err)
	}
}

func TestBuildWaitsForExecutionCPUBudget(t *testing.T) {
	h := capacityHarness(t)
	h.m.opt.Capacity.Policy.MaxActive = 4
	h.m.opt.Capacity.Policy.TotalCPU = "1"
	ctx := context.Background()
	inventory := func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }
	if err := h.m.opt.Capacity.ReserveCPU(ctx, "execution", 32<<20, 750, inventory); err != nil {
		t.Fatal(err)
	}
	g := graphWith(&domain.EnvironmentSpec{Image: "alpine", CPU: "0.5"})
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	ran := false
	run := func(ctx context.Context) (string, []CacheMount, error) {
		ran = true
		b := ctx.Value(buildBudgetKey{}).(buildBudget)
		if b.cpu != 500 {
			t.Fatal(b)
		}
		return "image", nil, nil
	}
	if _, _, err := h.m.withBuildCapacity(wait, g, io.Discard, run); !errors.Is(err, context.DeadlineExceeded) || ran {
		t.Fatal(err, ran)
	}
	if err := h.m.opt.Capacity.Release(ctx, "execution"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, run); err != nil || !ran {
		t.Fatal(err, ran)
	}
	g.Spec.Environment.CPU = "2"
	ran = false
	if _, _, err := h.m.withBuildCapacity(ctx, g, io.Discard, run); err == nil || ran {
		t.Fatal("oversized CPU reached Docker")
	}
}
