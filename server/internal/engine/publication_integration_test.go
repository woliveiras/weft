package engine_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Only the output failure is injected. Input leasing, redelivery, successful
// publication, consumption and persistence use the real broker and SQLite.
type failingOutputBroker struct {
	queue.Broker
	subject   string
	recovered atomic.Bool
}

func (b *failingOutputBroker) Publish(ctx context.Context, subject string, m *domain.Message) error {
	if subject == b.subject && !b.recovered.Load() {
		return errors.New("injected output outage")
	}
	return b.Broker.Publish(ctx, subject, m)
}

const publicationGraph = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: publication}
spec:
  defaults: {runtime: {kind: echo}}
  nodes:
    - {id: producer}
    - {id: first}
    - {id: second}
  channels:
    - {id: in, to: [producer], maxDeliver: 5}
    - {id: first, from: producer, to: [first]}
    - {id: second, from: producer, to: [second]}
`

func TestPublicationFailureRedeliversBeforeAcknowledgement(t *testing.T) {
	for _, channel := range []string{"first", "second"} {
		t.Run(channel, func(t *testing.T) {
			dir := t.TempDir()
			b := &failingOutputBroker{Broker: memory.New(), subject: queue.Subject("publication", channel)}
			r := newRig(t, openStore(t, dir), b, dir, "i1")
			r.deploy(publicationGraph)
			input := r.inject("publication", "in", "job", map[string]int{"value": 1})
			eventually(t, 5*time.Second, "publication failure persisted", func() bool {
				xs, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "publication", NodeID: "producer", Status: domain.ExecFailed})
				if err != nil {
					t.Fatal(err)
				}
				return len(xs) > 0 && strings.Contains(xs[0].Error, "injected output outage")
			})
			if r.acked("publication", "in") != 0 {
				t.Fatal("input acknowledged during publication failure")
			}
			processed, err := r.store.IsProcessed(r.ctx, queue.QueueGroup("in"), input.ID)
			if err != nil || processed {
				t.Fatalf("processed=%v err=%v before publication", processed, err)
			}
			if channel == "second" {
				xs, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "publication", NodeID: "producer", Status: domain.ExecFailed})
				if err != nil {
					t.Fatal(err)
				}
				if len(xs[0].OutputIDs) != 1 {
					t.Fatal("partial output missing from failed execution")
				}
			}
			b.recovered.Store(true)
			eventually(t, 10*time.Second, "redelivered input and both outputs consumed", func() bool {
				return r.acked("publication", "in") == 1 && r.acked("publication", "first") >= 1 && r.acked("publication", "second") == 1
			})
			records, err := r.store.GetMessage(r.ctx, input.ID)
			if err != nil {
				t.Fatal(err)
			}
			retried := false
			for _, rec := range records {
				if rec.Status == domain.MsgAcked && rec.Attempt > 1 {
					retried = true
				}
			}
			if !retried {
				t.Fatal("input did not complete through broker redelivery")
			}
		})
	}
}

func TestPublicationFailureDeadLettersWithoutAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	b := &failingOutputBroker{Broker: memory.New(), subject: queue.Subject("publication", "first")}
	r := newRig(t, openStore(t, dir), b, dir, "i1")
	r.deploy(strings.Replace(publicationGraph, "maxDeliver: 5", "maxDeliver: 2", 1))
	input := r.inject("publication", "in", "job", map[string]int{"value": 1})
	eventually(t, 10*time.Second, "failed publication reaches delivery limit", func() bool {
		records, err := r.store.GetMessage(r.ctx, input.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range records {
			if rec.Status == domain.MsgDeadLettered && rec.Attempt == 2 {
				xs, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "publication", NodeID: "producer", Status: domain.ExecFailed})
				if err != nil {
					t.Fatal(err)
				}
				return len(xs) == 2
			}
		}
		return false
	})
	if r.acked("publication", "in") != 0 {
		t.Fatal("dead-lettered input was acknowledged")
	}
	processed, err := r.store.IsProcessed(r.ctx, queue.QueueGroup("in"), input.ID)
	if err != nil || processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	xs, err := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: "publication", NodeID: "producer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(xs) != 2 {
		t.Fatalf("got %d executions, want 2", len(xs))
	}
	for _, x := range xs {
		if x.Status != domain.ExecFailed {
			t.Fatalf("unexpected status %s", x.Status)
		}
	}
}
