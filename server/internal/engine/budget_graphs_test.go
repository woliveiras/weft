package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/eventtest"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/observability"
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
		{name: "running graph with a hold", action: cost.HoldGraph, state: domain.GraphRunning, want: "pause"},
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
	for _, scenario := range []string{"project", "installation_reached_after_raise"} {
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
				if err != nil || len(v.NotResumed) != 0 || v.Paused {
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

func (s *budgetDeadlineStore) PutGraphRecordIf(ctx context.Context, rec *domain.GraphRecord, guards []storage.SettingRevision) error {
	if err := s.Store.PutGraphRecordIf(ctx, rec, guards); err != nil {
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

func TestReconcileBudgetChecksRunningGraphHold(t *testing.T) {
	for _, tc := range []struct {
		name       string
		scope      string
		defined    bool
		reached    bool
		wantPaused bool
		audit      string
	}{
		{"applicable reached hold", cost.TaskRun, false, true, true, "paused"},
		{"hold with headroom", cost.TaskRun, false, false, false, "limit_available"},
		{"removed definition", cost.Project, true, true, false, "not_applicable"},
		{"changed attribution", cost.Project, false, true, false, "not_applicable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, rec := budgetTransitionFixture(t, nil)
			ctx := t.Context()
			b := e.monetaryBudgets()
			id := rec.ID
			if tc.scope == cost.Project {
				project := "project-a"
				if !tc.defined {
					project = "project-b"
				}
				if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: tc.scope, ID: project, LimitMicros: 10}}, "admin", 0); err != nil {
					t.Fatal(err)
				}
				id = cost.MonthlyID(project, cost.Period(time.Now()))
			}
			putTransitionLedger(t, e, cost.Ledger{Scope: tc.scope, ID: id, Limit: 10, Reached: tc.reached,
				PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}})
			if tc.defined {
				if _, err := b.SetDefinitions(ctx, nil, "admin", 0); err != nil {
					t.Fatal(err)
				}
				if err := e.GraphAction(ctx, rec.ID, "resume", "operator"); err != nil {
					t.Fatal(err)
				}
			} else {
				rec.DesiredState = domain.GraphRunning
				if err := e.store.PutGraphRecord(ctx, rec); err != nil {
					t.Fatal(err)
				}
			}
			if e.reconcileBudgetGraphs(ctx) {
				t.Fatal("reconciliation backed off")
			}
			saved, err := e.store.GetGraphRecord(ctx, rec.ID)
			if err != nil || (saved.DesiredState == domain.GraphPaused) != tc.wantPaused {
				t.Fatalf("unexpected graph state: %+v %v", saved, err)
			}
			view, err := b.Ledger(ctx, tc.scope, id)
			if err != nil || view.Paused != tc.wantPaused || len(view.NotResumed) != 0 {
				t.Fatalf("unexpected hold state: %+v %v", view, err)
			}
			audit, err := e.store.ListEvents(ctx, storage.EventFilter{GraphID: rec.ID, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
			if err != nil || len(audit) != 1 || !bytes.Contains(audit[0].Data, []byte(`"outcome":"`+tc.audit+`"`)) {
				t.Fatalf("wrong transition audit: %v %v", audit, err)
			}
			eventtest.Capture(t, e.store, domain.EvBudgetGraphTransition)
		})
	}
}

func TestReconcileBudgetInvalidLedgerDoesNotBackOff(t *testing.T) {
	e, rec := budgetTransitionFixture(t, nil)
	var logs bytes.Buffer
	e.log = slog.New(slog.NewTextHandler(&logs, nil))
	ctx := t.Context()
	rec.DesiredState = domain.GraphRunning
	if err := e.store.PutGraphRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.PauseGraph, RunID: rec.RunID}}})
	if _, err := e.store.PutSetting(ctx, cost.BudgetPrefix+"run/unrelated",
		[]byte(`{"scope":"run","id":"unrelated","limit":10,"entries":{"invalid":{"amount":-1}},"pausedGraphs":["unrelated"]}`), 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if e.reconcileBudgetGraphs(ctx) {
			t.Fatal("an isolated invalid ledger made the reconciler back off")
		}
	}
	saved, err := e.store.GetGraphRecord(ctx, rec.ID)
	if err != nil || saved.DesiredState != domain.GraphPaused {
		t.Fatalf("healthy pause was not applied: %+v %v", saved, err)
	}
	audit, err := e.store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvBudgetLedgerInvalid}})
	if err != nil || len(audit) != 1 {
		t.Fatalf("expected one invalid-ledger audit per revision: %v %v", audit, err)
	}
	if n := bytes.Count(logs.Bytes(), []byte("reconcile budget graph transitions")); n != 1 {
		t.Fatalf("wanted one warning across the sweeps, got %d", n)
	}
	eventtest.Capture(t, e.store, domain.EvBudgetLedgerInvalid)
}

