package chat

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage/stores"
	"github.com/woliveiras/weft/server/internal/taskauth"
)

func TestTaskStartClaimIsVisibleToObservationAfterReopeningStore(t *testing.T) {
	ctx := t.Context()
	cfg := stores.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "admission.db")}
	store, err := stores.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sessions := Sessions{Store: store}
	const owner, id = "owner", "cd85104df80d48ed81bc908b7fc954d9"
	if _, _, err := sessions.Create(ctx, owner, id, "Start the saved task."); err != nil {
		t.Fatal(err)
	}
	_, claim, err := sessions.ClaimInterpretation(ctx, owner, id, id, false)
	if err != nil || claim.IsZero() {
		t.Fatalf("interpretation claim: %v %v", claim, err)
	}
	review, err := NewTaskStartReview(owner, id, TaskAdmission{
		ID: "tsk_example", WorkflowID: "editorial", Version: 1, Revision: 1,
		Consent: taskauth.StartConsent{Scope: "scope", Mode: taskauth.Ask},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.FinishInterpretation(ctx, owner, id, id, claim, Interpretation{Review: review}); err != nil {
		t.Fatal(err)
	}
	if _, _, claimed, err := sessions.ClaimReview(ctx, owner, id, review.ID, review.Digest); err != nil || !claimed {
		t.Fatalf("start claim: %v %v", claimed, err)
	}
	if _, err := sessions.Archive(ctx, owner, id, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = stores.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions.Store = store
	called := false
	_, err = sessions.ReconcilePage(ctx, "", func(candidate string) bool { return candidate == owner }, nil,
		func(ctx context.Context, subject string, session *Session) (*Session, error) {
			called = true
			if !session.Archived || session.Messages[1].Review.State != ReviewApplying {
				t.Fatalf("unexpected persisted claim: %+v", session)
			}
			return sessions.FinishTaskStart(ctx, subject, session.ID, review.ID, TaskSummary{ID: "tsk_example", State: "running"})
		})
	if err != nil || !called {
		t.Fatalf("the detached observer missed the interrupted start: %v %v", called, err)
	}
	settled, err := sessions.Get(ctx, owner, id)
	if err != nil || settled.Messages[1].Review.State != ReviewAccepted || len(settled.Watches) != 1 ||
		settled.Watches[0].Task.ID != "tsk_example" {
		t.Fatalf("start result and observation were not saved together: %+v %v", settled, err)
	}
}
