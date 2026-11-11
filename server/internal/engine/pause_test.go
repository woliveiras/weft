package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type pauseStore struct {
	storage.Store
	record *domain.GraphRecord
	err    error
}

func (s pauseStore) GetGraphRecord(context.Context, string) (*domain.GraphRecord, error) {
	return s.record, s.err
}

func TestPauseUsesDurableStateBeforeReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		desired                  domain.GraphState
		cached, node, fail, want bool
	}{
		{name: "persisted pause", desired: domain.GraphPaused, want: true},
		{name: "persisted resume", desired: domain.GraphRunning, cached: true},
		{name: "node pause", desired: domain.GraphRunning, node: true, want: true},
		{name: "stop pending", desired: domain.GraphStopped, want: true},
		{name: "storage failure", fail: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := pauseStore{record: &domain.GraphRecord{DesiredState: tc.desired}}
			if tc.fail {
				s.err = errors.New("unavailable")
			}
			gr := &graphRun{e: &Engine{store: s}, g: &domain.Graph{}, paused: tc.cached}
			r := &replica{run_: gr, nodePaused: tc.node}
			got, err := r.paused(t.Context())
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("paused=%v err=%v", got, err)
			}
		})
	}
}
