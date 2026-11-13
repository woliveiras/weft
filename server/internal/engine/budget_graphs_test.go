package engine

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestBudgetGraphOutcome(t *testing.T) {
	failure := errors.New("store unavailable")
	for _, tc := range []struct {
		name, action, runID, want string
		state                     domain.GraphState
		err                       error
	}{
		{name: "stopped pause", action: cost.PauseGraph, state: domain.GraphStopped, want: "stopped"},
		{name: "stopping pause", action: cost.PauseGraph, state: domain.GraphStopping, want: "stopped"},
		{name: "stopped resume", action: cost.ResumeGraph, state: domain.GraphStopped, want: "stopped"},
		{name: "deleted resume", action: cost.ResumeGraph, err: storage.ErrNotFound, want: "deleted"},
		{name: "replacement run", action: cost.ResumeGraph, state: domain.GraphPaused, runID: "old", want: "run_changed"},
		{name: "store failure", action: cost.ResumeGraph, err: failure},
		{name: "active run", action: cost.PauseGraph, state: domain.GraphRunning, want: "pause"},
		{name: "already paused", action: cost.PauseGraph, state: domain.GraphPaused, want: "paused"},
		{name: "paused run", action: cost.ResumeGraph, state: domain.GraphPaused, want: "resume"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := cost.BudgetGraph{GraphAction: cost.GraphAction{Action: tc.action, RunID: tc.runID}}
			rec := &domain.GraphRecord{DesiredState: tc.state, RunID: "current"}
			got, err := budgetGraphOutcome(target, rec, tc.err)
			if got != tc.want || (tc.err == failure && !errors.Is(err, failure)) || (tc.err != failure && err != nil) {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestBudgetPauseWithStoppedOrLeasedGraph(t *testing.T) {
	for _, state := range []string{"stopped", "stopping", "leased"} {
		t.Run(state, func(t *testing.T) {
			ctx := t.Context()
			st, err := fsstore.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			log := slog.New(slog.DiscardHandler)
			e := &Engine{store: st, log: log, bus: events.NewBus(st, log)}
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-run-budget-fixture"}}
			id := g.Metadata.ID
			if _, err := st.SaveGraph(ctx, g, 0); err != nil {
				t.Fatal(err)
			}
			rec := &domain.GraphRecord{ID: id, Version: 1, RunID: "run-a", DesiredState: domain.GraphRunning, State: domain.GraphRunning}
			if state != "leased" {
				rec.DesiredState, rec.State = domain.GraphStopped, domain.GraphState(state)
			}
			if err := st.PutGraphRecord(ctx, rec); err != nil {
				t.Fatal(err)
			}
			// Seed a reached ledger; exercise the real turn pause path, graph
			// transition lease, durable settings and recovery in one store.
			if _, err := st.PutSetting(ctx, cost.BudgetPrefix+"run/"+id,
				[]byte(`{"scope":"run","id":"task-run-budget-fixture","currency":"USD","limit":1,"reached":true,"entries":{}}`), 0, "fixture"); err != nil {
				t.Fatal(err)
			}
			key := "graph-transition/" + id
			if state == "leased" {
				if ok, err := st.AcquireLease(ctx, key, "other-controller", time.Minute); err != nil || !ok {
					t.Fatalf("hold transition: %t %v", ok, err)
				}
			}
			turn := &turnEnv{gr: &graphRun{e: e, g: g}, x: &domain.Execution{RunID: rec.RunID}}
			if err := turn.pauseForBudget(ctx, cost.TaskRun, id, 1); err != nil {
				t.Fatalf("pause added a turn error: %v", err)
			}
			b := e.monetaryBudgets()
			v, err := b.Ledger(ctx, cost.TaskRun, id)
			if err != nil || v.Paused != (state == "leased") {
				t.Fatalf("ledger after pause attempt: %+v %v", v, err)
			}
			if state == "leased" {
				if got, err := st.GetGraphRecord(ctx, id); err != nil || got.DesiredState != domain.GraphRunning {
					t.Fatalf("lease did not protect graph: %+v %v", got, err)
				}
				if err := st.ReleaseLease(ctx, key, "other-controller"); err != nil {
					t.Fatal(err)
				}
				e.reconcileBudgetGraphs(ctx)
				if got, err := st.GetGraphRecord(ctx, id); err != nil || got.DesiredState != domain.GraphPaused {
					t.Fatalf("persisted pause was not applied: %+v %v", got, err)
				}
				if v, err := b.Ledger(ctx, cost.TaskRun, id); err != nil || !v.Paused || len(v.NotResumed) != 0 {
					t.Fatalf("pause was not retained: %+v %v", v, err)
				}
			}
			events, err := st.ListEvents(ctx, storage.EventFilter{GraphID: id, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
			want := `"outcome":"stopped"`
			if state == "leased" {
				want = `"outcome":"paused"`
			}
			if err != nil || len(events) != 1 || events[0].Actor != "system:budget" || !bytes.Contains(events[0].Data, []byte(want)) {
				t.Fatalf("expected transition audit %s: %+v %v", want, events, err)
			}
		})
	}
}
