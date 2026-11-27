package chat

import (
	"fmt"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
)

func TestSpendQueryArguments(t *testing.T) {
	now := time.Date(2025, 3, 31, 22, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ raw, period string }{
		{`{"groupBy":"total"}`, "2025-03"},
		{`{"groupBy":"provider","period":"previous","page":"2"}`, "2025-02"},
		{`{"groupBy":"project","period":"2024-02","search":"wool"}`, "2024-02"},
		{`{"groupBy":"bad"}`, ""}, {`{"groupBy":"total","period":"2024-14"}`, ""}, {`{"groupBy":"total","search":"app"}`, ""},
		{`{"groupBy":"user","groupBy":"total"}`, ""}, {`{"groupBy":"user","page":"0"}`, ""},
		{`{"groupBy":"task","search":null}`, ""}, {`{"groupBy":"workflow","apply":true}`, ""},
	} {
		got, err := ParseSpendQuery([]byte(tc.raw), now)
		if tc.period == "" && err == nil || tc.period != "" && (err != nil || got.Period != tc.period) {
			t.Errorf("parse %s: %+v, %v", tc.raw, got, err)
		}
	}
}

func TestSpendBreakdownFiltersBeforePagingAndKeepsInstallationTotal(t *testing.T) {
	r := cost.Report{Currency: "USD", Period: "2025-01", Truncated: true, Total: cost.Line{Calls: 55, CostMicros: 66}}
	r.ByProvider = []cost.Line{{Key: "different", Model: "other"}}
	for i := 1; i <= 21; i++ {
		r.ByProvider = append(r.ByProvider, cost.Line{Key: fmt.Sprintf("provider-%02d", i), Model: "Small"})
	}
	q := SpendQuery{GroupBy: "provider", Search: "SMALL", Page: 1}
	first, err := q.Observe(r)
	if err != nil || len(first.Rows) != 20 || first.Rows[0].Key != "provider-01" || !first.More || !first.Truncated || first.MatchingGroups != 21 || first.Total != r.Total {
		t.Fatalf("page one: %+v, %v", first, err)
	}
	q.Page = 2
	second, err := q.Observe(r)
	if err != nil || len(second.Rows) != 1 || second.Rows[0].Key != "provider-21" || second.More {
		t.Fatalf("page two: %+v, %v", second, err)
	}
	r.ByProvider[21].Key = "multiline\nname"
	if _, err = q.Observe(r); err != ErrSpendUnavailable {
		t.Fatalf("unsafe row leaked: %v", err)
	}
}