func TestReconcileBudgetRolloverRetainsBlockingHold(t *testing.T) {
	for _, scope := range []string{cost.Installation, cost.TaskRun} {
		t.Run(scope, func(t *testing.T) {
			e, rec := budgetTransitionFixture(t, nil)
			ctx := t.Context()
			b := e.monetaryBudgets()
			month := cost.Period(time.Now())
			id := rec.ID
			if scope == cost.Installation {
				id = cost.MonthlyID("*", month)
				if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: scope, ID: "*", LimitMicros: 10}}, "admin", 0); err != nil {
					t.Fatal(err)
				}
			}
			hold := cost.GraphAction{Action: cost.HoldGraph, RunID: rec.RunID}
			putTransitionLedger(t, e, cost.Ledger{Scope: cost.Installation, ID: "*@2000-01", Limit: 10, Reached: true,
				PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: hold}})
			current := cost.Ledger{Scope: scope, ID: id, Limit: 10, Compacted: 10}
			if scope == cost.TaskRun {
				// A ledger can reach its limit through spend without a refusal.
				current.PausedGraphs = []string{rec.ID}
				current.GraphActions = map[string]cost.GraphAction{rec.ID: hold}
			}
			putTransitionLedger(t, e, current)
			for range 2 {
				if e.reconcileBudgetGraphs(ctx) {
					t.Fatal("rollover sweep failed")
				}
			}
			actions, _, err := b.PendingGraphActions(ctx)
			if err != nil || len(actions) != 1 || actions[0].Scope != scope || actions[0].ID != id || actions[0].GraphAction != hold {
				t.Fatalf("wrong ledger owns the hold: %+v %v", actions, err)
			}
			saved, err := e.store.GetGraphRecord(ctx, rec.ID)
			if err != nil || saved.DesiredState != domain.GraphPaused || saved.BudgetResumeID != "" {
				t.Fatalf("rollover resumed a blocked graph: %+v %v", saved, err)
			}
		})
	}
}

type reserveDuringResumeStore struct {
	storage.Store
	key    string
	onRead func()
}

func (s *reserveDuringResumeStore) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	row, err := s.Store.GetSetting(ctx, key)
	if key == s.key && s.onRead != nil {
		hook := s.onRead
		s.onRead = nil
		hook()
	}
	return row, err
}

func TestBudgetResumeRechecksConcurrentReservation(t *testing.T) {
	inject := &reserveDuringResumeStore{}
	e, rec := budgetTransitionFixture(t, func(st storage.Store) storage.Store {
		inject.Store = st
		return inject
	})
	ctx := t.Context()
	b := e.monetaryBudgets()
	scope, id := cost.Installation, cost.MonthlyID("*", cost.Period(time.Now()))
	if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: scope, ID: "*", LimitMicros: 20}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	putTransitionLedger(t, e, cost.Ledger{Scope: scope, ID: id, Limit: 20, Currency: "USD"})
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}})
	_, _, actions, err := b.Raise(ctx, cost.TaskRun, rec.ID, 30, "admin")
	if err != nil || len(actions) != 1 {
		t.Fatalf("schedule resume: %+v %v", actions, err)
	}
	inject.key = cost.BudgetPrefix + scope + "/" + id
	snapshotRead := make(chan struct{})
	reserved := make(chan error, 1)
	go func() {
		select {
		case <-snapshotRead:
		case <-ctx.Done():
			return
		}
		other := cost.Budgets{Store: inject.Store}
		prices := func(context.Context) (cost.Catalogue, error) {
			return cost.Catalogue{Currency: "USD", Prices: []cost.Price{
				{Provider: "fixture", Model: "*", OutputMicrosPerMTok: 1_000_000},
			}}, nil
		}
		_, err := other.Reserve(ctx, cost.Target{Graph: "other-graph"}, prices, "fixture", "openai",
			&llm.ChatRequest{Model: "fixture", MaxTokens: 20})
		reserved <- err
	}()
	inject.onRead = func() {
		// The other controller reserves against the real store after this
		// resume read captures the old snapshot.
		close(snapshotRead)
		if err := <-reserved; err != nil {
			t.Fatalf("competing reservation: %v", err)
		}
	}
	outcome, err := e.waitBudgetGraphTransition(ctx, actions[0])
	if outcome != "budget_reached" || err != nil {
		t.Fatalf("resume missed concurrent spend: %q %v", outcome, err)
	}
	saved, err := e.store.GetGraphRecord(ctx, rec.ID)
	if err != nil || saved.DesiredState != domain.GraphPaused {
		t.Fatalf("graph was resumed: %+v %v", saved, err)
	}
	view, err := b.Ledger(ctx, scope, id)
	if err != nil || !view.Paused || view.Reserved != 20 {
		t.Fatalf("reservation or hold lost: %+v %v", view, err)
	}
}

