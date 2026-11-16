package taskrun

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type settleGraphStore struct {
	storage.Store
	graph *domain.Graph
	err   error
}

func (s settleGraphStore) GetGraph(context.Context, string, int) (*domain.Graph, error) {
	return s.graph, s.err
}
func TestSettlementPreservesHistoricalGraphsAndFailsClosed(t *testing.T) {
	historical := &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-run-old", Labels: map[string]string{domain.LabelTask: "tsk_old"}}}
	m := Manager{Store: settleGraphStore{graph: historical}}
	// No engine or writable store: legacy copies must not reach any mutation.
	if err := m.Settle(t.Context(), historical.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("graph unavailable")
	m.Store = settleGraphStore{err: unavailable}
	if err := m.Settle(t.Context(), historical.Metadata.ID); !errors.Is(err, unavailable) {
		t.Fatal(err)
	}
	historical.Metadata.Labels["weft.task.lifecycle"] = "settled"
	historical.Metadata.Labels[domain.LabelTask] = "tsk_other"
	m.Store = settleGraphStore{graph: historical}
	if err := m.Settle(t.Context(), historical.Metadata.ID); err == nil {
		t.Fatal("invalid ownership accepted")
	}
}
