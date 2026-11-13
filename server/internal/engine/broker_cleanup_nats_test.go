package engine_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/eventtest"
	"github.com/woliveiras/weft/server/internal/queue/natsq"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestGraphIDReuseRecoversNATSBrokerCleanup(t *testing.T) {
	url := os.Getenv("WEFT_TEST_NATS_URL")
	if url == "" {
		t.Skip("WEFT_TEST_NATS_URL not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	connect := func() *natsq.Broker {
		t.Helper()
		b, err := natsq.New(ctx, natsq.Options{URL: url})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { b.Close() })
		if err := b.Ping(ctx); err != nil {
			t.Fatalf("JetStream unavailable: %v", err)
		}
		return b
	}
	broker, healthy := connect(), connect()
	dir := t.TempDir()
	store := openStore(t, dir)
	r := newRig(t, store, broker, dir, "nats-cleanup")
	var g domain.Graph
	if err := yaml.Unmarshal([]byte(pipeline), &g); err != nil {
		t.Fatal(err)
	}
	id := "cleanup-" + domain.NewID("")
	g.Metadata.ID = id
	subject := id + ".in"
	if _, _, err := r.eng.SaveGraph(ctx, &g, 0, "test"); err != nil {
		t.Fatal(err)
	}
	r.inject(id, "in", "job", map[string]string{"generation": "old"})

	// Break the engine's real NATS connection while keeping JetStream
	// and the original durable consumer available to the observer.
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	if err := broker.DeleteGroup(ctx, subject, "in"); !errors.Is(err, nats.ErrConnectionClosed) {
		t.Fatalf("expected real NATS connection failure, got %v", err)
	}
	if err := r.eng.WithGraphTransition(ctx, id, func(ctx context.Context) error {
		return r.eng.DeleteGraph(ctx, id, "test")
	}); err != nil {
		t.Fatalf("delete graph with failed broker cleanup: %v", err)
	}
	if _, _, err := r.eng.GetGraph(ctx, id, 0); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("deleted graph is still accessible: %v", err)
	}
	if _, _, err := r.eng.SaveGraph(ctx, &g, 0, "test"); !errors.Is(err, engine.ErrConflict) ||
		!strings.Contains(err.Error(), "pending broker cleanup") {
		t.Fatalf("recreate with failed broker cleanup: %v", err)
	}
	stats, err := healthy.Stats(ctx, subject, "in")
	if err != nil || stats.Pending != 1 {
		t.Fatalf("original JetStream consumer should still have one pending message: %+v, %v", stats, err)
	}
	t.Log("graph deleted; NATS connection failure blocked ID reuse while the original consumer retained its message")

	r.eng.Shutdown(ctx)
	recovered := newRig(t, store, healthy, dir, "nats-recovered")
	eventually(t, 6*time.Second, "NATS broker cleanup before ID reuse", func() bool {
		events, err := store.ListEvents(ctx, storage.EventFilter{GraphID: id, Types: []domain.EventType{domain.EvGraphBrokerCleanupCompleted}})
		if err != nil {
			t.Fatal(err)
		}
		return len(events) == 1
	})
	stats, err = healthy.Stats(ctx, subject, "in")
	if err != nil || stats.Pending != 0 || stats.InFlight != 0 {
		t.Fatalf("orphaned consumer retained old work: %+v, %v", stats, err)
	}
	if _, _, err := recovered.eng.SaveGraph(ctx, &g, 0, "test"); err != nil {
		t.Fatalf("recreate after restoring broker connectivity: %v", err)
	}
	stats, err = healthy.Stats(ctx, subject, "in")
	if err != nil || stats.Pending != 0 || stats.InFlight != 0 {
		t.Fatalf("recreated consumer inherited old work: %+v, %v", stats, err)
	}
	sub, err := healthy.Subscribe(ctx, subject, "in", "cleanup-test")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	newMessage := recovered.inject(id, "in", "job", map[string]string{"generation": "new"})
	deliveries, err := sub.Fetch(ctx, 1, 3*time.Second)
	if err != nil || len(deliveries) != 1 || deliveries[0].Message().ID != newMessage.ID {
		t.Fatalf("expected only the new message: %d deliveries, %v", len(deliveries), err)
	}
	if err := deliveries[0].Ack(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("new engine reused the ID, found an empty JetStream consumer and received only the new message")
	eventtest.Capture(t, store, domain.EvGraphBrokerCleanupCompleted)
}