func TestInvalidNodeControlDoesNotDelayHealthyGraph(t *testing.T) {
	e, rec := budgetTransitionFixture(t, nil)
	var logs bytes.Buffer
	e.log = slog.New(slog.NewTextHandler(&logs, nil))
	ctx := t.Context()
	if err := e.store.KVPut(ctx, controlNS(rec.ID), "node/reader", []byte(`{"paused":"invalid"}`)); err != nil {
		t.Fatal(err)
	}
	e.markCompleted(ctx, rec.ID, "reader", 0, rec.RunID)
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}})
	healthy := *rec
	healthy.ID += "-healthy"
	healthy.State, healthy.DesiredState = domain.GraphRunning, domain.GraphRunning
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: healthy.ID}}
	if _, err := e.store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	if err := e.store.PutGraphRecord(ctx, &healthy); err != nil {
		t.Fatal(err)
	}
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: healthy.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{healthy.ID}, GraphActions: map[string]cost.GraphAction{healthy.ID: {Action: cost.PauseGraph, RunID: healthy.RunID}}})
	other := *rec
	other.ID += "-invalid"
	g, err := e.store.GetGraph(ctx, rec.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	g.Metadata.ID = other.ID
	if _, err := e.store.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	if err := e.store.PutGraphRecord(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if err := e.store.KVPut(ctx, controlNS(other.ID), "node/reader", []byte(`{"paused":"invalid"}`)); err != nil {
		t.Fatal(err)
	}
	e.markCompleted(ctx, other.ID, "reader", 0, other.RunID)
	for range 3 {
		if _, err := e.Status(ctx, other.ID); err != nil {
			t.Fatal(err)
		}
		status, err := e.Status(ctx, rec.ID)
		if err != nil || status.RunFinished || status.DesiredState != domain.GraphPaused {
			t.Fatalf("invalid control must leave the run incomplete: %+v %v", status, err)
		}
		if e.reconcileBudgetGraphs(ctx) {
			t.Fatal("isolated invalid control caused backoff")
		}
	}
	saved, err := e.store.GetGraphRecord(ctx, healthy.ID)
	if err != nil || saved.DesiredState != domain.GraphPaused {
		t.Fatalf("healthy graph did not pause: %+v %v", saved, err)
	}
	if n := bytes.Count(logs.Bytes(), []byte("check paused run completion")); n != 2 || !bytes.Contains(logs.Bytes(), []byte("graph="+other.ID)) {
		t.Fatalf("wanted one warning per graph across polling and sweeps, got %d: %s", n, logs.String())
	}
}

func TestBudgetWarningDue(t *testing.T) {
	now := time.Now()
	var last atomic.Int64
	if !budgetWarningDue(&last, now) || budgetWarningDue(&last, now.Add(59*time.Second)) ||
		!budgetWarningDue(&last, now.Add(time.Minute)) || budgetWarningDue(&last, now.Add(time.Minute)) {
		t.Fatal("expected one warning per minute")
	}
}

func TestManualBudgetResumePreservesHoldAndAuditsOperator(t *testing.T) {
	e, rec := budgetTransitionFixture(t, nil)
	ctx := t.Context()
	action := cost.GraphAction{Action: cost.HoldGraph, RunID: rec.RunID}
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: action}})
	err := e.GraphAction(ctx, rec.ID, "resume", "operator")
	var blocker *BudgetResumeError
	if !errors.As(err, &blocker) || blocker.Scope != cost.TaskRun || blocker.ID != rec.ID {
		t.Fatalf("manual resume did not identify the blocking ledger: %v", err)
	}
	stored, err := e.store.GetGraphRecord(ctx, rec.ID)
	if err != nil || stored.DesiredState != domain.GraphPaused {
		t.Fatalf("manual resume changed graph state: %+v %v", stored, err)
	}
	current, err := e.monetaryBudgets().GraphActionCurrent(ctx, cost.BudgetGraph{
		Scope: cost.TaskRun, ID: rec.ID, Graph: rec.ID, GraphAction: action,
	})
	if err != nil || !current {
		t.Fatalf("manual resume replaced the hold: %t %v", current, err)
	}
	audit, err := e.store.ListEvents(ctx, storage.EventFilter{GraphID: rec.ID, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
	if err != nil || len(audit) != 1 || audit[0].Actor != "operator" ||
		!bytes.Contains(audit[0].Data, []byte(`"requestedAction":"resume"`)) {
		t.Fatalf("missing manual resume audit: %+v %v", audit, err)
	}
}

func TestBlockedStartKeepsRunIDAndBudgetHold(t *testing.T) {
	e, rec := budgetTransitionFixture(t, nil)
	ctx := t.Context()
	rec.DesiredState, rec.State = domain.GraphStopped, domain.GraphStopped
	if err := e.store.PutGraphRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	action := cost.GraphAction{Action: cost.HoldGraph, RunID: rec.RunID}
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Compacted: 10,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: action}})
	var blocker *BudgetResumeError
	if err := e.GraphAction(ctx, rec.ID, "start", "operator"); !errors.As(err, &blocker) || blocker.ID != rec.ID {
		t.Fatalf("expected the inherited task-run budget to block start: %v", err)
	}
	saved, err := e.store.GetGraphRecord(ctx, rec.ID)
	if err != nil || saved.RunID != rec.RunID || saved.DesiredState != domain.GraphStopped {
		t.Fatalf("blocked start changed the graph: %+v %v", saved, err)
	}
	current, err := e.monetaryBudgets().GraphActionCurrent(ctx, cost.BudgetGraph{
		Scope: cost.TaskRun, ID: rec.ID, Graph: rec.ID, GraphAction: action,
	})
	if err != nil || !current {
		t.Fatalf("blocked start cleared the hold: %t %v", current, err)
	}
}

