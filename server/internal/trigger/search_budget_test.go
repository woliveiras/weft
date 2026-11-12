package trigger

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestGitHubSearchBudgetUsesRollingMinute(t *testing.T) {
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	for range githubSearchesPerMinute {
		if _, err := reserveGitHubSearch(t.Context(), st, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reserveGitHubSearch(t.Context(), st, now.Add(59*time.Second)); !errors.Is(err, errSearchBudgetDeferred) {
		t.Fatalf("31st call was not deferred: %v", err)
	}
	if _, err := reserveGitHubSearch(t.Context(), st, now.Add(time.Minute)); err != nil {
		t.Fatalf("expired reservations did not free the budget: %v", err)
	}
}

func TestSearchTurnPrioritizesDeferralsThenLeastRecentGrant(t *testing.T) {
	newer := &Trigger{SearchTurn: 30}
	older := &Trigger{SearchTurn: 12}
	if compareSearchTurn(older, newer) >= 0 {
		t.Fatal("older grant did not get the first turn")
	}
	newer.SearchDeferredReason = githubSearchDeferred
	if compareSearchTurn(newer, older) >= 0 {
		t.Fatal("deferred trigger did not get the first turn")
	}
	older.SearchDeferredReason = githubSearchDeferred
	if compareSearchTurn(older, newer) >= 0 {
		t.Fatal("less recently granted deferral did not get the first turn")
	}
}

func TestSearchBudgetPreservesLegacyReservations(t *testing.T) {
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	active := make([]time.Time, githubSearchesPerMinute)
	for i := range active {
		active[i] = now
	}
	raw, _ := json.Marshal(active)
	if _, err := st.PutSetting(t.Context(), githubSearchBudgetKey, raw, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := reserveGitHubSearch(t.Context(), st, now); !errors.Is(err, errSearchBudgetDeferred) {
		t.Fatalf("legacy quota was not retained: %v", err)
	}
	if turn, err := reserveGitHubSearch(t.Context(), st, now.Add(time.Minute)); err != nil || turn != 1 {
		t.Fatalf("legacy window did not recover: %d, %v", turn, err)
	}
}
