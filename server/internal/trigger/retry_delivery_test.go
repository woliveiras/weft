package trigger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type retryEventFailure struct{ storage.Store }

func (retryEventFailure) ListEvents(context.Context, storage.EventFilter) ([]*domain.Event, error) {
	return nil, errors.New("event store unavailable")
}

func TestUndeliveredRetryFencesRunningDeliveredAndUncertainTasks(t *testing.T) {
	for _, scenario := range []string{"eligible", "running", "wrong-owner", "nonterminal", "approval", "delivered", "store-failure", "no-executions"} {
		t.Run(scenario, func(t *testing.T) {
			st, err := fsstore.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			d := &taskconfig.Draft{ID: "tsk_old", ExecutionGraphID: "task-run-old", Status: "blocked"}
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: d.ExecutionGraphID, Labels: map[string]string{domain.LabelTask: d.ID}}}
			if scenario == "wrong-owner" {
				g.Metadata.Labels[domain.LabelTask] = "tsk_other"
			}
			if _, err := st.SaveGraph(t.Context(), g, 0); err != nil {
				t.Fatal(err)
			}
			rec := &domain.GraphRecord{ID: d.ExecutionGraphID, Version: 1, State: domain.GraphStopped, DesiredState: domain.GraphStopped}
			if scenario == "running" {
				rec.DesiredState = domain.GraphRunning
			}
			if err := st.PutGraphRecord(t.Context(), rec); err != nil {
				t.Fatal(err)
			}
			x := &domain.Execution{ID: "exe_old", GraphID: d.ExecutionGraphID, Status: domain.ExecFailed, StartedAt: time.Now()}
			if scenario == "nonterminal" {
				x.Status = domain.ExecRunning
			}
			if scenario != "no-executions" {
				if err := st.PutExecution(t.Context(), x); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "approval" {
				if err := st.PutApproval(t.Context(), &domain.Approval{ID: "apr_old", GraphID: d.ExecutionGraphID, Status: domain.ApprovalPending}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "delivered" {
				if err := st.AppendEvent(t.Context(), domain.NewEvent(domain.EvPullRequestOpened, d.ExecutionGraphID, map[string]any{"url": "https://example.test/pr/1"})); err != nil {
					t.Fatal(err)
				}
			}
			m := Manager{Store: st}
			if scenario == "store-failure" {
				m.Store = retryEventFailure{st}
			}
			err = m.checkUndeliveredTask(t.Context(), d)
			if (err == nil) != (scenario == "eligible") {
				t.Fatalf("%s: %v", scenario, err)
			}
		})
	}
}
