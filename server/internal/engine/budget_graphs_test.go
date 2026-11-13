package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
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

func budgetTransitionFixture(t *testing.T, wrap func(storage.Store) storage.Store) (*Engine, *domain.GraphRecord) {
	t.Helper()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var store storage.Store = st
	if wrap != nil {
		store = wrap(store)
	}
	log := slog.New(slog.DiscardHandler)
	e := &Engine{store: store, log: log, bus: events.NewBus(store, log)}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-run-budget-fixture",
		Labels: map[string]string{domain.LabelTaskProject: "project-a"}},
		Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}
	rec := &domain.GraphRecord{ID: g.Metadata.ID, Version: 1, RunID: "run-a",
		DesiredState: domain.GraphPaused, State: domain.GraphPaused}
	if _, err := store.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.PutGraphRecord(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
	return e, rec
}

func putTransitionLedger(t *testing.T, e *Engine, l cost.Ledger) {
	t.Helper()
	if l.Entries == nil {
		l.Entries = make(map[string]cost.Entry)
	}
	raw, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	key := cost.BudgetPrefix + l.Scope + "/" + l.ID
	var revision int64
	if existing, err := e.store.GetSetting(t.Context(), key); err == nil {
		revision = existing.Revision
	} else if !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := e.store.PutSetting(t.Context(), key, raw, revision, "fixture"); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetResumeChecksLatestApplicableLedgers(t *testing.T) {
	for _, scenario := range []string{"project", "insufficient_raise", "installation_reached_after_raise"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			e, rec := budgetTransitionFixture(t, nil)
			b := e.monetaryBudgets()
			now := time.Now()
			month := cost.Period(now)
			l := cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Currency: "USD", Limit: 10, Reached: true,
				PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{
					rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID},
				}}
			var blockingScope, blockingID string
			switch scenario {
			case "project":
				blockingScope, blockingID = cost.Project, cost.MonthlyID("project-a", month)
				if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Project, ID: "project-a", LimitMicros: 25}}, "admin", 0); err != nil {
					t.Fatal(err)
				}
				// Actual spend reaches the limit without a previous refusal.
				putTransitionLedger(t, e, cost.Ledger{Scope: blockingScope, ID: blockingID, Limit: 25, Compacted: 25})
			case "insufficient_raise":
				blockingScope, blockingID = l.Scope, l.ID
				l.Compacted = 25
			case "installation_reached_after_raise":
				l.Scope, l.ID = cost.Installation, cost.MonthlyID("*", month)
				blockingScope, blockingID = l.Scope, l.ID
				if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Installation, ID: "*", LimitMicros: 10}}, "admin", 0); err != nil {
					t.Fatal(err)
				}
			}
			putTransitionLedger(t, e, l)
			if scenario == "installation_reached_after_raise" {
				lease := "graph-transition/" + rec.ID
				if ok, err := e.store.AcquireLease(ctx, lease, "fixture", time.Minute); err != nil || !ok {
					t.Fatalf("hold resume lease: %t %v", ok, err)
				}
				v, err := e.RaiseBudget(ctx, l.Scope, l.ID, 20, "admin")
				if err != nil || !slices.Equal(v.NotResumed, []string{rec.ID}) {
					t.Fatalf("schedule pending resume: %+v %v", v, err)
				}
				// A different graph then reaches the raised installation limit
				// through the same reservation path used before dispatch.
				catalogue := func(context.Context) (cost.Catalogue, error) {
					return cost.Catalogue{Currency: "USD", Prices: []cost.Price{
						{Provider: "fixture", Model: "*", OutputMicrosPerMTok: 1_000_000},
					}}, nil
				}
				if _, err := b.Reserve(ctx, cost.Target{Graph: "other-graph"}, catalogue, "fixture", "openai",
					&llm.ChatRequest{Model: "fixture", MaxTokens: 21}); !errors.Is(err, cost.ErrBudgetReached) {
					t.Fatalf("other graph should reach the raised limit: %v", err)
				}
				actions, _, err := b.PendingGraphActions(ctx)
				if err != nil || len(actions) != 1 {
					t.Fatalf("load pending action: %+v %v", actions, err)
				}
				// Advance the persisted retry timestamp instead of sleeping.
				action := actions[0].GraphAction
				action.Raised = now.Add(-time.Minute)
				l.Limit, l.RaisedBy = 20, "admin"
				l.GraphActions[rec.ID] = action
				putTransitionLedger(t, e, l)
				if err := e.store.ReleaseLease(ctx, lease, "fixture"); err != nil {
					t.Fatal(err)
				}
				e.reconcileBudgetGraphs(ctx)
			} else {
				v, err := e.RaiseBudget(ctx, l.Scope, l.ID, 20, "admin")
				if err != nil || len(v.NotResumed) != 0 || v.Paused != (scenario == "insufficient_raise") {
					t.Fatalf("raise: %+v %v", v, err)
				}
			}
			got, err := e.store.GetGraphRecord(ctx, rec.ID)
			if err != nil || got.DesiredState != domain.GraphPaused {
				t.Fatalf("reached budget was bypassed: %+v %v", got, err)
			}
			v, err := b.Ledger(ctx, blockingScope, blockingID)
			if err != nil || !v.Reached || !v.Paused || len(v.NotResumed) != 0 {
				t.Fatalf("blocking ledger should own the hold: %+v %v", v, err)
			}
			audit, err := e.store.ListEvents(ctx, storage.EventFilter{GraphID: rec.ID, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
			if err != nil || len(audit) != 1 || !bytes.Contains(audit[0].Data, []byte(`"outcome":"budget_reached"`)) ||
				!bytes.Contains(audit[0].Data, []byte(`"blockingScope":"`+blockingScope+`"`)) {
				t.Fatalf("missing scoped refusal: %v %v", audit, err)
			}
		})
	}
}

