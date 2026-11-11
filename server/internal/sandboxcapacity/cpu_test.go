package sandboxcapacity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCPUParsingAndPolicy(t *testing.T) {
	for raw, want := range map[string]int64{"1": 1000, "0.01": 10, "1.125": 1125, "2.5": 2500} {
		got, err := CPU(raw)
		if err != nil || got != want {
			t.Fatalf("%s: %d %v", raw, got, err)
		}
		if round, err := CPU(FormatCPU(got)); err != nil || round != got {
			t.Fatal(round, err)
		}
	}
	for _, raw := range []string{"", "0", "-1", "NaN", "Inf", "1e2", "0.001", "0.1001", "1m", "999999999999999999999"} {
		if _, err := CPU(raw); err == nil {
			t.Fatal(raw)
		}
	}
	m := manager(t)
	m.Policy.TotalCPU, m.Policy.DefaultCPU = "1.5", "0.5"
	if got, err := m.CPU(""); err != nil || got != 500 {
		t.Fatal(got, err)
	}
	if _, err := m.CPU("2"); err == nil {
		t.Fatal("oversized request")
	}
	m.Policy.DefaultCPU = "2"
	if m.Policy.Validate() == nil {
		t.Fatal("invalid CPU default")
	}
}

func TestCPUAdmissionPersistsAndCancelsAcrossManagers(t *testing.T) {
	m := manager(t)
	m.Policy.MaxActive = 10
	m.Policy.TotalCPU = "1.5"
	ctx := context.Background()
	if err := m.ReserveCPU(ctx, "build", 32<<20, 1000, empty); err != nil {
		t.Fatal(err)
	}
	next := *m
	if err := next.ReserveCPU(ctx, "setup", 32<<20, 500, empty); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	if err := next.ReserveCPU(wait, "execution", 32<<20, 10, empty); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := next.ReserveCPU(ctx, "build", 32<<20, 500, empty); err == nil {
		t.Fatal("changed live CPU reservation")
	}
	if err := m.Release(ctx, "build"); err != nil {
		t.Fatal(err)
	}
	if err := next.ReserveCPU(ctx, "execution", 32<<20, 1000, empty); err != nil {
		t.Fatal(err)
	}
	slots, err := m.load(ctx)
	if err != nil || len(slots) != 2 || slots["execution"].MilliCPU != 1000 {
		t.Fatal(slots, err)
	}
	m.Policy.TotalCPU = "1"
	if err := m.ReserveCPU(ctx, "execution", 32<<20, 1000, empty); err == nil {
		t.Fatal("reduced CPU budget accepted")
	}
}

func TestCPUInventoryAndLegacyReservationsFailClosed(t *testing.T) {
	m := manager(t)
	ctx := context.Background()
	for _, cpu := range []int64{0, -1} {
		if err := m.ReserveCPU(ctx, "new", 32<<20, 1000, func(context.Context) ([]Container, error) {
			return []Container{{Name: "existing", Memory: 32 << 20, MilliCPU: cpu}}, nil
		}); err == nil {
			t.Fatal("unbounded inventory")
		}
	}
	if err := m.Store.KVPut(ctx, namespace, m.Scope, []byte(`{"legacy":33554432}`)); err != nil {
		t.Fatal(err)
	}
	if err := m.ReserveCPU(ctx, "new", 32<<20, 1000, empty); err == nil {
		t.Fatal("legacy CPU guessed")
	}
	if err := m.Release(ctx, "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := m.ReserveCPU(ctx, "new", 32<<20, 1000, empty); err != nil {
		t.Fatal(err)
	}
	// Inventory and durable reservation are counted once, using the larger cap.
	if err := m.ReserveCPU(ctx, "new", 32<<20, 1000, func(context.Context) ([]Container, error) {
		return []Container{{Name: "new", Memory: 32 << 20, MilliCPU: 1000}}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCPURequestsCannotOverbook(t *testing.T) {
	m := manager(t)
	m.Policy.TotalCPU = "1"
	m.Policy.MaxActive = 10
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	results := make(chan error, 2)
	for _, name := range []string{"a", "b"} {
		next := *m
		go func() { results <- next.ReserveCPU(ctx, name, 32<<20, 1000, empty) }()
	}
	admitted := 0
	for range 2 {
		if err := <-results; err == nil {
			admitted++
		} else if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if admitted != 1 {
		t.Fatal(admitted)
	}
}

func TestAtomicGroupCountAndInventory(t *testing.T) {
	m := manager(t)
	m.Policy.MaxActive = 3
	ctx := context.Background()
	if err := m.ReserveGroup(ctx, "group", 64<<20, 1000, 2, empty); err != nil {
		t.Fatal(err)
	}
	inventory := func(context.Context) ([]Container, error) {
		return []Container{{Name: "group", Memory: 64 << 20, MilliCPU: 1000, Count: 2}}, nil
	}
	if err := m.ReserveGroup(ctx, "group", 64<<20, 1000, 2, inventory); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	if err := m.ReserveGroup(wait, "another", 64<<20, 1000, 2, inventory); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	slots, err := m.load(ctx)
	if err != nil || len(slots) != 1 || slots["group"].Count != 2 {
		t.Fatal(slots, err)
	}
}
