package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
)

type reviewDispatchUnitStore struct {
	storage.Store
	record     *domain.GraphRecord
	readErr    error
	control    []byte
	controlErr error
}

func (s *reviewDispatchUnitStore) GetGraphRecord(context.Context, string) (*domain.GraphRecord, error) {
	return s.record, s.readErr
}
func (*reviewDispatchUnitStore) AcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (*reviewDispatchUnitStore) ReleaseLease(context.Context, string, string) error { return nil }
func (s *reviewDispatchUnitStore) KVGet(context.Context, string, string) ([]byte, error) {
	if s.controlErr != nil {
		return nil, s.controlErr
	}
	if s.control == nil {
		return nil, storage.ErrNotFound
	}
	return s.control, nil
}
func (*reviewDispatchUnitStore) RecordMessage(context.Context, *domain.MessageRecord) error {
	return nil
}
func (*reviewDispatchUnitStore) AppendEvent(context.Context, *domain.Event) error { return nil }

type reviewDispatchUnitBroker struct {
	queue.Broker
	messages []*domain.Message
	err      error
}

func (b *reviewDispatchUnitBroker) Publish(_ context.Context, _ string, m *domain.Message) error {
	b.messages = append(b.messages, m.Clone())
	return b.err
}

func reviewDispatchUnitEngine(t *testing.T) (*Engine, *graphRun, *reviewDispatchUnitStore, *reviewDispatchUnitBroker, ReviewDispatch) {
	t.Helper()
	store := &reviewDispatchUnitStore{record: &domain.GraphRecord{ID: "review", Version: 1, RunID: "run-review", State: domain.GraphRunning, DesiredState: domain.GraphRunning}}
	broker := &reviewDispatchUnitBroker{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &Engine{store: store, opt: Options{Broker: broker}, log: log, bus: events.NewBus(store, log), graphs: map[string]*graphRun{}}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "review", Version: 1}, Spec: domain.GraphSpec{
		Nodes:    []domain.Node{{ID: "reviewer", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopEvent}}},
		Channels: []domain.Channel{{ID: "requests", To: []string{"reviewer"}}},
	}}
	graph.Normalize(g)
	gr, err := newGraphRun(e, g, "run-review")
	if err != nil {
		t.Fatal(err)
	}
	e.graphs["review"] = gr
	m, err := domain.NewMessage("review.requested", "ignored", "ignored", map[string]any{"number": 7})
	if err != nil {
		t.Fatal(err)
	}
	in := ReviewDispatch{GraphID: "review", Version: 1, Channel: "requests", Node: "reviewer", ExpectedRunID: "run-review", Message: m}
	return e, gr, store, broker, in
}

