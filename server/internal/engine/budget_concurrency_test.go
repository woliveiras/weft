package engine

import (
	"context"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/eventtest"
	"github.com/woliveiras/weft/server/internal/storage"
)

type reviewBeforePauseStore struct {
	storage.Store
	beforePause func()
}

func (s *reviewBeforePauseStore) PutGraphRecordIf(ctx context.Context, rec *domain.GraphRecord, guards []storage.SettingRevision) error {
	if rec.DesiredState == domain.GraphPaused && s.beforePause != nil {
		hook := s.beforePause
		s.beforePause = nil
		hook()
	}
	return s.Store.PutGraphRecordIf(ctx, rec, guards)
}

func TestRaiseOvertakesRunningGraphPause(t *testing.T) {
	store := &reviewBeforePauseStore{}
	e, rec := budgetTransitionFixture(t, func(st storage.Store) storage.Store {
		store.Store = st
		return store
	})
	ctx := t.Context()
	b := e.monetaryBudgets()
	rec.DesiredState = domain.GraphRunning
	if err := e.store.PutGraphRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Compacted: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}})
	store.beforePause = func() {
		if _, _, _, err := b.Raise(ctx, cost.TaskRun, rec.ID, 30, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	if e.reconcileBudgetGraphs(ctx) {
		t.Fatal("sweep failed")
	}
	saved, err := store.GetGraphRecord(ctx, rec.ID)
	if err != nil || saved.DesiredState != domain.GraphRunning {
		t.Fatalf("stale pause after the raise committed: state=%s err=%v", saved.DesiredState, err)
	}
}

type beforeHoldRetirementStore struct {
	storage.Store
	beforeRetire func()
}

func (s *beforeHoldRetirementStore) PutSettingIf(ctx context.Context, key string, value []byte, revision int64, by string, guards []storage.SettingRevision) (int64, error) {
	if s.beforeRetire != nil {
		hook := s.beforeRetire
		s.beforeRetire = nil
		hook()
	}
	return s.Store.PutSettingIf(ctx, key, value, revision, by, guards)
}

func TestRemovingThenRestoringDefinitionCanDiscardHold(t *testing.T) {
	inject := &beforeHoldRetirementStore{}
	e, rec := budgetTransitionFixture(t, func(st storage.Store) storage.Store {
		inject.Store = st
		return inject
	})
	ctx := t.Context()
	b := e.monetaryBudgets()
	id := cost.MonthlyID("project-a", cost.Period(time.Now()))
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.Project, ID: id, Limit: 10, Compacted: 10, Reached: true,
		PausedGraphs: []string{rec.ID}, GraphActions: map[string]cost.GraphAction{rec.ID: {Action: cost.HoldGraph, RunID: rec.RunID}}})
	if _, err := b.SetDefinitions(ctx, nil, "admin", 0); err != nil {
		t.Fatal(err)
	}
	inject.beforeRetire = func() {
		if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Project, ID: "project-a", LimitMicros: 10}}, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
	if e.reconcileBudgetGraphs(ctx) || inject.beforeRetire != nil {
		t.Fatal("expected stale hold retirement to conflict without backing off")
	}
	v, err := b.Ledger(ctx, cost.Project, id)
	if err != nil || !v.Paused {
		t.Fatalf("hold lost after definition restoration: ledger=%+v err=%v", v, err)
	}
}

func TestPauseTransferEmitsOneCompleteAudit(t *testing.T) {
	e, rec := budgetTransitionFixture(t, nil)
	ctx := t.Context()
	b := e.monetaryBudgets()
	rec.DesiredState = domain.GraphRunning
	if err := e.store.PutGraphRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	id := cost.MonthlyID("project-a", cost.Period(time.Now()))
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.Project, ID: id, Limit: 10, Compacted: 10, Reached: true})
	putTransitionLedger(t, e, cost.Ledger{Scope: cost.TaskRun, ID: rec.ID, Limit: 10, Compacted: 10, Reached: true})
	if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.Project, ID: "project-a", LimitMicros: 20}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	turn := &turnEnv{gr: &graphRun{e: e, g: &domain.Graph{Metadata: domain.GraphMetadata{ID: rec.ID}}}, x: &domain.Execution{RunID: rec.RunID}}
	if err := turn.pauseForBudget(ctx, cost.Project, id, 10); err != nil {
		t.Fatal(err)
	}
	events, err := e.store.ListEvents(ctx, storage.EventFilter{GraphID: rec.ID, Types: []domain.EventType{domain.EvBudgetGraphTransition}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		t.Log(string(ev.Data))
	}
	if len(events) != 1 {
		t.Fatalf("got %d transition audits, want one", len(events))
	}
	eventtest.Capture(t, e.store, domain.EvBudgetGraphTransition)
}
