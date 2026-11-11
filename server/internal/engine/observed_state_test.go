package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type observedStore struct {
	storage.Store
	rec    *domain.GraphRecord
	err    error
	writes int
}

func (s *observedStore) GetGraphRecord(context.Context, string) (*domain.GraphRecord, error) {
	c := *s.rec
	return &c, nil
}
func (s *observedStore) UpdateGraphState(_ context.Context, snapshot *domain.GraphRecord, state domain.GraphState) error {
	s.writes++
	if snapshot.State != domain.GraphStopped {
		panic("snapshot mutated before compare")
	}
	return s.err
}
func TestObservedStatePreservesSnapshotAndPropagatesFailure(t *testing.T) {
	for _, err := range []error{nil, storage.ErrConflict, storage.ErrNotFound, errors.New("storage unavailable")} {
		s := &observedStore{err: err}
		e := &Engine{store: s, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		rec := &domain.GraphRecord{ID: "g", State: domain.GraphStopped, DesiredState: domain.GraphStopped}
		if e.updateObservedState(t.Context(), rec, domain.GraphRunning) != (err == nil) {
			t.Fatal("failure treated as success")
		}
		if rec.State != domain.GraphStopped {
			t.Fatal("caller snapshot mutated")
		}
	}
}
func TestOldRunCannotPublishRunningState(t *testing.T) {
	for _, tc := range []struct {
		desired domain.GraphState
		version int
		runID   string
	}{
		{domain.GraphStopped, 1, "run1"}, {domain.GraphRunning, 2, "run1"}, {domain.GraphRunning, 1, "run2"},
	} {
		s := &observedStore{rec: &domain.GraphRecord{ID: "g", State: domain.GraphStopped, DesiredState: tc.desired, Version: tc.version, RunID: tc.runID}}
		g := &domain.Graph{}
		g.Metadata.ID = "g"
		r := &graphRun{e: &Engine{store: s}, g: g, version: 1, runID: "run1"}
		r.updateRecord(t.Context())
		if s.writes != 0 {
			t.Fatal("old run wrote observed state")
		}
	}
}
