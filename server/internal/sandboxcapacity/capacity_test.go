package sandboxcapacity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func empty(context.Context) ([]Container, error) { return nil, nil }
func manager(t *testing.T) *Manager {
	t.Helper()
	s, e := fsstore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return &Manager{Store: s, Scope: "test", Policy: Policy{MaxActive: 2, TotalMemory: "128m", DefaultMemory: "64m"}}
}
func TestMemoryPolicy(t *testing.T) {
	for _, s := range []string{"", "-1", "0", "1.5g", "9999999999999999999g", "8gb"} {
		if _, e := Bytes(s); e == nil {
			t.Fatal(s)
		}
	}
	if n, e := Bytes("64M"); e != nil || n != 64<<20 {
		t.Fatalf("%d %v", n, e)
	}
	m := manager(t)
	for _, s := range []string{"1m", "129m", "-2g"} {
		if _, e := m.Memory(s); e == nil {
			t.Fatal(s)
		}
	}
	if n, e := m.Memory(""); e != nil || n != 64<<20 {
		t.Fatalf("%d %v", n, e)
	}
	for _, p := range []Policy{{MaxActive: 0, TotalMemory: "1g", DefaultMemory: "64m"}, {MaxActive: 1, TotalMemory: "0", DefaultMemory: "64m"}, {MaxActive: 1, TotalMemory: "64m", DefaultMemory: "65m"}} {
		if p.Validate() == nil {
			t.Fatal(p)
		}
	}
}
func TestPersistentCapacityCancellationAndRelease(t *testing.T) {
	m := manager(t)
	ctx := context.Background()
	if e := m.Reserve(ctx, "a", 96<<20, empty); e != nil {
		t.Fatal(e)
	}
	// Reconstructed controller honors durable reservations, even with no live container.
	next := *m
	waited := make(chan struct{}, 1)
	next.Notify = func(_, state string) {
		if state == "waiting" {
			select {
			case waited <- struct{}{}:
			default:
			}
		}
	}
	waitCtx, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { result <- next.Reserve(waitCtx, "b", 64<<20, empty) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("not queued")
	}
	cancel()
	if e := <-result; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	slots, e := m.load(ctx)
	if e != nil || len(slots) != 1 {
		t.Fatalf("cancel leaked reservation: %v %v", slots, e)
	}
	if e := m.Release(ctx, "a"); e != nil {
		t.Fatal(e)
	}
	if e := next.Reserve(ctx, "b", 64<<20, empty); e != nil {
		t.Fatal(e)
	}
	if e := next.Reserve(ctx, "b", 64<<20, empty); e != nil {
		t.Fatal(e)
	}
	slots, _ = m.load(ctx)
	if len(slots) != 1 {
		t.Fatal(slots)
	}
}
func TestInventoryAndDependencyFailures(t *testing.T) {
	m := manager(t)
	ctx := context.Background()
	failure := errors.New("daemon offline")
	if e := m.Reserve(ctx, "a", 64<<20, func(context.Context) ([]Container, error) { return nil, failure }); !errors.Is(e, failure) {
		t.Fatal(e)
	}
	if e := m.Reserve(ctx, "a", 64<<20, func(context.Context) ([]Container, error) { return []Container{{Name: "old", Memory: 0}}, nil }); e == nil {
		t.Fatal("unbounded sandbox admitted")
	}
	if e := m.Reserve(ctx, "a", 64<<20, empty); e != nil {
		t.Fatal(e)
	}
	if e := m.Reserve(ctx, "a", 32<<20, empty); e == nil {
		t.Fatal("changed live reservation")
	}
	slots, _ := m.load(ctx)
	if len(slots) != 1 {
		t.Fatal(slots)
	}
	if e := m.Store.KVPut(ctx, namespace, m.Scope, []byte("corrupt")); e != nil {
		t.Fatal(e)
	}
	if e := m.Reserve(ctx, "b", 64<<20, empty); e == nil {
		t.Fatal("corrupt accounting allowed admission")
	}
}
func TestConcurrentManagersCannotOverbook(t *testing.T) {
	m := manager(t)
	m.Policy.MaxActive = 1
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	results := make(chan error, 2)
	for _, name := range []string{"a", "b"} {
		copy := *m
		go func() { results <- copy.Reserve(ctx, name, 64<<20, empty) }()
	}
	accepted := 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			accepted++
		} else if !errors.Is(e, context.DeadlineExceeded) {
			t.Fatal(e)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d", accepted)
	}
}

func TestCapacityMetricsHaveBoundedLabels(t *testing.T) {
	m := manager(t)
	ctx := context.Background()
	if e := m.Reserve(ctx, "metrics", 64<<20, empty); e != nil {
		t.Fatal(e)
	}
	if e := m.Release(ctx, "metrics"); e != nil {
		t.Fatal(e)
	}
	families, e := prometheus.DefaultGatherer.Gather()
	if e != nil {
		t.Fatal(e)
	}
	foundCounter, foundLatency := false, false
	for _, f := range families {
		switch f.GetName() {
		case "weft_sandbox_capacity_transitions_total":
			foundCounter = true
			for _, metric := range f.Metric {
				if len(metric.Label) != 1 || metric.Label[0].GetName() != "state" {
					t.Fatal("unbounded capacity metric labels")
				}
			}
		case "weft_sandbox_admission_duration_seconds":
			foundLatency = true
		}
	}
	if !foundCounter || !foundLatency {
		t.Fatal("missing capacity metrics")
	}
}
