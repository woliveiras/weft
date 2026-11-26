package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestTaskReviewBindsRequestWorkflowAndScope(t *testing.T) {
	t.Parallel()
	plan, err := decodePlan(`{"kind":"task.create","taskTitle":"Summarize notes","workflow":"Editorial","workflowVersion":"1","request":"Read the notes.","outcome":"understand"}`)
	if err != nil || plan.Kind != CreateTask || !plan.TaskDraft.MatchWorkflowVersion(1) || plan.TaskDraft.MatchWorkflowVersion(2) {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	in := plan.TaskDraft.Input()
	in.GraphID, in.GraphVersion, in.NodeID = "editorial", 1, "worker"
	now := time.Now()
	review, err := NewTaskReview("owner", "session", in, "workflow-fingerprint", now)
	if err != nil || review.ValidateConfirmation("owner", "session", review.Digest, now) != nil {
		t.Fatalf("review = %+v, %v", review, err)
	}
	review.TaskDraft.Input.Request = "Do another task."
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed request passed confirmation: %v", err)
	}
	for _, body := range []string{
		`{"kind":"task.create","taskTitle":"Review","workflow":"Editorial","request":"Read notes","outcome":"deploy"}`,
		`{"kind":"task.create","taskTitle":"Review","workflow":"Editorial","request":"Read notes","outcome":"review","grants":"filesystem.write"}`,
		`{"kind":"task.create","taskTitle":"Review","workflow":"Editorial","request":"Read notes","outcome":"review","start":"yes"}`,
		`{"kind":"task.watch","task":"tsk_example","workflow":"Editorial"}`,
	} {
		if _, err := decodePlan(body); !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("accepted %s: %v", body, err)
		}
	}
}

func TestTaskReviewRequiresBoundDraft(t *testing.T) {
	t.Parallel()
	if _, err := NewTaskReview("owner", "session", taskconfig.Input{Title: "Missing target"}, "workflow-fingerprint", time.Now()); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("missing workflow accepted: %v", err)
	}
}