func TestDispatchReviewValidatesExactDestinationBeforePublication(t *testing.T) {
	dependency := errors.New("store unavailable")
	for _, tc := range []struct {
		name string
		edit func(*Engine, *graphRun, *reviewDispatchUnitStore, *ReviewDispatch)
		want error
	}{
		{"stopped", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) {
			s.record.DesiredState = domain.GraphStopped
		}, ErrReviewDispatchInactive},
		{"paused", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) {
			s.record.DesiredState = domain.GraphPaused
		}, ErrReviewDispatchInactive},
		{"observed failed", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) {
			s.record.State = domain.GraphFailed
		}, ErrReviewDispatchInactive},
		{"record version changed", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) { s.record.Version++ }, ErrReviewDispatchStale},
		{"record run changed", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) {
			s.record.RunID = "run-other"
		}, ErrReviewDispatchStale},
		{"loaded version differs", func(_ *Engine, gr *graphRun, _ *reviewDispatchUnitStore, _ *ReviewDispatch) { gr.version++ }, ErrReviewDispatchStale},
		{"loaded run differs", func(_ *Engine, gr *graphRun, _ *reviewDispatchUnitStore, _ *ReviewDispatch) { gr.runID = "run-other" }, ErrReviewDispatchStale},
		{"not loaded", func(e *Engine, _ *graphRun, _ *reviewDispatchUnitStore, _ *ReviewDispatch) {
			delete(e.graphs, "review")
		}, ErrReviewDispatchStale},
		{"node is stopped", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) {
			s.control = []byte(`{"stopped":true}`)
		}, ErrReviewDispatchInactive},
		{"node is paused", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) {
			s.control = []byte(`{"paused":true}`)
		}, ErrReviewDispatchInactive},
		{"record failure", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) { s.readErr = dependency }, dependency},
		{"node control failure", func(_ *Engine, _ *graphRun, s *reviewDispatchUnitStore, _ *ReviewDispatch) { s.controlErr = dependency }, dependency},
		{"non-event node", func(_ *Engine, gr *graphRun, _ *reviewDispatchUnitStore, _ *ReviewDispatch) {
			gr.g.Spec.Nodes[0].Loop.Mode = domain.LoopOnce
		}, ErrInvalid},
		{"disabled node", func(_ *Engine, gr *graphRun, _ *reviewDispatchUnitStore, _ *ReviewDispatch) {
			gr.g.Spec.Nodes[0].Disabled = true
		}, ErrInvalid},
		{"another channel consumer", func(_ *Engine, gr *graphRun, _ *reviewDispatchUnitStore, _ *ReviewDispatch) {
			gr.g.Spec.Channels[0].To = []string{"other"}
		}, ErrInvalid},
		{"multiple channel consumers", func(_ *Engine, gr *graphRun, _ *reviewDispatchUnitStore, _ *ReviewDispatch) {
			gr.g.Spec.Channels[0].To = []string{"reviewer", "other"}
		}, ErrInvalid},
		{"missing run pin", func(_ *Engine, _ *graphRun, _ *reviewDispatchUnitStore, in *ReviewDispatch) { in.ExpectedRunID = "" }, ErrInvalid},
		{"missing message", func(_ *Engine, _ *graphRun, _ *reviewDispatchUnitStore, in *ReviewDispatch) { in.Message = nil }, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, gr, store, broker, in := reviewDispatchUnitEngine(t)
			tc.edit(e, gr, store, &in)
			run, err := e.DispatchReview(t.Context(), in)
			if !errors.Is(err, tc.want) || run != "" || len(broker.messages) != 0 {
				t.Fatalf("run=%q error=%v published=%d", run, err, len(broker.messages))
			}
		})
	}
}

func TestDispatchReviewPreservesStableEnvelopeAndPublicationErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "broker failure"}[fail], func(t *testing.T) {
			e, _, _, broker, in := reviewDispatchUnitEngine(t)
			dependency := errors.New("broker unavailable")
			if fail {
				broker.err = dependency
			}
			in.Message.SetMeta(domain.MetaGraphVersion, 99)
			in.Message.SetMeta(domain.MetaRunID, "untrusted-run")
			run, err := e.DispatchReview(t.Context(), in)
			if (fail && !errors.Is(err, dependency)) || (!fail && (err != nil || run != in.ExpectedRunID)) || len(broker.messages) != 1 {
				t.Fatalf("run=%q error=%v published=%d", run, err, len(broker.messages))
			}
			got := broker.messages[0]
			if got.ID != in.Message.ID || string(got.Payload) != string(in.Message.Payload) || got.Target != in.Node || got.Source != "system:review-router" || got.Metadata[reviewDispatchMeta] != true || got.MetaInt(domain.MetaGraphVersion) != in.Version || got.MetaString(domain.MetaRunID) != in.ExpectedRunID {
				t.Fatalf("incorrect dispatch envelope: %+v", got)
			}
			if in.Message.Target != "ignored" || in.Message.MetaString(domain.MetaRunID) != "untrusted-run" {
				t.Fatal("caller envelope mutated")
			}
		})
	}
}

func TestRuntimeEmitCannotForgeReviewDispatch(t *testing.T) {
	e, gr, _, broker, _ := reviewDispatchUnitEngine(t)
	gr.g.Spec.Channels[0].From = "reviewer"
	_, err := e.emit(t.Context(), gr, &gr.g.Spec.Nodes[0], 0, &domain.Execution{ID: "execution"}, nil,
		runtime.Emit{Type: "review.requested", Channel: "requests", Metadata: map[string]any{reviewDispatchMeta: true}})
	if err != nil || len(broker.messages) != 1 || broker.messages[0].Metadata[reviewDispatchMeta] != nil {
		t.Fatalf("runtime output forged internal review metadata: %v %+v", err, broker.messages)
	}
}
