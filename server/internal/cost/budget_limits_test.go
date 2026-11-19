package cost

import (
	"context"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
)

func TestApplyMonthlyLimitKeepsOnlyTheExplicitRaiseAsFloor(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ledger        Ledger
		wantHighFloor int64
		wantLowLimit  int64
	}{
		{"no explicit raise", Ledger{Limit: 100, Reached: true}, 0, 90},
		{"explicit raise", Ledger{Limit: 120, RaisedLimit: 120, RaisedBy: "admin", Reached: true}, 120, 120},
		{"legacy raised ledger", Ledger{Limit: 120, RaisedBy: "admin", Reached: true}, 120, 120},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := tc.ledger
			applicable{scope: User, limit: 200}.applyLimit(&l)
			if l.Limit != 200 || l.RaisedLimit != tc.wantHighFloor || l.Reached {
				t.Fatalf("increased definition: %+v", l)
			}
			applicable{scope: User, limit: 90}.applyLimit(&l)
			if l.Limit != tc.wantLowLimit || l.RaisedLimit != tc.wantHighFloor {
				t.Fatalf("decreased definition: %+v", l)
			}
		})
	}
}

// Only definition reads are available: a view cannot mutate stored ledgers.
type viewDefinitionStore struct {
	storage.Store
	value []byte
	err   error
}

func (s viewDefinitionStore) GetSetting(context.Context, string) (*storage.Setting, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &storage.Setting{Value: s.value, Revision: 1}, nil
}

func TestVisibleMonthlyLimitUsesDefinitionsWithoutLosingRaiseFloors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ledger Ledger
		want   int64
	}{
		{"ordinary increase", Ledger{Scope: Installation, ID: "*@2026-11", Limit: 100, Compacted: 10}, 200},
		{"raised floor", Ledger{Scope: Installation, ID: "*@2026-11", Limit: 300, RaisedLimit: 250, RaisedBy: "admin", Compacted: 10}, 250},
		{"legacy raised floor", Ledger{Scope: Installation, ID: "*@2026-11", Limit: 300, RaisedBy: "admin", Compacted: 10}, 300},
		{"closed month", Ledger{Scope: Installation, ID: "*@2026-10", Limit: 100, Compacted: 10}, 100},
		{"task run", Ledger{Scope: TaskRun, ID: "task-run-fixed", Limit: 100, Compacted: 10}, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := Budgets{Store: viewDefinitionStore{value: []byte(`{"budgets":[{"scope":"installation","id":"*","limitMicros":200}]}`)}}
			got, err := b.visibleView(t.Context(), &tc.ledger, "2026-11")
			if err != nil || got.Limit != tc.want || got.Charged != 10 || got.Remaining != tc.want-10 {
				t.Fatalf("view=%+v err=%v", got, err)
			}
		})
	}
}
func TestVisibleMonthlyLimitPropagatesDefinitionFailures(t *testing.T) {
	for _, st := range []viewDefinitionStore{{err: errors.New("storage unavailable")}, {value: []byte(`not-json`)}} {
		b := Budgets{Store: st}
		l := Ledger{Scope: Installation, ID: "*@2026-11", Limit: 100}
		if _, err := b.visibleView(t.Context(), &l, "2026-11"); err == nil {
			t.Fatal("definition failure appeared successful")
		}
		if l.Limit != 100 {
			t.Fatal("failed read modified ledger")
		}
	}
}
