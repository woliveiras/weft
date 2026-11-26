package api

import (
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestTaskApprovalChoicesOnlyIncludeCurrentPendingIDs(t *testing.T) {
	now := time.Now()
	pending := []*domain.Approval{
		{ID: "available", Status: domain.ApprovalPending, Title: "Primary"},
		{ID: "other", Status: domain.ApprovalPending, Title: "available"},
		{ID: "expired", Status: domain.ApprovalPending, ExpiresAt: &now},
		{ID: "done", Status: domain.ApprovalRejected},
	}
	all := pendingTaskApprovalChoices(pending, now, "")
	if len(all.Choices) != 2 || all.Incomplete() {
		t.Fatal(all)
	}
	for _, tc := range []struct {
		id    string
		count int
	}{{"available", 1}, {"expired", 0}, {"missing", 0}, {"done", 0}} {
		result := pendingTaskApprovalChoices(pending, now, tc.id)
		if len(result.Choices) != tc.count || (tc.count == 1 && result.Choices[0].ID != tc.id) {
			t.Fatal(tc, result)
		}
	}
}
