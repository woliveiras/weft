package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestCheckoutPlanRequiresAnAliasAndOneReplacement(t *testing.T) {
	for _, tc := range []struct {
		name, args string
		ok         bool
	}{
		{"edit_task_draft", `{"task":"demo","checkoutAlias":"docs","replacementProject":"New docs"}`, true},
		{"edit_task_draft", `{"task":"demo","checkoutAlias":"../docs","replacementProject":"New docs"}`, false},
		{"edit_task_draft", `{"task":"demo","checkoutAlias":"docs"}`, false},
		{"edit_task_draft", `{"task":"demo","replacementProject":"New docs"}`, false},
		{"edit_task_draft", `{"task":"demo","checkoutAlias":"docs","replacementProject":"Docs","newPayloadJson":"{}"}`, false},
		{"watch_task", `{"task":"demo","checkoutAlias":"docs","replacementProject":"Docs"}`, false},
	} {
		plan, known, err := NativeOperationPlan(llm.ToolCall{Name: tc.name, Arguments: json.RawMessage(tc.args)})
		if !known || tc.ok && (err != nil || plan.CheckoutAlias != "docs" || plan.ReplacementProject != "New docs") || !tc.ok && !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("%s %s: %+v %v", tc.name, tc.args, plan, err)
		}
	}
}

func TestCheckoutReviewBindsRepositoryDetails(t *testing.T) {
	old := domain.TaskRepository{Alias: "docs", ProjectID: "prj_old", BaseCommit: strings.Repeat("a", 40)}
	d := taskconfig.Draft{ID: "tsk_docs", Status: "draft", Revision: 2, Input: taskconfig.Input{Title: "Docs", Request: "Read docs", Outcome: "review", GraphID: "demo", GraphVersion: 1, NodeID: "reader", Repositories: []domain.TaskRepository{old}}}
	next := old
	next.ProjectID, next.BaseCommit = "prj_new", strings.Repeat("b", 40)
	change := TaskCheckoutChange{Before: old, After: next, ProjectRevision: 2}
	now := time.Now()
	r, err := NewTaskCheckoutReview("owner", "session", &d, change, "graph-digest", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ValidateConfirmation("owner", "session", r.Digest, now); err != nil {
		t.Fatal(err)
	}
	r.TaskCorrection.Repository.After.BaseCommit = old.BaseCommit
	if err = r.ValidateConfirmation("owner", "session", r.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed checkout retained digest: %v", err)
	}
	change.After.Checks = []domain.Check{{Name: "verify", Command: "Authorization: Bearer secret-fixture-value"}}
	if r, err = NewTaskCheckoutReview("owner", "session", &d, change, "graph-digest", now); err == nil || r != nil {
		t.Fatal("sensitive checkout became visible")
	}
}
