package cost_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestReservationUsesDefinitionReplacedDuringPricing(t *testing.T) {
	_, b := budgetEnv(t, 0)
	ctx := t.Context()
	target := cost.Target{Graph: "task-run-review", User: "alice"}
	put := func(limit int64) {
		t.Helper()
		if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.User, ID: "alice", LimitMicros: limit}}, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
	put(100)
	r, err := b.Reserve(ctx, target, func(context.Context) (cost.Catalogue, error) {
		put(10)
		return flat, nil
	}, "team", "openai", request(30))
	if !errors.Is(err, cost.ErrBudgetReached) {
		t.Fatalf("reservation after PUT lowered the limit: reserved=%v err=%v", r != nil, err)
	}
}

func TestDefinitionIncreaseDoesNotBecomeAnExplicitRaise(t *testing.T) {
	_, b := budgetEnv(t, 0)
	ctx := t.Context()
	now := time.Now()
	id := cost.MonthlyID("alice", cost.Period(now))
	target := cost.Target{Graph: "task-run-review", User: "alice"}
	put := func(limit int64) {
		t.Helper()
		if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.User, ID: "alice", LimitMicros: limit}}, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
	put(100)
	r, err := b.Reserve(ctx, target, cat(flat), "team", "openai", request(20))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := b.Raise(ctx, cost.User, id, 120, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reserve(ctx, target, cat(flat), "team", "openai", request(120)); !errors.Is(err, cost.ErrBudgetReached) {
		t.Fatalf("initial refusal: %v", err)
	}
	hold := cost.BudgetGraph{Scope: cost.User, ID: id, Graph: target.Graph, GraphAction: cost.GraphAction{RunID: "r1", Action: cost.HoldGraph}}
	if err := b.MarkPaused(ctx, hold, 120); err != nil {
		t.Fatal(err)
	}
	put(200)
	if err := b.ScheduleGraphResume(ctx, hold, now); err != nil {
		t.Fatal(err)
	}
	put(150)
	r, err = b.Reserve(ctx, target, cat(flat), "team", "openai", request(120))
	if !errors.Is(err, cost.ErrBudgetReached) {
		v, _ := b.Ledger(ctx, cost.User, id)
		t.Fatalf("PUT(150) after background resume under PUT(200): reserved=%v err=%v ledger=%+v", r != nil, err, v)
	}
}

type definitionDuringReservationStore struct {
	storage.Store
	beforeWrite func(string)
}

func (s *definitionDuringReservationStore) PutSettingIf(ctx context.Context, key string, value []byte, revision int64, by string, guards []storage.SettingRevision) (int64, error) {
	if s.beforeWrite != nil {
		s.beforeWrite(key)
	}
	return s.Store.PutSettingIf(ctx, key, value, revision, by, guards)
}

func TestReservationReleasesPartialCreditBeforeRetryingAfterDefinitionEdit(t *testing.T) {
	st, b := budgetEnv(t, 100)
	ctx := t.Context()
	target := cost.Target{Graph: "task-run-partial", User: "alice"}
	id := cost.MonthlyID("alice", cost.Period(time.Now()))
	put := func(limit int64) {
		t.Helper()
		if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.User, ID: "alice", LimitMicros: limit}}, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
	put(100)
	wrapper := &definitionDuringReservationStore{Store: st}
	b.Store = wrapper
	wrapper.beforeWrite = func(key string) {
		if key != cost.BudgetPrefix+cost.User+"/"+id {
			return
		}
		wrapper.beforeWrite = nil
		v, err := b.Ledger(ctx, cost.TaskRun, target.Graph)
		if err != nil || v == nil || v.Reserved != 40 {
			t.Fatalf("first budget should still hold 40: %+v %v", v, err)
		}
		put(10)
	}
	r, err := b.Reserve(ctx, target, cat(flat), "team", "openai", request(30))
	if r != nil || !errors.Is(err, cost.ErrBudgetReached) {
		t.Fatalf("reservation after PUT: %v %v", r, err)
	}
	v, err := b.Ledger(ctx, cost.TaskRun, target.Graph)
	if err != nil || v == nil || v.Reserved != 0 || v.Charged != 0 {
		t.Fatalf("rejected request leaked credit: %+v %v", v, err)
	}
}

func TestMonthlyRaiseRechecksDefinitionBeforeCommitting(t *testing.T) {
	st, b := budgetEnv(t, 0)
	ctx := t.Context()
	target := cost.Target{Graph: "task-run-raise", User: "alice"}
	id := cost.MonthlyID("alice", cost.Period(time.Now()))
	put := func(limit int64) {
		t.Helper()
		if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.User, ID: "alice", LimitMicros: limit}}, "admin", 0); err != nil {
			t.Fatal(err)
		}
	}
	put(100)
	r, err := b.Reserve(ctx, target, cat(flat), "team", "openai", request(20))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx, nil); err != nil {
		t.Fatal(err)
	}
	wrapper := &definitionDuringReservationStore{Store: st}
	b.Store = wrapper
	wrapper.beforeWrite = func(key string) {
		if key != cost.BudgetPrefix+cost.User+"/"+id {
			t.Fatalf("unexpected guarded write: %s", key)
		}
		wrapper.beforeWrite = nil
		put(200)
	}
	var ve *cost.ValidationError
	if _, _, _, err := b.Raise(ctx, cost.User, id, 150, "admin"); !errors.As(err, &ve) {
		t.Fatalf("raise below the committed monthly limit: %v", err)
	}
	v, err := b.Ledger(ctx, cost.User, id)
	if err != nil || v == nil || v.RaisedBy != "" {
		t.Fatalf("stale raise was persisted: %+v %v", v, err)
	}
}