func TestMonthlyDefinitionChangesOnlyResumeWithAnIncreaseAndHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		spend, reserved, limit int64
		resume                 bool
	}{
		{"reduced after projected refusal", 8, 0, 9, false},
		{"increased to total usage", 8, 4, 12, false},
		{"increased above total usage", 8, 4, 13, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, rec := budgetTransitionFixture(t, nil)
			b := e.monetaryBudgets()
			ctx := t.Context()
			id := cost.MonthlyID("project-a", cost.Period(time.Now()))
			putTransitionLedger(t, e, cost.Ledger{Scope: cost.Project, ID: id, Limit: 10, Compacted: tc.spend, Reached: true,
				Entries:      map[string]cost.Entry{"pending": {Amount: tc.reserved}},
				PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}})
			if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Project, ID: "project-a", LimitMicros: tc.limit}}, "admin", 0); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if e.reconcileBudgetGraphs(ctx) {
					t.Fatal("sweep failed")
				}
			}
			saved, err := e.store.GetGraphRecord(ctx, rec.ID)
			if err != nil || (saved.DesiredState == domain.GraphRunning) != tc.resume || saved.RunID != rec.RunID {
				t.Fatalf("unexpected lifecycle state: %+v %v", saved, err)
			}
			view, err := b.Ledger(ctx, cost.Project, id)
			if err != nil || view.Paused == tc.resume || len(view.NotResumed) != 0 {
				t.Fatalf("unexpected ledger hold: %+v %v", view, err)
			}
		})
	}
}

