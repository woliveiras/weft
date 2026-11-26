package chat

import (
	"github.com/woliveiras/weft/server/internal/taskauth"
	"strings"
	"testing"
	"time"
)

func TestTaskAdmissionBindsProjectCommitAndRevision(t *testing.T) {
	now := time.Now()
	start := TaskAdmission{ID: "tsk_project", WorkflowID: "docs", Version: 1, Revision: 1, Consent: taskauth.StartConsent{Scope: "scope", Mode: taskauth.Ask}, Repositories: []StartRepository{{Project: "prj_demo", Commit: strings.Repeat("a", 40), Revision: 1}}}
	r, err := NewTaskStartReview("owner", "session", start, now)
	if err != nil {
		t.Fatal(err)
	}
	r.TaskStart.Repositories[0].Revision++
	if err = r.ValidateConfirmation("owner", "session", r.Digest, now); err == nil {
		t.Fatal("ignored project revision")
	}
	start.Repositories[0].Commit = "main"
	if _, err := NewTaskStartReview("owner", "session", start, now); err == nil {
		t.Fatal("accepted a mutable ref")
	}
}