func TestReconcileBudgetRetiresInactiveHolds(t *testing.T) {
	for _, reason := range []string{"stopped", "deleted", "run_finished", "run_changed", "legacy_unbound", "period_ended"} {
		t.Run(reason, func(t *testing.T) {
			ctx := t.Context()
			e, rec := budgetTransitionFixture(t, nil)
			l := cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Currency: "USD", Limit: 10, Reached: true,
				PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{
					rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID},
				}}
			switch reason {
			case "stopped":
				rec.DesiredState = domain.GraphStopped
			case "deleted":
				if err := e.store.DeleteGraph(ctx, rec.ID); err != nil {
					t.Fatal(err)
				}
			case "run_finished":
				e.markCompleted(ctx, rec.ID, "reader", 0, rec.RunID)
			case "run_changed":
				rec.RunID = "next-run"
			case "legacy_unbound":
				l.GraphActions = nil
			case "period_ended":
				l.Scope, l.ID = cost.Installation, "*@2000-01"
			}
			if reason != "deleted" {
				if err := e.store.PutGraphRecord(ctx, rec); err != nil {
					t.Fatal(err)
				}
			}
			putTransitionLedger(t, e, l)
			e.reconcileBudgetGraphs(ctx)
			if reason == "period_ended" {
				e.reconcileBudgetGraphs(ctx) // the expired hold becomes a durable resume
				got, err := e.store.GetGraphRecord(ctx, rec.ID)
				if err != nil || got.DesiredState != domain.GraphRunning {
					t.Fatalf("fresh monthly budget should let the graph resume: %+v %v", got, err)
				}
			}
			v, err := e.monetaryBudgets().Ledger(ctx, l.Scope, l.ID)
			if err != nil || v.Paused || len(v.NotResumed) != 0 {
				t.Fatalf("hold still visible: %+v %v", v, err)
			}
			actions, _, err := e.monetaryBudgets().PendingGraphActions(ctx)
			if err != nil || len(actions) != 0 {
				t.Fatalf("hold still persisted: %+v %v", actions, err)
			}
			audit, err := e.store.ListEvents(ctx, storage.EventFilter{GraphID: rec.ID, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
			if err != nil || !slices.ContainsFunc(audit, func(ev *domain.Event) bool {
				return bytes.Contains(ev.Data, []byte(`"outcome":"`+reason+`"`))
			}) {
				t.Fatalf("missing cleanup reason %s: %v %v", reason, audit, err)
			}
		})
	}
}

