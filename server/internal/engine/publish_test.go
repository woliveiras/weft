package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
)

type publicationStore struct{ storage.Store }

func (*publicationStore) RecordMessage(context.Context, *domain.MessageRecord) error { return nil }
func (*publicationStore) AppendEvent(context.Context, *domain.Event) error           { return nil }

type publicationBroker struct {
	queue.Broker
	calls  int
	failAt int
	err    error
}

func (b *publicationBroker) Publish(context.Context, string, *domain.Message) error {
	b.calls++
	if b.calls == b.failAt {
		return b.err
	}
	return nil
}

func TestEmitPublicationOutcome(t *testing.T) {
	failure := errors.New("broker unavailable")
	for _, tc := range []struct {
		name               string
		failAt, calls, ids int
		payload            json.RawMessage
		wantErr            bool
	}{
		{"success", 0, 3, 1, json.RawMessage(`{}`), false},
		{"first channel fails", 1, 1, 0, json.RawMessage(`{}`), true},
		{"partial publication", 2, 2, 1, json.RawMessage(`{}`), true},
		{"invalid payload", 0, 0, 0, json.RawMessage(`{`), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			broker := &publicationBroker{failAt: tc.failAt, err: failure}
			store := &publicationStore{}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			e := &Engine{opt: Options{Broker: broker}, store: store, bus: events.NewBus(store, log), log: log}
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "unit"}, Spec: domain.GraphSpec{MaxHops: 10,
				Nodes: []domain.Node{{ID: "producer"}}, Channels: []domain.Channel{
					{ID: "one", From: "producer"}, {ID: "two", From: "producer"}, {ID: "three", From: "producer"},
				}}}
			gr, err := newGraphRun(e, g, "run")
			if err != nil {
				t.Fatal(err)
			}
			ids, err := e.emit(context.Background(), gr, &g.Spec.Nodes[0], 0, &domain.Execution{ID: "execution"}, nil, runtime.Emit{Type: "result", Payload: tc.payload})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if tc.failAt > 0 && !errors.Is(err, failure) {
				t.Fatalf("lost publication cause: %v", err)
			}
			if broker.calls != tc.calls || len(ids) != tc.ids {
				t.Fatalf("calls=%d ids=%v; want %d calls, %d ids", broker.calls, ids, tc.calls, tc.ids)
			}
		})
	}
}
