package trigger

import (
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
		if err := reserveGitHubSearch(t.Context(), st, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := reserveGitHubSearch(t.Context(), st, now.Add(59*time.Second)); !errors.Is(err, errSearchBudgetDeferred) {
		t.Fatalf("31st call was not deferred: %v", err)
	}
	if err := reserveGitHubSearch(t.Context(), st, now.Add(time.Minute)); err != nil {
		t.Fatalf("expired reservations did not free the budget: %v", err)
	}
}