type budgetDeadlineStore struct {
	storage.Store
	cancel         context.CancelFunc
	failResolve    bool
	loseWriteReply bool
}

func (s *budgetDeadlineStore) PutGraphRecord(ctx context.Context, rec *domain.GraphRecord) error {
	if err := s.Store.PutGraphRecord(ctx, rec); err != nil {
		return err
	}
	if rec.BudgetResumeID != "" {
		if s.cancel != nil {
			s.cancel()
		}
		if s.loseWriteReply {
			return context.DeadlineExceeded
		}
	}
	return nil
}

func (s *budgetDeadlineStore) PutSetting(ctx context.Context, key string, value []byte, revision int64, by string) (int64, error) {
	if s.failResolve && !bytes.Contains(value, []byte(`"pausedGraphs"`)) {
		return 0, context.DeadlineExceeded
	}
	return s.Store.PutSetting(ctx, key, value, revision, by)
}

func TestBudgetResumeReceiptSurvivesCanceledCommandAndLedgerFailure(t *testing.T) {
	inject := &budgetDeadlineStore{loseWriteReply: true}
	e, rec := budgetTransitionFixture(t, func(st storage.Store) storage.Store {
		inject.Store = st
		return inject
	})
	l := cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{
			rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID},
		}}
	putTransitionLedger(t, e, l)
	inject.failResolve = true
	v, err := e.RaiseBudget(t.Context(), l.Scope, l.ID, 20, "admin")
	if err != nil || v.Paused || len(v.NotResumed) != 0 {
		t.Fatalf("committed resume omitted from raise result: %+v %v", v, err)
	}
	audit, err := e.store.ListEvents(t.Context(), storage.EventFilter{GraphID: rec.ID, Types: []domain.EventType{domain.EvBudgetRaised}})
	if err != nil || len(audit) != 1 || !bytes.Contains(audit[0].Data, []byte(`"resumed":["`+rec.ID+`"]`)) {
		t.Fatalf("incorrect raise audit: %+v %v", audit, err)
	}
	saved, err := e.monetaryBudgets().Ledger(t.Context(), l.Scope, l.ID)
	if err != nil || saved.Paused || len(saved.NotResumed) != 0 {
		t.Fatalf("ledger reported committed resume as pending: %+v %v", saved, err)
	}
	inject.failResolve = false
	pending, _, err := e.monetaryBudgets().PendingGraphActions(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatalf("missing retry record: %+v %v", pending, err)
	}
	if outcome, err := e.waitBudgetGraphTransition(t.Context(), pending[0]); err != nil || outcome != "resumed" {
		t.Fatalf("recovery misclassified the resume: %s %v", outcome, err)
	}
	events, err := e.store.ListEvents(t.Context(), storage.EventFilter{GraphID: rec.ID, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
	if err != nil || len(events) != 1 || !bytes.Contains(events[0].Data, []byte(`"outcome":"resumed"`)) {
		t.Fatalf("expected one successful transition audit: %+v %v", events, err)
	}
}

func TestBudgetResumeFinalizesAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e, rec := budgetTransitionFixture(t, func(st storage.Store) storage.Store {
		return &budgetDeadlineStore{Store: st, cancel: cancel}
	})
	l := cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}}
	putTransitionLedger(t, e, l)
	_, _, actions, err := e.monetaryBudgets().Raise(ctx, l.Scope, l.ID, 20, "admin")
	if err != nil || len(actions) != 1 {
		t.Fatalf("schedule resume: %v %v", actions, err)
	}
	outcome, err := e.waitBudgetGraphTransition(ctx, actions[0])
	if outcome != "resumed" || err != nil || ctx.Err() == nil {
		t.Fatalf("resume after cancellation: %s %v", outcome, err)
	}
	v, err := e.monetaryBudgets().Ledger(t.Context(), l.Scope, l.ID)
	if err != nil || v.Paused || len(v.NotResumed) != 0 {
		t.Fatalf("committed resume was left pending: %+v %v", v, err)
	}
}
