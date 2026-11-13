package cost

import "testing"

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
