package chat

import (
	"errors"
	"testing"
	"time"
)

func TestTaskCancelReviewSealsTheCurrentRun(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"task.cancel","task":"tsk_editorial"}`)
	if err != nil || plan.Kind != CancelTask || plan.Task != "tsk_editorial" {
		t.Fatalf("cancellation intent: %+v %v", plan, err)
	}
	now := time.Now()
	review, err := NewTaskCancellationReview("owner", "session", TaskCancellation{
		ID: plan.Task, Revision: 3, ExecutionGraphID: "task-run-editorial", ExecutionVersion: 1, RunID: "run_first",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	review.TaskCancel.RunID = "run_other"
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("another run received consent: %v", err)
	}
	if _, err := decodePlan(`{"kind":"task.cancel","task":"tsk_editorial","controlAction":"kill"}`); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("mixed scope cancelled a task: %v", err)
	}
}
