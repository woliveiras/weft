package chat

import (
	"errors"
	"testing"
	"time"
)

func TestRetryDraftReviewRejectsDifferentSourceRevision(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"task.retry","task":"tsk_original"}`)
	if err != nil || plan.Kind != RetryTask || plan.Task != "tsk_original" {
		t.Fatalf("retry plan = %+v, %v", plan, err)
	}
	now := time.Now()
	review, err := NewTaskRetryReview("owner", "session", TaskRetry{ID: plan.Task, Revision: 2, WorkflowID: "editorial", WorkflowVersion: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	review.TaskRetry.Revision++
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed source revision accepted: %v", err)
	}
	if _, err := decodePlan(`{"kind":"task.retry","task":"tsk_original","start":"true"}`); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("retry plan could request dispatch: %v", err)
	}
}
