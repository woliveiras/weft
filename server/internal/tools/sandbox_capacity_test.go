package tools

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestSandboxCapacityIntegration(t *testing.T) {
	bin, log := fakeDocker(t, "0")
	store, e := fsstore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	waited := make(chan string, 10)
	budget := &sandboxcapacity.Manager{Store: store, Scope: "test", Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "128m", DefaultMemory: "64m"}, Notify: func(n, s string) {
		if s == "waiting" {
			waited <- n
		}
	}}
	newSandbox := func(name string) *DockerSandbox {
		t.Helper()
		b := *budget
		s, e := NewDockerSandbox(DockerSandboxConfig{Name: name, Image: "alpine", HostDir: "/fixture", DockerBin: bin, Capacity: &b})
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	a, b := newSandbox("a"), newSandbox("b")
	if e := a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	args := strings.Join(a.RunArgs(), " ")
	if !strings.Contains(args, "--cpus 1.000") || !strings.Contains(args, "--memory 67108864 --memory-swap 67108864") {
		t.Fatal(args)
	}
	result := make(chan error, 1)
	go func() { result <- b.Start(context.Background()) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("not waiting")
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "run -d --name b") {
		t.Fatal("started before admission")
	}
	if e := a.Close(); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("capacity not released")
	}
	c := newSandbox("c")
	go func() { result <- c.Start(context.Background()) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("not waiting")
	}
	if e := c.Close(); e != nil {
		t.Fatal(e)
	}
	if e := <-result; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e := b.Close(); e != nil {
		t.Fatal(e)
	}
}
func TestSandboxStartFailureReleasesOnlyAfterCleanup(t *testing.T) {
	bin, log := fakeDocker(t, "0")
	data, e := os.ReadFile(bin)
	if e != nil {
		t.Fatal(e)
	}
	data = []byte(strings.Replace(string(data), "case \"$1\" in", "case \"$1\" in\n run) echo failed; exit 1 ;;", 1))
	if e := os.WriteFile(bin, data, 0755); e != nil {
		t.Fatal(e)
	}
	store, e := fsstore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	budget := &sandboxcapacity.Manager{Store: store, Scope: "failure", Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "64m", DefaultMemory: "64m"}}
	s, e := NewDockerSandbox(DockerSandboxConfig{Name: "failed", Image: "alpine", HostDir: "/fixture", DockerBin: bin, Capacity: budget})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.Start(context.Background()); e == nil {
		t.Fatal("missing failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := budget.Reserve(ctx, "next", 64<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); e != nil {
		t.Fatal(e)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "rm -f failed") {
		t.Fatal(string(calls))
	}
}

func TestRemovalFailureRetainsCapacity(t *testing.T) {
	bin, _ := fakeDocker(t, "0")
	store, e := fsstore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	budget := &sandboxcapacity.Manager{Store: store, Scope: "retained", Policy: sandboxcapacity.Policy{MaxActive: 1, TotalMemory: "64m", DefaultMemory: "64m"}}
	s, e := NewDockerSandbox(DockerSandboxConfig{Name: "held", Image: "fixture", HostDir: "/fixture", DockerBin: bin, Capacity: budget})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(bin)
	if e != nil {
		t.Fatal(e)
	}
	modified := strings.Replace(string(data), "case \"$1\" in", "case \"$1\" in\n rm) echo daemon-unreachable; exit 1 ;;", 1)
	if e := os.WriteFile(bin, []byte(modified), 0755); e != nil {
		t.Fatal(e)
	}
	if e := s.Close(); e == nil {
		t.Fatal("missing cleanup error")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if e := budget.Reserve(ctx, "next", 64<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("reservation released before confirmed removal: %v", e)
	}
	if e := os.WriteFile(bin, data, 0755); e != nil {
		t.Fatal(e)
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if e := budget.Reserve(ctx2, "next", 64<<20, func(context.Context) ([]sandboxcapacity.Container, error) { return nil, nil }); e != nil {
		t.Fatal(e)
	}
}

func TestCPUInventoryParsing(t *testing.T) {
	for _, tc := range []struct {
		row  string
		want int64
		bad  bool
	}{
		{"/running true 67108864 1250000000", 1250, false},
		{"/running true 67108864 1250000001", 1251, false},
		{"/running true 67108864 bad", 0, true},
		{"/running true 67108864", 0, true},
	} {
		result, err := DockerCapacityInventory(context.Background(), func(_ context.Context, args ...string) (string, error) {
			if args[0] == "ps" {
				return "id", nil
			}
			return tc.row, nil
		})
		if (err != nil) != tc.bad {
			t.Fatalf("%s: %v", tc.row, err)
		}
		if !tc.bad && (len(result) != 1 || result[0].MilliCPU != tc.want) {
			t.Fatal(result)
		}
	}
}
