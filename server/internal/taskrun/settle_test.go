package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type settleGraphStore struct {
	storage.Store
	graph   *domain.Graph
	err     error
	task    *taskconfig.Draft
	taskErr error
}

func (s settleGraphStore) GetGraph(context.Context, string, int) (*domain.Graph, error) {
	return s.graph, s.err
}
func (s settleGraphStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	if s.taskErr != nil {
		return nil, s.taskErr
	}
	raw, err := json.Marshal(s.task)
	return &storage.Setting{Value: raw}, err
}
func TestSettlementPreservesHistoricalGraphsAndFailsClosed(t *testing.T) {
	historical := &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-run-old", Labels: map[string]string{domain.LabelTask: "tsk_old"}}}
	m := Manager{Store: settleGraphStore{graph: historical, task: &taskconfig.Draft{ID: "tsk_old", Status: "running"}}}
	// No engine or writable store: legacy copies must not reach any mutation.
	if err := m.Settle(t.Context(), historical.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("graph unavailable")
	m.Store = settleGraphStore{err: unavailable}
	if err := m.Settle(t.Context(), historical.Metadata.ID); !errors.Is(err, unavailable) {
		t.Fatal(err)
	}
	m.Store = settleGraphStore{graph: historical, taskErr: unavailable}
	if err := m.Settle(t.Context(), historical.Metadata.ID); !errors.Is(err, unavailable) {
		t.Fatal(err)
	}
	m.Store = settleGraphStore{graph: historical, task: &taskconfig.Draft{ID: "tsk_old", Status: "cancel_requested", ExecutionGraphID: "task-run-other"}}
	if err := m.Settle(t.Context(), historical.Metadata.ID); err == nil {
		t.Fatal("invalid task binding accepted")
	}
	historical.Metadata.Labels["weft.task.lifecycle"] = "settled"
	historical.Metadata.Labels[domain.LabelTask] = "tsk_other"
	m.Store = settleGraphStore{graph: historical}
	if err := m.Settle(t.Context(), historical.Metadata.ID); err == nil {
		t.Fatal("invalid ownership accepted")
	}
}

func TestCancellationRequiresConfirmedStopAndCompleteHistory(t *testing.T) {
	for _, scenario := range []string{"settled", "no-executions", "running", "desired-running", "execution-active", "approval-pending", "bounded-history"} {
		t.Run(scenario, func(t *testing.T) {
			rec := &domain.GraphRecord{State: domain.GraphStopped, DesiredState: domain.GraphStopped}
			v := &View{Executions: []*domain.Execution{{Status: domain.ExecSucceeded}}}
			switch scenario {
			case "no-executions":
				v.Executions = nil
			case "running":
				rec.State = domain.GraphRunning
			case "desired-running":
				rec.DesiredState = domain.GraphRunning
			case "execution-active":
				v.Executions[0].Status = domain.ExecRunning
			case "approval-pending":
				v.PendingApprovals = []*domain.Approval{{Status: domain.ApprovalPending}}
			case "bounded-history":
				v.Executions = make([]*domain.Execution, 1000)
			}
			if got, want := cancellationConfirmed(rec, v), scenario == "settled" || scenario == "no-executions"; got != want {
				t.Fatalf("confirmed=%t, want %t", got, want)
			}
		})
	}
}