func TestRemovedDefinitionTransfersHoldToApplicableBudget(t *testing.T) {
	for _, state := range []domain.GraphState{domain.GraphRunning, domain.GraphPaused} {
		t.Run(string(state), func(t *testing.T) {
			e, rec := budgetTransitionFixture(t, nil)
			ctx := t.Context()
			rec.DesiredState = state
			if err := e.store.PutGraphRecord(ctx, rec); err != nil {
				t.Fatal(err)
			}
			id := cost.MonthlyID("project-a", cost.Period(time.Now()))
			hold := cost.GraphAction{Action: cost.HoldGraph, RunID: rec.RunID}
			putTransitionLedger(t, e, cost.Ledger{Scope: cost.Project, ID: id, Limit: 10, Reached: true,
				PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: hold}})
			putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true})
			for range 2 {
				if e.reconcileBudgetGraphs(ctx) {
					t.Fatal("sweep failed")
				}
			}
			actions, _, err := e.monetaryBudgets().PendingGraphActions(ctx)
			if err != nil || len(actions) != 1 || actions[0].Scope != cost.TaskRun || actions[0].GraphAction != hold {
				t.Fatalf("hold was not transferred: %+v %v", actions, err)
			}
			saved, err := e.store.GetGraphRecord(ctx, rec.ID)
			if err != nil || saved.DesiredState != domain.GraphPaused {
				t.Fatalf("graph escaped the hold: %+v %v", saved, err)
			}
			audit, err := e.store.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvBudgetGraphTransition}})
			if err != nil || len(audit) != 1 || !bytes.Contains(audit[0].Data, []byte(`"blockingScope":"run"`)) {
				t.Fatalf("missing hold transfer audit: %+v %v", audit, err)
			}
		})
	}
}

type changingBudgetDefinitions struct {
	storage.Store
	revision int64
}

func (s *changingBudgetDefinitions) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	row, err := s.Store.GetSetting(ctx, key)
	if err == nil && key == cost.DefinitionsKey {
		s.revision++
		row.Revision = s.revision
	}
	return row, err
}

func TestBudgetSweepCountsAndRateLimitsConflicts(t *testing.T) {
	e, rec := budgetTransitionFixture(t, func(store storage.Store) storage.Store {
		return &changingBudgetDefinitions{Store: store}
	})
	ctx := t.Context()
	if _, err := e.monetaryBudgets().SetDefinitions(ctx, nil, "admin", 0); err != nil {
		t.Fatal(err)
	}
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}})
	var logs bytes.Buffer
	e.log = slog.New(slog.NewTextHandler(&logs, nil))
	counter := observability.BudgetOperations.WithLabelValues("all", "reconcile_graph", "conflict")
	var before, after dto.Metric
	if err := counter.Write(&before); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if e.reconcileBudgetGraphs(ctx) {
			t.Fatal("ordinary conflicts changed the retry cadence")
		}
	}
	if err := counter.Write(&after); err != nil {
		t.Fatal(err)
	}
	if after.GetCounter().GetValue()-before.GetCounter().GetValue() != 2 ||
		bytes.Count(logs.Bytes(), []byte("budget graph transition conflicts")) != 1 {
		t.Fatalf("unexpected metric or log: before %v, after %v, logs %s", &before, &after, &logs)
	}
}

func TestInvalidControlWarningsAreBoundedPerGraph(t *testing.T) {
	now := time.Now()
	var limiter graphWarningLimiter
	if !limiter.allow("a", now) || !limiter.allow("b", now) || limiter.allow("a", now) {
		t.Fatal("warnings were not limited independently")
	}
	for i := range maxGraphWarnings - 2 {
		if !limiter.allow(fmt.Sprintf("graph-%d", i), now) {
			t.Fatal("limiter rejected a graph before reaching capacity")
		}
	}
	if limiter.allow("overflow", now) || len(limiter.warned) != maxGraphWarnings {
		t.Fatal("warning limiter exceeded capacity")
	}
	if !limiter.allow("overflow", now.Add(time.Minute)) || len(limiter.warned) != 1 {
		t.Fatal("expired entries were not reclaimed")
	}
}

func TestNodeStartChecksBudgetBeforeResettingCompletion(t *testing.T) {
	e, rec := budgetTransitionFixture(t, nil)
	ctx := t.Context()
	rec.State, rec.DesiredState = domain.GraphStopped, domain.GraphStopped
	if err := e.store.PutGraphRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	e.markCompleted(ctx, rec.ID, "reader", 0, rec.RunID)
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Reached: true})
	var blocked *BudgetResumeError
	if err := e.NodeAction(ctx, rec.ID, "reader", "start", "operator"); !errors.As(err, &blocked) || blocked.ID != rec.ID {
		t.Fatalf("node start was not blocked: %v", err)
	}
	if _, err := e.store.KVGet(ctx, controlNS(rec.ID), doneKey("reader", 0)); err != nil {
		t.Fatalf("blocked node start reset completion: %v", err)
	}
	saved, err := e.store.GetGraphRecord(ctx, rec.ID)
	if err != nil || saved.RunID != rec.RunID || saved.DesiredState != domain.GraphStopped {
		t.Fatalf("blocked node start changed graph: %+v %v", saved, err)
	}
}
