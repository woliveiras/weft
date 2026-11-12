package trigger_test

import (
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestCompletionWritesBackAndTakesTheNextIssue(t *testing.T) {
	st := triggerStore(t)
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "First", "weft"), ghIssue(2, "Second", "weft")}
	c := &clock{t: time.Now().UTC()}
	r := runner(st, c, "a", gh)
	fake := &tasks{status: map[string]trigger.TaskStatus{}}
	r.Tasks, r.BaseURL = fake, "https://weft.example"
	tr, _ := r.Triggers.Create(t.Context(), input(), "alice")
	r.Tick(t.Context()) // takes #1
	task := "tsk_acme/app1"
	// Still running: nothing written.
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if len(gh.comments["#1"]) != 1 {
		t.Fatalf("wrote back before the task ended: %v", gh.comments)
	}
	fake.status[task] = trigger.TaskStatus{State: "succeeded", PullRequestURL: "https://github.example/acme/app/pull/9"}
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if got := gh.comments["#1"]; len(got) != 2 || !strings.Contains(got[1], "https://github.example/acme/app/pull/9") || !strings.Contains(got[1], "Task: https://weft.example/#/tasks/"+task) {
		t.Fatalf("completion comment = %v", got)
	}
	labels := gh.find("#1")["labels"].([]map[string]any)
	if labels[len(labels)-1]["name"] != trigger.LabelReview {
		t.Fatalf("labels = %v", labels)
	}
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	if got.State != trigger.Active || got.CurrentTask != "" {
		t.Fatalf("after success: %+v", got)
	}
	// The next run takes the next issue (#1 no longer carries the label).
	gh.issues[0]["labels"] = []map[string]any{{"name": trigger.LabelReview}}
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if len(fake.started) != 2 || fake.started[1].Key != "acme/app#2" {
		t.Fatalf("next issue: %+v", fake.started)
	}
	// A failed task reports why, marks the issue and pauses the trigger.
	fake.status["tsk_acme/app2"] = trigger.TaskStatus{State: "failed", Reason: "git.push rejected: missing permission"}
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if got := gh.comments["#2"]; len(got) != 2 || !strings.Contains(got[1], "Review the task") {
		t.Fatalf("failure comment = %v", got)
	}
	labels = gh.find("#2")["labels"].([]map[string]any)
	if labels[len(labels)-1]["name"] != trigger.LabelFailed {
		t.Fatalf("labels = %v", labels)
	}
	got, _ = r.Triggers.Get(t.Context(), tr.ID)
	if got.State != trigger.Paused || !strings.Contains(got.PausedReason, "Review delivery") || got.CurrentTask != "" {
		t.Fatalf("after failure: %+v", got)
	}
	// A finished task without a pull request requires operator attention.
	fake2 := &tasks{status: map[string]trigger.TaskStatus{"tsk_acme/app3": {State: "succeeded"}}}
	gh.issues = []map[string]any{ghIssue(3, "Third", "weft")}
	r.Tasks = fake2
	if _, err := r.Triggers.Resume(t.Context(), tr.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	r.Tick(t.Context())
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if got := gh.comments["#3"]; len(got) != 2 || !strings.Contains(got[1], "without an open pull request") {
		t.Fatalf("no-PR completion = %v", got)
	}
}
