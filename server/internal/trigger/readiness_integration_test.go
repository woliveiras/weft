package trigger_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func advance(r *trigger.Runner, c *clock, t *testing.T, id string) *trigger.Trigger {
	t.Helper()
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	current, err := r.Triggers.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func ghReply(id int, who, kind, body string, at time.Time) map[string]any {
	authorID := 701
	if who == "weft-service" {
		authorID = 700
	}
	return map[string]any{"id": id, "user": map[string]any{"login": who, "type": kind, "id": authorID},
		"body": body, "created_at": at}
}

func ghTodoEvent(id int, who, kind string, at time.Time) map[string]any {
	return map[string]any{"id": id, "actor": map[string]string{"login": who, "type": kind}, "label": map[string]string{"name": trigger.LabelTodo},
		"event": "labeled", "created_at": at}
}

func TestReadinessAsksTwiceAndRequiresVerifiedManualRequeue(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "Describe parser", "weft", "bug")}
	r := runner(st, c, "one", gh)
	tasks := &tasks{}
	r.Tasks = tasks
	var evaluated []string
	r.Assessor = assessFunc(func(_ context.Context, _ *trigger.Trigger, input string) trigger.ReadinessAssessment {
		evaluated = append(evaluated, input)
		if strings.Contains(input, "FINAL ACCEPTANCE") {
			return trigger.ReadinessAssessment{}
		}
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	// The first request replaces the custom selection label. The bug label
	// stays in place and no task was admitted.
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].Round != 1 || tr.Holds[0].Publication != "confirmed" || len(tasks.started) != 0 || len(tr.Processed) != 0 ||
		!strings.Contains(gh.comments["#1"][0], "Weft question") {
		t.Fatalf("initial hold: %+v, comments %v", tr.Holds, gh.comments)
	}
	labels := gh.find("#1")["labels"].([]map[string]any)
	if len(labels) != 2 || labels[0]["name"] != "bug" || labels[1]["name"] != trigger.LabelFailed {
		t.Fatal(labels)
	}
	gh.replies["#1"] = []map[string]any{
		ghReply(1, "author", "User", "old", c.t.Add(-time.Second)),
		ghReply(2, "other", "User", "I speak for author", c.t.Add(time.Second)),
		ghReply(3, "weft-bot", "Bot", "bot answer", c.t.Add(time.Second)),
		ghReply(4, "author", "User", "Thanks!", c.t.Add(time.Second)),
		ghReply(10, "author", "User", gh.comments["#1"][0], c.t.Add(time.Second)),
		ghReply(11, "weft-bot", "User", gh.comments["#1"][0], c.t.Add(time.Second)),
	}
	tr = advance(r, c, t, tr.ID)
	if len(evaluated) != 1 || len(gh.comments["#1"]) != 1 {
		t.Fatal("an invalid reply triggered evaluation")
	}
	gh.replies["#1"] = append(gh.replies["#1"], ghReply(5, "author", "User", "author partial", c.t.Add(time.Second)))
	tr = advance(r, c, t, tr.ID)
	if len(evaluated) != 2 || tr.Holds[0].Round != 2 || !strings.Contains(evaluated[1], "author partial") ||
		strings.Contains(evaluated[1], "bot answer") || strings.Contains(evaluated[1], "I speak for author") {
		t.Fatalf("second round: %+v, %v", tr.Holds, evaluated)
	}
	gh.replies["#1"] = append(gh.replies["#1"], ghReply(6, "author", "User", "author partial", c.t.Add(time.Second)))
	tr = advance(r, c, t, tr.ID)
	if len(evaluated) != 2 {
		t.Fatal("duplicate answer triggered evaluation")
	}
	gh.replies["#1"] = append(gh.replies["#1"], ghReply(7, "weft-bot", "User", "owner partial", c.t.Add(time.Second)))
	tr = advance(r, c, t, tr.ID)
	if len(evaluated) != 3 || !strings.Contains(evaluated[2], "owner partial") ||
		tr.Holds[0].Reason != "round_limit" || len(gh.comments["#1"]) != 3 ||
		!strings.Contains(gh.comments["#1"][2], "operator") {
		t.Fatalf("round limit: %+v, comments %v", tr.Holds, gh.comments)
	}
	gh.find("#1")["body"] = "FINAL ACCEPTANCE"
	gh.find("#1")["labels"] = append(gh.find("#1")["labels"].([]map[string]any), map[string]any{"name": trigger.LabelTodo})
	gh.roles["maintainer"] = "maintain"
	gh.labelEvents["#1"] = []map[string]any{ghTodoEvent(1, "robot", "Bot", c.t.Add(time.Second))}
	tr = advance(r, c, t, tr.ID)
	if len(evaluated) != 3 {
		t.Fatal("bot label triggered evaluation")
	}
	gh.roles["contributor"] = "write"
	gh.labelEvents["#1"] = append(gh.labelEvents["#1"], ghTodoEvent(2, "contributor", "User", c.t.Add(time.Second)))
	tr = advance(r, c, t, tr.ID)
	if len(evaluated) != 3 {
		t.Fatal("label edit by non-maintainer triggered evaluation")
	}
	gh.labelEvents["#1"] = append(gh.labelEvents["#1"], ghTodoEvent(3, "maintainer", "User", c.t.Add(time.Second)))
	tr = advance(r, c, t, tr.ID)
	if len(evaluated) != 4 || len(tr.Holds) != 0 || len(tasks.started) != 1 ||
		!strings.Contains(trigger.Request(tasks.started[0]), "FINAL ACCEPTANCE") || tr.CurrentTask == "" {
		t.Fatalf("maintainer requeue: %+v, tasks %+v", tr, tasks.started)
	}
	labels = gh.find("#1")["labels"].([]map[string]any)
	if len(labels) != 2 || labels[0]["name"] != "bug" || labels[1]["name"] != trigger.LabelInProgress {
		t.Fatal(labels)
	}
}

func TestReadinessCapacityAndIndependentAdmission(t *testing.T) {
	for _, capacity := range []bool{false, true} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			for n := 1; n <= 11; n++ {
				gh.issues = append(gh.issues, ghIssue(n, fmt.Sprintf("Issue %d", n), trigger.LabelTodo))
			}
			r := runner(st, c, "one", gh)
			tasks := &tasks{status: map[string]trigger.TaskStatus{}}
			r.Tasks = tasks
			calls := 0
			r.Assessor = assessFunc(func(_ context.Context, _ *trigger.Trigger, input string) trigger.ReadinessAssessment {
				calls++
				if (!capacity && strings.Contains(input, "Issue 2")) || strings.Contains(input, "READY AFTER HOLD") {
					return trigger.ReadinessAssessment{}
				}
				return trigger.ReadinessAssessment{Missing: []string{"objective"}}
			})
			in := input()
			in.Label = ""
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil || tr.Label != trigger.LabelTodo || tr.ReadinessDisabled {
				t.Fatalf("default trigger: %+v, %v", tr, err)
			}
			tr = advance(r, c, t, tr.ID)
			if !capacity {
				tr = advance(r, c, t, tr.ID)
				if len(tr.Holds) != 1 || len(tasks.started) != 1 || tasks.started[0].Key != "acme/app#2" {
					t.Fatalf("hold occupied active slot: %+v, tasks %+v", tr.Holds, tasks.started)
				}
				return
			}
			for range 10 {
				tr = advance(r, c, t, tr.ID)
			}
			if calls != 10 || len(tr.Holds) != 10 || tr.ReadinessBlock == "" || len(tasks.started) != 0 || len(gh.comments["#11"]) != 0 {
				t.Fatalf("capacity: calls %d, holds %d, reason %q", calls, len(tr.Holds), tr.ReadinessBlock)
			}
			gh.replies["#1"] = []map[string]any{ghReply(1, "author", "User", "READY AFTER HOLD", c.t.Add(time.Second))}
			tr = advance(r, c, t, tr.ID)
			if len(tr.Holds) != 9 || tr.ReadinessBlock != "" || tr.CurrentTask == "" || len(tasks.started) != 1 {
				t.Fatalf("resolved hold did not release capacity: %+v", tr)
			}
			tasks.status[tr.CurrentTask] = trigger.TaskStatus{State: "succeeded", PullRequestURL: "https://example.test/pull/1"}
			tr = advance(r, c, t, tr.ID)
			tr = advance(r, c, t, tr.ID)
			if calls != 12 || len(gh.comments["#11"]) != 1 || len(tr.Holds) != 10 {
				t.Fatalf("new issue did not use freed capacity: calls %d, holds %d, comments %v", calls, len(tr.Holds), gh.comments)
			}

		})
	}
}

func TestReadinessRevalidatesSnapshotAndPreservesHoldsWhenDisabled(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
	r := runner(st, c, "one", gh)
	tasks := &tasks{}
	r.Tasks = tasks
	evaluations := 0
	r.Assessor = assessFunc(func(_ context.Context, _ *trigger.Trigger, _ string) trigger.ReadinessAssessment {
		evaluations++
		if evaluations == 1 {
			gh.find("#1")["body"] = "edited during model call"
			return trigger.ReadinessAssessment{}
		}
		return trigger.ReadinessAssessment{Missing: []string{"scope"}}
	})
	in := input()
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].Reason != "snapshot_changed" || len(tasks.started) != 0 ||
		slices.ContainsFunc(gh.rest, func(call string) bool { return strings.Contains(call, "/labels") }) {
		t.Fatalf("stale verdict changed tracker or started work: %+v", tr)
	}
	tr = advance(r, c, t, tr.ID)
	if evaluations != 2 || tr.Holds[0].Reason != "incomplete" {
		t.Fatalf("edited issue not reevaluated: %+v", tr)
	}
	consent := tr.Consent.Scope
	in.ReadinessDisabled = true
	tr, err = r.Triggers.Edit(t.Context(), tr.ID, in, "admin")
	if err != nil || tr.State != trigger.Paused || len(tr.Holds) != 1 {
		t.Fatalf("disable: %+v %v", tr, err)
	}
	tr, err = r.Triggers.Resume(t.Context(), tr.ID, "admin")
	if err != nil || tr.Consent.Scope == consent || slices.Contains(tr.Consent.Operations, "tracker.comments") {
		t.Fatalf("disabled consent = %+v %v", tr, err)
	}
	gh.find("#1")["labels"] = append(gh.find("#1")["labels"].([]map[string]any), map[string]any{"name": "weft"})
	tr = advance(r, c, t, tr.ID)
	if evaluations != 2 || len(tr.Holds) != 1 || len(tasks.started) != 0 {
		t.Fatalf("disable released a hold: %+v", tr)
	}
}

func TestReadinessRecoversUnconfirmedCommentWithoutRepeatingIt(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
	gh.loseCommentResponse = true
	r := runner(st, c, "first", gh)
	r.Tasks = &tasks{}
	modelCalls := 0
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		modelCalls++
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].Publication != "comment_pending" || len(gh.comments["#1"]) != 1 || tr.LastError == "" {
		t.Fatalf("uncertain write-back not recorded: %+v", tr)
	}
	gh.replies["#1"] = []map[string]any{
		ghReply(2, "author", "User", gh.comments["#1"][0], c.t.Add(-3*time.Minute)),
		ghReply(1, "weft-service", "User", gh.comments["#1"][0], c.t.Add(-2*time.Minute)),
	}
	restarted := runner(st, c, "second", gh)
	restarted.Tasks, restarted.Assessor = r.Tasks, r.Assessor
	tr = advance(restarted, c, t, tr.ID)
	if tr.Holds[0].Publication != "confirmed" || tr.Holds[0].CommentID != "1" || tr.Holds[0].CommentAuthor != "700" || len(gh.comments["#1"]) != 1 || modelCalls != 1 || tr.CurrentTask != "" {
		t.Fatalf("restart did not reconcile the existing comment: %+v", tr)
	}
}

func TestReadinessScopeAndBudgetChangesInvalidateConsentBeforePolling(t *testing.T) {
	for _, field := range []string{"connection", "money"} {
		t.Run(field, func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
			r := runner(st, c, "one", gh)
			revision := int64(1)
			r.Triggers.TrackerRevision = func(context.Context, string, string) (int64, error) { return revision, nil }
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			if field == "connection" {
				revision++
			} else {
				r.Triggers.ReadinessMaxMicros = 1
			}
			tr = advance(r, c, t, tr.ID)
			if tr.State != trigger.Paused || tr.PausedReason == "" || len(gh.calls)+len(gh.rest) != 0 || len(tr.Holds) != 0 {
				t.Fatalf("stale consent made a tracker call: %+v", tr)
			}
		})
	}
}

func TestHeldIssueRechecksEligibilityBeforeReopening(t *testing.T) {
	for _, state := range []string{"closed", "blocked", "assigned", "processed", "unlabeled"} {
		t.Run(state, func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
			r := runner(st, c, "one", gh)
			tasks := &tasks{}
			r.Tasks = tasks
			calls := 0
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				calls++
				if calls == 1 {
					return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
				}
				return trigger.ReadinessAssessment{}
			})
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			issue := gh.find("#1")
			switch state {
			case "closed":
				issue["state"] = "closed"
			case "blocked":
				issue["labels"] = append(issue["labels"].([]map[string]any), map[string]any{"name": "blocked"})
			case "assigned":
				issue["assignees"] = []map[string]any{{"login": "other"}}
			case "unlabeled":
				issue["labels"] = []map[string]any{}
			case "processed":
				_, err := r.Triggers.Update(t.Context(), tr.ID, "fixture", func(current *trigger.Trigger) error {
					current.Processed = map[string]string{"github:acme/app#1@factory": "tsk_prior"}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			gh.replies["#1"] = []map[string]any{ghReply(1, "author", "User", "Acceptance example", c.t.Add(time.Second))}
			tr = advance(r, c, t, tr.ID)
			if calls != 1 || len(tasks.started) != 0 || len(tr.Holds) != 0 || len(tr.HoldResolutions) != 1 || tr.HoldResolutions[0].Reason == "" {
				t.Fatalf("ineligible issue was reevaluated or admitted: %+v", tr.Holds)
			}
		})
	}
}

func TestClosedHoldsReleaseFullCapacity(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	for n := 1; n <= 11; n++ {
		gh.issues = append(gh.issues, ghIssue(n, "Issue", "weft"))
	}
	r := runner(st, c, "one", gh)
	r.Tasks = &tasks{}
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		tr = advance(r, c, t, tr.ID)
	}
	if len(tr.Holds) != 10 || tr.ReadinessBlock == "" {
		t.Fatal(tr)
	}
	for n := range 10 {
		gh.issues[n]["state"] = "closed"
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.HoldResolutions) != 10 || len(tr.Holds) != 1 || tr.Holds[0].Key != "acme/app#11" || tr.ReadinessBlock != "" {
		t.Fatalf("closed holds stranded the queue: %+v", tr)
	}
}

func TestReadinessCommentRetriesOnlyAfterReconciliation(t *testing.T) {
	for _, status := range []int{403, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
			const post = "POST /repos/acme/app/issues/1/comments"
			if status == 403 {
				gh.fail[post] = true
			} else {
				gh.transientComments = 1
			}
			r := runner(st, c, "one", gh)
			r.Tasks = &tasks{}
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
			})
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			h := tr.Holds[0]
			if h.CommentAttempts != 1 || len(gh.comments["#1"]) != 0 {
				t.Fatal(h)
			}
			if status == 403 {
				if h.Publication != "comment_failed" || !strings.Contains(h.PublicationMessage, "permission to write issue comments") {
					t.Fatal(h)
				}
				tr = advance(r, c, t, tr.ID)
				if tr.Holds[0].CommentAttempts != 1 || strings.Count(strings.Join(gh.rest, "\n"), post) != 1 {
					t.Fatal(gh.rest)
				}
				return
			}
			if h.Publication != "comment_pending" || h.RetryCommentAt == nil || !h.RetryCommentAt.After(c.t) {
				t.Fatal(h)
			}
			tr = advance(r, c, t, tr.ID)
			log := strings.Join(gh.rest, "\n")
			if tr.Holds[0].Publication != "confirmed" || tr.Holds[0].CommentAttempts != 2 || len(gh.comments["#1"]) != 1 ||
				!strings.Contains(log, "GET /repos/acme/app/issues/1/comments\nGET /user\n"+post) {
				t.Fatalf("retry was not reconciled: %+v %v", tr.Holds, gh.rest)
			}
		})
	}
}

func TestFailedAssessmentsExhaustRoundsDespiteRepeatedAuthorReplies(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
	r := runner(st, c, "one", gh)
	r.Tasks = &tasks{}
	calls := 0
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		calls++
		return trigger.ReadinessAssessment{Failure: "timeout"}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if calls != 1 || tr.Holds[0].Round != 1 || tr.Holds[0].Reason != "timeout" {
		t.Fatal(tr.Holds)
	}
	for n := 1; n <= 5; n++ {
		gh.replies["#1"] = append(gh.replies["#1"], ghReply(n, "author", "User", fmt.Sprintf("Acceptance example %d", n), c.t.Add(time.Second)))
		tr = advance(r, c, t, tr.ID)
	}
	if calls != 2 || tr.Holds[0].Round != 2 || tr.Holds[0].Reason != "failure_round_limit" || len(gh.comments["#1"]) != 2 ||
		!strings.Contains(gh.comments["#1"][1], "operator should review") || tr.CurrentTask != "" {
		t.Fatalf("failed evaluations exceeded the allowance: calls %d, holds %+v, comments %v", calls, tr.Holds, gh.comments)
	}
}

func TestChangedSnapshotRollsBackClaimAndNewAssignment(t *testing.T) {
	for _, preassigned := range []bool{false, true} {
		t.Run(fmt.Sprint(preassigned), func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.issues = []map[string]any{ghIssue(1, "Issue", "weft", "bug")}
			if preassigned {
				gh.issues[0]["assignees"] = []map[string]any{{"login": "weft-bot"}}
			}
			gh.editOnAssignment = true
			r := runner(st, c, "one", gh)
			starter := &tasks{}
			r.Tasks = starter
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			row := gh.issues[0]
			assignees := row["assignees"].([]map[string]any)
			labels := row["labels"].([]map[string]any)
			if (len(assignees) == 1) != preassigned || len(labels) != 2 || labels[0]["name"] != "bug" ||
				labels[1]["name"] != trigger.LabelFailed || len(starter.started) != 0 || len(tr.Holds) != 1 || tr.Holds[0].Reason != "snapshot_changed" {
				t.Fatalf("claim not restored: issue %+v, holds %+v", row, tr.Holds)
			}
		})
	}
}

func TestHistoryLimitBecomesAResolvableHoldWithoutRepeatedScans(t *testing.T) {
	for _, kind := range []string{"comments", "events", "reconciliation"} {
		t.Run(kind, func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
			gh.loseCommentResponse = kind == "reconciliation"
			r := runner(st, c, "one", gh)
			r.Tasks = &tasks{}
			assessments := 0
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				assessments++
				return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
			})
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			scan := "comments"
			for n := range 100 {
				if kind == "events" {
					scan = "events"
					gh.labelEvents["#1"] = append(gh.labelEvents["#1"], ghTodoEvent(n+1, "author", "User", c.t.Add(time.Second)))
				} else {
					gh.replies["#1"] = append(gh.replies["#1"], ghReply(n+1, "author", "User", "example", c.t.Add(time.Second)))
				}
			}
			if kind == "events" {
				i := gh.issues[0]
				i["labels"] = append(i["labels"].([]map[string]any), map[string]any{"name": trigger.LabelTodo})
			}
			tr = advance(r, c, t, tr.ID)
			reads := func() int { return strings.Count(strings.Join(gh.rest, "\n"), "GET /repos/acme/app/issues/1/"+scan) }
			if tr.LastError != "" || tr.Holds[0].Reason != "history_limit" || !strings.Contains(tr.Holds[0].Message, "resolve the hold") || reads() != 10 {
				t.Fatalf("history overflow was not held: %+v, reads %d", tr, reads())
			}
			tr = advance(r, c, t, tr.ID)
			if reads() != 10 || assessments != 1 || len(gh.comments["#1"]) != 1 {
				t.Fatal("terminal scan repeated or duplicated publication")
			}
			resolved, err := r.Triggers.ResolveHold(t.Context(), tr.ID, tr.Holds[0].Key, tr.Holds[0].Generation, "operator")
			if err != nil || len(resolved.Holds) != 0 || len(resolved.HoldResolutions) != 1 {
				t.Fatalf("resolve: %+v %v", resolved, err)
			}
		})
	}
}

func TestHeldIssueReturnsWithTargetedQueryBeyondFirstAdmissionPage(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.pageSearch = true
	gh.issues = []map[string]any{ghIssue(60, "Target", "weft", "bug")}
	r := runner(st, c, "one", gh)
	starter := &tasks{}
	r.Tasks = starter
	r.Assessor = assessFunc(func(_ context.Context, _ *trigger.Trigger, input string) trigger.ReadinessAssessment {
		if strings.Contains(input, "expected output") {
			return trigger.ReadinessAssessment{}
		}
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	in := input()
	in.Source.Query = "milestone:release"
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 {
		t.Fatal(tr)
	}
	target := gh.issues[0]
	gh.issues = nil
	for n := 1; n < 60; n++ {
		gh.issues = append(gh.issues, ghIssue(n, "Older", "weft"))
	}
	gh.issues = append(gh.issues, target)
	gh.replies["#60"] = []map[string]any{ghReply(1, "author", "User", "expected output", c.t.Add(time.Second))}
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 1 || starter.started[0].Key != "acme/app#60" || len(tr.Holds) != 0 ||
		len(gh.args) != 2 || !strings.Contains(gh.args[1]["query"].(string), "created:2026-11-01T00:01:00Z milestone:release") {
		t.Fatalf("held issue was not found beyond the first page: %+v", tr)
	}
	if got := target["labels"].([]map[string]any); len(got) != 2 || got[0]["name"] != "bug" || got[1]["name"] != trigger.LabelInProgress {
		t.Fatalf("claim did not preserve unrelated labels: %v", got)
	}
}

func TestResolveLeavesWaitingLabelUntilReselectedAndClaimed(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	row := ghIssue(1, "Issue", "weft", "bug")
	gh.issues = []map[string]any{row}
	r := runner(st, c, "one", gh)
	starter := &tasks{}
	r.Tasks = starter
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	in := input()
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	h := tr.Holds[0]
	in.ReadinessDisabled = true
	if _, err := r.Triggers.Edit(t.Context(), tr.ID, in, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Triggers.Resume(t.Context(), tr.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	calls := len(gh.rest)
	if _, err := r.Triggers.ResolveHold(t.Context(), tr.ID, h.Key, h.Generation, "operator"); err != nil {
		t.Fatal(err)
	}
	if len(gh.rest) != calls {
		t.Fatal("manual resolution wrote to the tracker")
	}
	tr = advance(r, c, t, tr.ID)
	if tr.CurrentTask != "" {
		t.Fatal("resolution started an unselected issue")
	}
	row["labels"] = append(row["labels"].([]map[string]any), map[string]any{"name": "weft"})
	tr = advance(r, c, t, tr.ID)
	if tr.CurrentTask == "" {
		t.Fatal("reselected issue did not start")
	}
	if got := row["labels"].([]map[string]any); len(got) != 2 || got[0]["name"] != "bug" || got[1]["name"] != trigger.LabelInProgress {
		t.Fatal(got)
	}
}

func TestRepeatedSnapshotChangesStopReadinessAndPublishOnlyIntervention(t *testing.T) {
	for _, duringClaim := range []bool{false, true} {
		t.Run(fmt.Sprint(duringClaim), func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			row := ghIssue(1, "Issue", "weft")
			row["assignees"] = []map[string]any{}
			gh.issues = []map[string]any{row}
			gh.editOnAssignment = duringClaim
			r := runner(st, c, "one", gh)
			starter := &tasks{}
			r.Tasks = starter
			calls := 0
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				calls++
				if !duringClaim {
					row["body"] = fmt.Sprint(calls)
				}
				return trigger.ReadinessAssessment{}
			})
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			if len(gh.comments["#1"]) != 0 || tr.Holds[0].Round != 0 || tr.Holds[0].SnapshotEdits != 1 {
				t.Fatalf("first invalidation: %+v", tr.Holds)
			}
			first := tr.Holds[0]
			for range 4 {
				tr = advance(r, c, t, tr.ID)
			}
			if calls != 3 || len(starter.started) != 0 || len(gh.comments["#1"]) != 1 ||
				tr.Holds[0].Round != 0 || tr.Holds[0].SnapshotEdits != 3 ||
				tr.Holds[0].Reason != "snapshot_round_limit" || len(row["assignees"].([]map[string]any)) != 0 {
				t.Fatalf("stale snapshots were not bounded: %d calls, holds %+v, comments %+v", calls, tr.Holds, gh.comments)
			}
			if tr.Holds[0].Generation == first.Generation || !tr.Holds[0].OpenedAt.Equal(first.OpenedAt) {
				t.Fatalf("the edit did not rotate the same hold: %+v -> %+v", first, tr.Holds[0])
			}
			if _, err := r.Triggers.ResolveHold(t.Context(), tr.ID, first.Key, first.Generation, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatalf("stale operator action returned %v", err)
			}
		})
	}
}

func TestReadinessLabelWriteStopsOnTrackerRefusalOrMissingIssue(t *testing.T) {
	for _, status := range []int{403, 404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			st := triggerStore(t)
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
			const write = "POST /repos/acme/app/issues/1/labels"
			gh.status[write] = status
			if status == 404 {
				gh.status[write] = 503
			}
			r := runner(st, c, "one", gh)
			r.Tasks = &tasks{}
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
			})
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			if status == 404 {
				gh.issues = nil
				delete(gh.status, write)
			}
			for range 5 {
				tr = advance(r, c, t, tr.ID)
			}
			wantAttempts := map[int]int{403: 1, 404: 2, 503: 3}[status]
			if len(tr.Holds) != 1 || tr.Holds[0].Publication != "label_failed" ||
				!strings.Contains(tr.Holds[0].PublicationMessage, "assignment") ||
				strings.Count(strings.Join(gh.rest, "\n"), write) != wantAttempts || len(gh.comments) != 0 {
				t.Fatalf("hold: %+v, requests: %v", tr.Holds, gh.rest)
			}
			h := tr.Holds[0]
			if _, err := r.Triggers.ResolveHold(t.Context(), tr.ID, h.Key, h.Generation, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatalf("regular resolution returned %v", err)
			}
			resolved, err := r.Triggers.ForceResolveHold(t.Context(), tr.ID, h.Key, h.Generation, "operator")
			if err != nil || len(resolved.Holds) != 0 || !strings.Contains(resolved.HoldResolutions[0].Reason, "abandoned") {
				t.Fatalf("forced resolution: %+v %v", resolved, err)
			}
		})
	}
}

func TestPendingClaimRollbackRunsWithoutReadiness(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			row := ghIssue(1, "Issue", "weft", "bug")
			gh.issues = []map[string]any{row}
			gh.editOnAssignment = true
			const undo = "DELETE /repos/acme/app/issues/1/assignees"
			gh.status[undo] = 503
			r := runner(triggerStore(t), c, "one", gh)
			starter := &tasks{}
			r.Tasks = starter
			in := input()
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			if len(tr.Holds) != 1 || tr.Holds[0].UndoOwner == "" || tr.Claiming != "" {
				t.Fatalf("rollback was not persisted: %+v", tr)
			}
			delete(gh.status, undo)
			if disabled {
				in.ReadinessDisabled = true
				if _, err := r.Triggers.Edit(t.Context(), tr.ID, in, "admin"); err != nil {
					t.Fatal(err)
				}
				if _, err := r.Triggers.Resume(t.Context(), tr.ID, "admin"); err != nil {
					t.Fatal(err)
				}
			} else if _, err := r.Triggers.Pause(t.Context(), tr.ID, "operator paused", "admin"); err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			labels := row["labels"].([]map[string]any)
			if len(tr.Holds) != 1 || tr.Holds[0].Publication != "confirmed" || tr.Holds[0].UndoOwner != "" ||
				len(starter.started) != 0 || len(gh.comments) != 0 || len(row["assignees"].([]map[string]any)) != 0 ||
				len(labels) != 2 || labels[0]["name"] != "bug" || labels[1]["name"] != trigger.LabelFailed {
				t.Fatalf("rollback not completed: %+v, issue %+v", tr, row)
			}
		})
	}
}

func TestReadinessEditLeavesBothQuestionRoundsAvailable(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	row := ghIssue(1, "Issue", "weft")
	gh.issues = []map[string]any{row}
	r := runner(triggerStore(t), c, "one", gh)
	r.Tasks = &tasks{}
	calls := 0
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		calls++
		if calls == 1 {
			row["body"] = "Edited during the first assessment."
		}
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if tr.Holds[0].Round != 0 || tr.Holds[0].SnapshotEdits != 1 || len(gh.comments) != 0 {
		t.Fatalf("the stale verdict spent a question: %+v", tr.Holds)
	}
	tr = advance(r, c, t, tr.ID)
	gh.replies["#1"] = append(gh.replies["#1"], ghReply(9, "author", "User", "Here is a partial answer.", c.t.Add(time.Second)))
	tr = advance(r, c, t, tr.ID)
	comments := gh.comments["#1"]
	if tr.Holds[0].Round != 2 || tr.Holds[0].SnapshotEdits != 1 || len(comments) != 2 ||
		!strings.Contains(comments[0], "round 1 of 2") || !strings.Contains(comments[1], "round 2 of 2") {
		t.Fatalf("questions: %v, hold: %+v", comments, tr.Holds)
	}
}

type admissionFaultStore struct {
	storage.Store
	fault string
}

func (s *admissionFaultStore) AcquireLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	if strings.HasPrefix(key, "trigger-admissions/") {
		switch s.fault {
		case "busy":
			return false, nil
		case "lease":
			return false, errors.New("fixture lease error")
		}
	}
	return s.Store.AcquireLease(ctx, key, holder, ttl)
}

func (s *admissionFaultStore) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	if s.fault == "store" && strings.HasPrefix(key, "trigger-admissions/") {
		return nil, errors.New("fixture store error")
	}
	return s.Store.GetSetting(ctx, key)
}

func TestTransientAdmissionFailuresPreserveReadinessAllowances(t *testing.T) {
	for _, fault := range []string{"busy", "lease", "store", "claim read"} {
		t.Run(fault, func(t *testing.T) {
			st := &admissionFaultStore{Store: triggerStore(t), fault: fault}
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			row := ghIssue(1, "Issue", "weft")
			gh.issues = []map[string]any{row}
			want := "claim_busy"
			if fault == "claim read" {
				gh.claimReadFailures = 1
				want = "claim_recheck_unavailable"
			}
			r := runner(st, c, "one", gh)
			starter := &tasks{}
			r.Tasks = starter
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			h := tr.Holds[0]
			if h.Reason != want || h.Round != 0 || h.SnapshotEdits != 0 || h.RetryFailures != 1 || h.RetryAt == nil ||
				tr.Claiming != "" || len(starter.started) != 0 || len(gh.comments) != 0 {
				t.Fatalf("transient failure spent readiness or left a claim: %+v", tr)
			}
			if fault == "claim read" && (len(row["assignees"].([]map[string]any)) != 0 || row["labels"].([]map[string]any)[0]["name"] != trigger.LabelFailed) {
				t.Fatalf("tracker claim not rolled back: %+v", row)
			}
			st.fault = ""
			tr = advance(r, c, t, tr.ID)
			if len(starter.started) != 1 || len(tr.Holds) != 0 || len(gh.comments["#1"]) != 1 ||
				!strings.Contains(gh.comments["#1"][0], "started working") {
				t.Fatalf("retry failed: %+v, comments %v", tr, gh.comments)
			}
		})
	}
}

func TestReadinessTrackerIdentityRetryDoesNotSpendCommentAttempts(t *testing.T) {
	for _, kind := range []string{"github", "jira"} {
		t.Run(kind, func(t *testing.T) {
			for _, recoverIdentity := range []bool{true, false} {
				t.Run(fmt.Sprint(recoverIdentity), func(t *testing.T) {
					c := &clock{t: time.Now().UTC()}
					in := input()
					gh := newTracker("gh")
					gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
					lookup, key := "GET /user", "#1"
					if kind == "jira" {
						in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "atl", CloudID: cloud}
						in.Owner = "acc-1"
						gh = newTracker("atl")
						gh.issues = []map[string]any{jiraIssue("APP-1", "Issue", "weft")}
						lookup, key = "GET /ex/jira/"+cloud+"/rest/api/3/myself", "APP-1"
					}
					gh.status[lookup] = 503
					r := runner(triggerStore(t), c, "one", gh)
					r.Tasks = &tasks{}
					r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
						return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
					})
					tr, err := r.Triggers.Create(t.Context(), in, "admin")
					if err != nil {
						t.Fatal(err)
					}
					tr = advance(r, c, t, tr.ID)
					h := tr.Holds[0]
					if h.Round != 1 || h.SnapshotEdits != 0 || h.Publication != "label_applied" || h.CommentAttempts != 0 ||
						h.RetryCommentAt == nil || !strings.Contains(h.PublicationMessage, "backoff") || len(gh.comments) != 0 {
						t.Fatalf("identity failure spent a comment: %+v", h)
					}
					if recoverIdentity {
						delete(gh.status, lookup)
						tr = advance(r, c, t, tr.ID)
						if tr.Holds[0].Round != 1 || tr.Holds[0].CommentAttempts != 1 || tr.Holds[0].Publication != "confirmed" ||
							len(gh.comments[key]) != 1 || !strings.Contains(gh.comments[key][0], "round 1 of 2") {
							t.Fatalf("identity retry did not publish original question: %+v, %v", tr.Holds, gh.comments)
						}
					} else {
						for range 5 {
							tr = advance(r, c, t, tr.ID)
						}
						if tr.Holds[0].Round != 1 || tr.Holds[0].CommentAttempts != 0 || tr.Holds[0].Publication != "comment_failed" ||
							strings.Count(strings.Join(gh.rest, "\n"), lookup) != 4 || len(gh.comments) != 0 {
							t.Fatalf("identity lookup did not stop: %+v, %v", tr.Holds, gh.rest)
						}
					}
				})
			}
		})
	}
}

func TestHeldGitHubSearchBudgetRotatesTenIssues(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.pageSearch = true
	for n := 1; n <= 60; n++ {
		gh.issues = append(gh.issues, ghIssue(n, "Issue", "weft"))
	}
	r := runner(triggerStore(t), c, "one", gh)
	r.Tasks = &tasks{}
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	in := input()
	in.Source.Query = "milestone:release"
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		before := len(gh.args)
		tr = advance(r, c, t, tr.ID)
		if len(gh.args)-before > 9 {
			t.Fatalf("poll exceeded its search budget: %d", len(gh.args)-before)
		}
	}
	if len(tr.Holds) != 10 {
		t.Fatalf("wanted ten held issues, got %d", len(tr.Holds))
	}
	seen := map[string]bool{}
	for range 2 {
		before := len(gh.args)
		tr = advance(r, c, t, tr.ID)
		if len(gh.args)-before != 8 {
			t.Fatalf("held scope searches: %d", len(gh.args)-before)
		}
		for _, args := range gh.args[before:] {
			query := args["query"].(string)
			if !strings.Contains(query, " created:") || args["page"] != float64(1) {
				t.Fatalf("unbounded held scope query: %v", args)
			}
			seen[query] = true
		}
		deferred := 0
		for _, h := range tr.Holds {
			if strings.Contains(h.Eligibility, "search budget") {
				deferred++
			}
			if h.Round != 1 {
				t.Fatalf("verification changed rounds: %+v", h)
			}
		}
		if deferred != 2 {
			t.Fatalf("wanted two visible deferrals, got %d", deferred)
		}
	}
	if len(seen) != 10 {
		t.Fatalf("only %d holds received a search turn", len(seen))
	}
}

func TestGitHubScopeReleaseRequiresTwoConfirmedMissesAndPreservesRounds(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.pageSearch = true
	row := ghIssue(1, "Issue", "weft")
	gh.issues = []map[string]any{row}
	r := runner(triggerStore(t), c, "one", gh)
	r.Tasks = &tasks{}
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	in := input()
	in.Source.Query = "milestone:release"
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	gh.unindexed[1] = true
	for range 2 {
		tr = advance(r, c, t, tr.ID)
		if len(tr.Holds) != 1 || tr.Holds[0].Round != 1 || !strings.Contains(tr.Holds[0].Eligibility, "has not indexed") {
			t.Fatalf("lagging index released the hold: %+v", tr)
		}
	}
	delete(gh.unindexed, 1)
	gh.outsideQuery[1] = true
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || !strings.Contains(tr.Holds[0].Eligibility, "another poll") {
		t.Fatalf("first miss released the hold: %+v", tr)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 0 || len(tr.HoldResolutions) != 1 || tr.HoldResolutions[0].Round != 1 ||
		tr.HoldResolutions[0].Actor != "system:trigger" {
		t.Fatalf("second verified miss did not release the hold: %+v", tr)
	}
	delete(gh.outsideQuery, 1)
	row["labels"] = append(row["labels"].([]map[string]any), map[string]any{"name": "weft"})
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].Round != 2 || len(gh.comments["#1"]) != 2 ||
		!strings.Contains(gh.comments["#1"][1], "round 2 of 2") {
		t.Fatalf("rounds lost on return: %+v, %v", tr, gh.comments)
	}
}

func TestIncompleteGitHubAdmissionSearchReportsTheSearchFailure(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
	gh.incompleteSearch = true
	r := runner(triggerStore(t), c, "one", gh)
	starter := &tasks{}
	r.Tasks = starter
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if tr.LastError != "GitHub returned incomplete issue search results" || len(starter.started) != 0 || len(tr.Holds) != 0 {
		t.Fatalf("incomplete search admitted work or reported the wrong error: %+v", tr)
	}
}

func TestClaimAfterScopeEditCleansWaitingLabelBeyondResolutionHistory(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.pageSearch = true
	row := ghIssue(1, "Issue", "weft", "bug")
	gh.issues = []map[string]any{row}
	r := runner(triggerStore(t), c, "one", gh)
	starter := &tasks{}
	r.Tasks = starter
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	in := input()
	tr, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 {
		t.Fatal(tr.Holds)
	}
	key := tr.Holds[0].Key
	in.Source.Query = "milestone:release"
	editAndResume := func() {
		t.Helper()
		if _, err := r.Triggers.Edit(t.Context(), tr.ID, in, "admin"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Triggers.Resume(t.Context(), tr.ID, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	editAndResume()
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 0 || len(tr.HoldResolutions) != 1 || tr.HoldResolutions[0].Actor != "system:trigger" {
		t.Fatalf("scope edit did not release hold: %+v", tr)
	}
	audit, err := r.Store.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{domain.EvTriggerHoldResolved}})
	if err != nil || len(audit) != 1 || audit[0].Actor != "system:trigger" {
		t.Fatalf("automatic scope resolution audit: %+v, %v", audit, err)
	}
	for n := 2; n <= 21; n++ {
		other := trigger.Hold{Key: fmt.Sprintf("acme/app#%d", n), Generation: fmt.Sprint(n)}
		if _, err := r.Triggers.Update(t.Context(), tr.ID, "fixture", func(current *trigger.Trigger) error {
			current.Holds = []trigger.Hold{other}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Triggers.ResolveHold(t.Context(), tr.ID, other.Key, other.Generation, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	in.ReadinessDisabled = true
	editAndResume()
	row["labels"] = append(row["labels"].([]map[string]any), map[string]any{"name": "weft"})
	tr = advance(r, c, t, tr.ID)
	labels := row["labels"].([]map[string]any)
	if len(tr.HoldResolutions) != 20 || slices.ContainsFunc(tr.HoldResolutions, func(h trigger.HoldResolution) bool { return h.Key == key }) ||
		len(starter.started) != 1 || len(labels) != 2 || labels[0]["name"] != "bug" || labels[1]["name"] != trigger.LabelInProgress {
		t.Fatalf("claim depended on resolution history: %+v, labels %v", tr, labels)
	}
}

func TestGitHubIndexLagProcessesVerifiedReturnSignalsWithoutClaiming(t *testing.T) {
	for _, requeue := range []bool{false, true} {
		t.Run(fmt.Sprintf("requeue=%v", requeue), func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.pageSearch = true
			row := ghIssue(1, "Issue", "weft")
			gh.issues = []map[string]any{row}
			r := runner(triggerStore(t), c, "one", gh)
			starter := &tasks{}
			r.Tasks = starter
			assessments := 0
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				assessments++
				if assessments == 1 {
					return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
				}
				return trigger.ReadinessAssessment{}
			})
			in := input()
			in.Source.Query = "milestone:release"
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			gh.unindexed[1] = true
			if requeue {
				gh.roles["maintainer"] = "maintain"
				gh.labelEvents["#1"] = []map[string]any{ghTodoEvent(7, "maintainer", "User", c.t.Add(time.Second))}
				row["labels"] = append(row["labels"].([]map[string]any), map[string]any{"name": trigger.LabelTodo})
			} else {
				gh.replies["#1"] = append(gh.replies["#1"], ghReply(9, "author", "User", "The CSV output uses the header Account.", c.t.Add(time.Second)))
			}
			for range 2 {
				tr = advance(r, c, t, tr.ID)
			}
			if assessments != 1 {
				t.Fatal("fallback ran before three lag observations")
			}
			tr = advance(r, c, t, tr.ID)
			if assessments != 2 || len(tr.Holds) != 1 || tr.Holds[0].State != "scope_waiting" ||
				!strings.Contains(tr.Holds[0].Eligibility, "direct GitHub reads") || len(starter.started) != 0 {
				t.Fatalf("fallback did not safely process the signal: %+v", tr.Holds)
			}
			tr = advance(r, c, t, tr.ID)
			if assessments != 2 || len(starter.started) != 0 {
				t.Fatal("unchanged REST verdict was evaluated again or admitted without query evidence")
			}
			delete(gh.unindexed, 1)
			tr = advance(r, c, t, tr.ID)
			if assessments != 3 || len(starter.started) != 1 || len(tr.Holds) != 0 {
				t.Fatalf("fresh verified query did not admit: %+v", tr)
			}
		})
	}
}

func TestSharedGitHubBudgetRotatesAcrossFourTriggers(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.pageSearch = true
	gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
	r := runner(triggerStore(t), c, "one", gh)
	r.Tasks = &tasks{}
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	in := input()
	in.Source.Query = "milestone:release"
	first, err := r.Triggers.Create(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	first = advance(r, c, t, first.ID)
	holds := append([]trigger.Hold(nil), first.Holds...)
	for n := 2; n <= 10; n++ {
		gh.issues = append(gh.issues, ghIssue(n, "Issue", trigger.LabelFailed))
		hold := first.Holds[0]
		hold.Key = fmt.Sprintf("acme/app#%d", n)
		holds = append(holds, hold)
	}
	triggers := []*trigger.Trigger{first}
	for n := 2; n <= 4; n++ {
		c.t = c.t.Add(time.Second)
		in.Name = fmt.Sprintf("Trigger %d", n)
		tr, err := r.Triggers.Create(t.Context(), in, "admin")
		if err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, tr)
	}
	for _, tr := range triggers {
		if _, err := r.Triggers.Update(t.Context(), tr.ID, "fixture", func(t *trigger.Trigger) error {
			t.Holds = holds
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	deferred := map[string]bool{}
	for range 4 {
		before := len(gh.args)
		advance(r, c, t, first.ID)
		if len(gh.args)-before != 30 {
			t.Fatalf("shared window dispatched %d searches", len(gh.args)-before)
		}
		current, err := r.Triggers.List(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		numDeferred := 0
		for _, tr := range current {
			if tr.SearchDeferredReason != "" {
				numDeferred++
				deferred[tr.ID] = true
			}
			if tr.LastError != "" || tr.SearchTurn == 0 {
				t.Fatalf("deferral was an error or turn was lost: %+v", tr)
			}
		}
		if numDeferred != 1 {
			t.Fatalf("expected one visibly deferred trigger, got %d", numDeferred)
		}
	}
	if len(deferred) != 4 {
		t.Fatalf("the same triggers kept getting deferred: %v", deferred)
	}
}

func TestReadinessBoundsEligibilityChangesDuringEvaluation(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	row := ghIssue(1, "Issue", "weft")
	gh.issues = []map[string]any{row}
	r := runner(triggerStore(t), c, "one", gh)
	starter := &tasks{}
	r.Tasks = starter
	evaluations := 0
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		evaluations++
		row["labels"] = []map[string]any{{"name": "weft"}, {"name": fmt.Sprintf("scope-%d", evaluations)}}
		return trigger.ReadinessAssessment{}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		tr = advance(r, c, t, tr.ID)
	}
	if len(tr.Holds) != 1 || tr.Holds[0].Reason != "eligibility_edit_limit" || tr.Holds[0].SnapshotEdits != 3 ||
		tr.Holds[0].Round != 0 || evaluations != 3 || len(starter.started) != 0 || len(gh.comments["#1"]) != 1 ||
		!strings.Contains(gh.comments["#1"][0], "eligibility change") {
		t.Fatalf("eligibility invalidations were not bounded: %+v, assessments=%d, comments=%v", tr.Holds, evaluations, gh.comments)
	}
	labels := row["labels"].([]map[string]any)
	if !slices.ContainsFunc(labels, func(label map[string]any) bool { return label["name"] == trigger.LabelFailed }) {
		t.Fatalf("missing intervention label: %v", labels)
	}
}

func TestPollingMaintenancePrunesExpiredReadinessOnPausedTrigger(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	r := runner(triggerStore(t), c, "one", gh)
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Triggers.Pause(t.Context(), tr.ID, "fixture paused", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Triggers.Update(t.Context(), tr.ID, "fixture", func(t *trigger.Trigger) error {
		t.RetainedReadiness = map[string]trigger.ReadinessProgress{
			"expired": {Round: 2, RetainedAt: c.t.Add(-30 * 24 * time.Hour)},
			"recent":  {Round: 1, RetainedAt: c.t.Add(-time.Hour)},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.RetainedReadiness) != 1 || tr.RetainedReadiness["recent"].Round != 1 ||
		tr.State != trigger.Paused || tr.LastRunAt != nil || len(gh.calls) != 0 || len(gh.rest) != 0 {
		t.Fatalf("maintenance dispatched polling or failed to prune: %+v", tr)
	}
}

func TestMissingHeldIssuePreservesProgressAndOperatorHolds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		repository  int
		operator    bool
		wantRelease bool
		disabled    bool
	}{
		{"repository hidden", 404, 404, false, false, false},
		{"repository refused", 404, 403, false, false, false},
		{"consecutive absence", 404, 0, false, true, false},
		{"deleted", 410, 0, false, true, false},
		{"operator absent", 404, 0, true, false, false},
		{"operator deleted", 410, 0, true, false, false},
		{"repository Issues disabled", 410, 0, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.issues = []map[string]any{ghIssue(1, "Issue", "weft")}
			r := runner(triggerStore(t), c, "one", gh)
			r.Tasks = &tasks{}
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
			})
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			if _, err := r.Triggers.Update(t.Context(), tr.ID, "fixture", func(t *trigger.Trigger) error {
				h := &t.Holds[0]
				h.SnapshotEdits = 2
				if tc.operator {
					h.State, h.Reason = "operator", "eligibility_edit_limit"
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			gh.status["GET /repos/acme/app/issues/1"] = tc.status
			gh.status["GET /repos/acme/app"] = tc.repository
			gh.githubIssuesDisabled = tc.disabled
			tr = advance(r, c, t, tr.ID)
			if tc.status == 404 {
				if len(tr.Holds) != 1 || len(tr.HoldResolutions) != 0 {
					t.Fatalf("first 404 released the hold: %+v", tr)
				}
				tr = advance(r, c, t, tr.ID)
			}
			if tc.wantRelease {
				progress := tr.RetainedReadiness["github:acme/app#1@factory"]
				if len(tr.Holds) != 0 || len(tr.HoldResolutions) != 1 || progress.Round != 1 || progress.SnapshotEdits != 2 {
					t.Fatalf("release did not preserve progress: %+v", tr)
				}
			} else if len(tr.Holds) != 1 || len(tr.HoldResolutions) != 0 {
				t.Fatalf("unsafe release: %+v", tr)
			}
			if tc.repository != 0 && !strings.Contains(tr.LastError, "could not access the configured GitHub repository") {
				t.Fatalf("repository loss is not visible: %+v", tr)
			}
			if tc.operator && (!strings.Contains(tr.Holds[0].Eligibility, "could not be found") || tr.Holds[0].Reason != "eligibility_edit_limit") {
				t.Fatalf("operator issue loss is not visible or changed the hold reason: %+v", tr.Holds)
			}
			probes := slices.DeleteFunc(slices.Clone(gh.rest), func(s string) bool { return s != "GET /repos/acme/app" })
			if tc.disabled && !strings.Contains(tr.LastError, "Issues are disabled") {
				t.Fatalf("disabled Issues is not visible: %+v", tr)
			}
			if (tc.status == 404 && len(probes) != 2) || (tc.status == 410 && len(probes) != 1) {
				t.Fatalf("unexpected repository probes: %v", gh.rest)
			}
		})
	}
}

func TestDeferredLongTriggerGetsFirstTurnInReopenedWindow(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	r := runner(triggerStore(t), c, "one", gh)
	in := input()
	in.IntervalMinutes = 5
	first := c.t
	create := func(name string, interval int) *trigger.Trigger {
		t.Helper()
		in.Name, in.IntervalMinutes = name, interval
		tr, err := r.Triggers.Create(t.Context(), in, "admin")
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	for n := range 30 {
		create(fmt.Sprintf("Short %d", n), 5)
	}
	c.t = first.Add(time.Second)
	long := create("Long", 15)
	short := create("Short due next window", 5)
	reopensAt := c.t.Add(time.Minute)
	if _, err := r.Triggers.Update(t.Context(), short.ID, "fixture", func(t *trigger.Trigger) error {
		t.NextRunAt = &reopensAt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	get := func(id string) *trigger.Trigger {
		t.Helper()
		tr, err := r.Triggers.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	r.Tick(t.Context())
	long = get(long.ID)
	if len(gh.args) != 30 || long.NextRunAt == nil || !long.NextRunAt.Equal(reopensAt) ||
		long.SearchDeferredReason == "" || long.SearchTurn != 0 {
		t.Fatalf("long trigger missed the next budget window: %+v, searches=%d", long, len(gh.args))
	}
	c.t = reopensAt.Add(-time.Nanosecond)
	r.Tick(t.Context())
	if len(gh.args) != 30 {
		t.Fatal("a search was dispatched before the budget reopened")
	}
	c.t = reopensAt
	r.Tick(t.Context())
	long, short = get(long.ID), get(short.ID)
	if len(gh.args) != 32 || long.SearchTurn != 31 || short.SearchTurn != 32 ||
		long.SearchDeferredReason != "" || !long.NextRunAt.Equal(reopensAt.Add(15*time.Minute)) {
		t.Fatalf("deferred trigger did not get first turn: long %+v, short %+v, searches=%d", long, short, len(gh.args))
	}
}

func TestGitHubIndexLagDefersQuestionsUntilQueryConfirmation(t *testing.T) {
	for _, changeAfterEvaluation := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed=%v", changeAfterEvaluation), func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			st := triggerStore(t)
			gh := newTracker("gh")
			gh.pageSearch = true
			row := ghIssue(1, "Issue", "weft")
			gh.issues = []map[string]any{row}
			calls := 0
			assessor := assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				calls++
				return trigger.ReadinessAssessment{Missing: []string{[]string{"objective", "acceptance", "scope"}[min(calls-1, 2)]}}
			})
			r := runner(st, c, "first", gh)
			r.Tasks, r.Assessor = &tasks{}, assessor
			in := input()
			in.Source.Query = "milestone:release"
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			gh.unindexed[1] = true
			if changeAfterEvaluation {
				gh.roles["maintainer"] = "maintain"
				gh.labelEvents["#1"] = []map[string]any{ghTodoEvent(3, "maintainer", "User", c.t.Add(time.Second))}
				row["labels"] = append(row["labels"].([]map[string]any), map[string]any{"name": trigger.LabelTodo})
			} else {
				gh.replies["#1"] = append(gh.replies["#1"], ghReply(7, "author", "User", "The parser should export CSV.", c.t.Add(time.Second)))
			}
			writes := func() int {
				return strings.Count(strings.Join(gh.rest, "\n"), "POST /repos/acme/app/issues/1/")
			}
			originalWrites := writes()
			for range 3 {
				tr = advance(r, c, t, tr.ID)
			}
			h := tr.Holds[0]
			if calls != 2 || writes() != originalWrites || h.Round != 1 || h.ScopeAssessment == nil ||
				!slices.Equal(h.ScopeAssessment.Missing, []string{"acceptance"}) || len(gh.comments["#1"]) != 1 {
				t.Fatalf("fallback did not hold back the second question: %+v, calls=%d, writes=%d", h, calls, writes())
			}
			r = runner(st, c, "restarted", gh)
			r.Tasks, r.Assessor = &tasks{}, assessor
			tr = advance(r, c, t, tr.ID)
			if calls != 2 || writes() != originalWrites || tr.Holds[0].ScopeAssessment == nil {
				t.Fatal("restart lost the deferred assessment or repeated an external operation")
			}
			wantCalls, wantText := 2, "What input, observable result"
			if changeAfterEvaluation {
				row["body"] = "The proposal now covers only the report endpoint."
				wantCalls, wantText = 3, "Which behavior or surfaces"
			}
			delete(gh.unindexed, 1)
			tr = advance(r, c, t, tr.ID)
			comments := gh.comments["#1"]
			if calls != wantCalls || tr.Holds[0].Round != 2 || tr.Holds[0].ScopeAssessment != nil ||
				len(comments) != 2 || !strings.Contains(comments[1], wantText) {
				t.Fatalf("confirmed query did not publish the current question: %+v, comments=%v, calls=%d", tr.Holds, comments, calls)
			}
		})
	}
}

func TestJiraMissingHoldRequiresProjectAccess(t *testing.T) {
	for _, tc := range []struct{ issueStatus, projectStatus int }{
		{404, 0}, {404, 403}, {404, 404}, {410, 0}, {410, 403}, {410, 404},
	} {
		t.Run(fmt.Sprintf("issue=%d/project=%d", tc.issueStatus, tc.projectStatus), func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			atl := newTracker("jira")
			atl.issues = []map[string]any{jiraIssue("APP-1", "Issue", "weft")}
			r := runner(triggerStore(t), c, "one", atl)
			r.Tasks = &tasks{}
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
			})
			const cloudID = "11111111-2222-3333-4444-555555555555"
			in := input()
			in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "jira", CloudID: cloudID}
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			if len(tr.Holds) != 1 || tr.Holds[0].Round != 1 {
				t.Fatalf("fixture did not hold the issue: %+v", tr)
			}
			project := "GET /ex/jira/" + cloudID + "/rest/api/3/project/APP"
			atl.status[project] = tc.projectStatus
			atl.status["GET /ex/jira/"+cloudID+"/rest/api/3/issue/APP-1"] = tc.issueStatus
			atl.issues = nil
			tr = advance(r, c, t, tr.ID)
			if len(tr.Holds) != 1 {
				t.Fatal("first missing issue response released the hold")
			}
			tr = advance(r, c, t, tr.ID)
			probes := strings.Count(strings.Join(atl.rest, "\n"), project)
			if probes != 2 {
				t.Fatalf("expected same-connection project checks: %v", atl.rest)
			}
			if tc.projectStatus == 0 {
				if len(tr.Holds) != 0 || len(tr.HoldResolutions) != 1 || tr.RetainedReadiness["jira:APP-1@factory"].Round != 1 {
					t.Fatalf("verified absence did not preserve progress: %+v", tr)
				}
			} else if len(tr.Holds) != 1 || len(tr.HoldResolutions) != 0 ||
				!strings.Contains(tr.LastError, "could not access the held issue's Jira project") ||
				!strings.Contains(tr.LastError, "Browse Projects") || !strings.Contains(tr.LastError, "read:project:jira") {
				t.Fatalf("inaccessible project did not preserve the hold and report how to restore access: %+v", tr)
			}
		})
	}
}

type repoRateLimit struct{ reset time.Time }

func (e repoRateLimit) Error() string { return "fixture repository rate limit" }

func (e repoRateLimit) TrackerRateLimit() (bool, time.Duration, time.Time) {
	return true, 0, e.reset
}

func TestTemporaryRepositoryProbeFailureDefersOnlyAffectedHold(t *testing.T) {
	for _, rateLimited := range []bool{false, true} {
		t.Run(fmt.Sprintf("rate_limited=%t", rateLimited), func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			gh := newTracker("gh")
			gh.pageSearch = true
			gh.issues = []map[string]any{ghIssue(1, "Missing", "weft"), ghIssue(2, "Waiting", "weft")}
			r := runner(triggerStore(t), c, "one", gh)
			r.Tasks = &tasks{}
			r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
				return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
			})
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				tr = advance(r, c, t, tr.ID)
			}
			if len(tr.Holds) != 2 {
				t.Fatalf("fixture did not hold both issues: %+v", tr)
			}
			const issueRead = "GET /repos/acme/app/issues/1"
			const probe = "GET /repos/acme/app"
			gh.status[issueRead] = 404
			gh.responseErrors[probe] = errors.New("fixture transport interrupted")
			pollAt := c.t.Add(time.Hour)
			want := pollAt.Add(time.Minute)
			if rateLimited {
				want = pollAt.Add(45 * time.Minute)
				gh.status[probe], gh.responseErrors[probe] = 403, repoRateLimit{reset: want}
			}
			gh.replies["#2"] = append(gh.replies["#2"], ghReply(7, "author", "User", "The report uses CSV as input.", c.t.Add(time.Second)))
			gh.issues = append(gh.issues, ghIssue(3, "New", "weft"))
			tr = advance(r, c, t, tr.ID)
			if len(tr.Holds) != 3 || len(tr.HoldResolutions) != 0 || tr.LastError != "" ||
				tr.Holds[0].RetryAccessAt == nil || !tr.Holds[0].RetryAccessAt.Equal(want) ||
				tr.Holds[1].Round != 2 || tr.Holds[2].Round != 1 {
				t.Fatalf("temporary repository probe interrupted other work or lost backoff: %+v", tr)
			}
			c.t = want.Add(-time.Second)
			if _, err := r.Triggers.Update(t.Context(), tr.ID, "fixture", func(t *trigger.Trigger) error {
				t.NextRunAt = nil
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			r.Tick(t.Context())
			probes := func() int {
				return slices.Index(gh.rest, probe)
			}
			firstProbe := probes()
			if firstProbe < 0 || slices.Contains(gh.rest[firstProbe+1:], probe) || slices.Contains(gh.rest[firstProbe+1:], issueRead) {
				t.Fatalf("affected hold was checked before its deadline: %v", gh.rest)
			}
			delete(gh.status, probe)
			delete(gh.responseErrors, probe)
			tr = advance(r, c, t, tr.ID)
			if !slices.Contains(gh.rest[firstProbe+1:], probe) || len(tr.Holds) != 3 || tr.Holds[0].RetryAccessAt != nil ||
				!strings.Contains(tr.Holds[0].Eligibility, "awaiting confirmation") {
				t.Fatalf("recovery failed or counted the failed probe: %+v, calls %v", tr, gh.rest)
			}
		})
	}
}

func TestMissingIssueConfirmationSpacingSurvivesRestartAndResetsOnSuccess(t *testing.T) {
	for _, successfulRead := range []bool{false, true} {
		t.Run(fmt.Sprintf("successful_read=%t", successfulRead), func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			st := triggerStore(t)
			gh := newTracker("gh")
			gh.pageSearch = true
			gh.issues = []map[string]any{ghIssue(1, "Waiting", "weft")}
			configure := func(instance string) *trigger.Runner {
				r := runner(st, c, instance, gh)
				r.Tasks = &tasks{}
				r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
					return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
				})
				return r
			}
			r := configure("first")
			tr, err := r.Triggers.Create(t.Context(), input(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			// Move a scheduled poll forward as a search-budget deferral can.
			poll := func(at time.Time) *trigger.Trigger {
				t.Helper()
				c.t = at
				if _, err := r.Triggers.Update(t.Context(), tr.ID, "fixture", func(t *trigger.Trigger) error {
					t.NextRunAt = &at
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				r.Tick(t.Context())
				got, err := r.Triggers.Get(t.Context(), tr.ID)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			const read = "GET /repos/acme/app/issues/1"
			first := c.t.Add(time.Hour)
			gh.status[read] = 404
			tr = poll(first)
			if len(tr.Holds) != 1 || !tr.Holds[0].MissingSince.Equal(first) {
				t.Fatalf("first observation was not persisted: %+v", tr.Holds)
			}
			r = configure("restarted")
			tr = poll(first.Add(time.Minute))
			if len(tr.Holds) != 1 || !tr.Holds[0].MissingSince.Equal(first) {
				t.Fatalf("early poll released the hold or lost its durable observation: %+v", tr.Holds)
			}
			interval := time.Duration(tr.IntervalMinutes) * time.Minute
			if successfulRead {
				delete(gh.status, read)
				tr = poll(first.Add(2 * time.Minute))
				if len(tr.Holds) != 1 || tr.Holds[0].MissingPoll != 0 || !tr.Holds[0].MissingSince.IsZero() {
					t.Fatalf("successful issue read did not clear the observation: %+v", tr.Holds)
				}
				gh.status[read] = 404
				first = first.Add(interval)
				tr = poll(first)
				if len(tr.Holds) != 1 || !tr.Holds[0].MissingSince.Equal(first) {
					t.Fatalf("404 counted across a successful issue read: %+v", tr.Holds)
				}
			}
			tr = poll(first.Add(interval))
			if len(tr.Holds) != 0 || len(tr.HoldResolutions) != 1 {
				t.Fatalf("consecutive verified 404s at the minimum spacing did not release: %+v", tr)
			}
		})
	}
}

func TestPersistentIssueAccessFailuresStayVisibleUntilReviewed(t *testing.T) {
	for _, tc := range []struct {
		kind     string
		status   int
		guidance string
	}{
		{"jira", 401, "read:project:jira"},
		{"github", 503, "repository access"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c := &clock{t: time.Now().UTC()}
			st := triggerStore(t)
			gh := newTracker("gh")
			gh.pageSearch = true
			gh.issues = []map[string]any{ghIssue(1, "Waiting", "weft")}
			in := input()
			probe, issue := "GET /repos/acme/app", "GET /repos/acme/app/issues/1"
			if tc.kind == "jira" {
				const cloudID = "11111111-2222-3333-4444-555555555555"
				in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "gh", CloudID: cloudID}
				gh.issues = []map[string]any{jiraIssue("APP-1", "Waiting", "weft")}
				probe, issue = "GET /ex/jira/"+cloudID+"/rest/api/3/project/APP", "GET /ex/jira/"+cloudID+"/rest/api/3/issue/APP-1"
			}
			configure := func(instance string) *trigger.Runner {
				r := runner(st, c, instance, gh)
				r.Tasks = &tasks{}
				r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
					return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
				})
				return r
			}
			r := configure("first")
			tr, err := r.Triggers.Create(t.Context(), in, "admin")
			if err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			gh.status[issue], gh.status[probe] = 404, tc.status
			for n := 1; n <= 5; n++ {
				tr = advance(r, c, t, tr.ID)
				if len(tr.Holds) != 1 || len(tr.HoldResolutions) != 0 || tr.Holds[0].AccessFailures != n ||
					(n < 5 && tr.LastError != "") {
					t.Fatalf("failure count, hold or diagnostic changed early: %+v", tr)
				}
				if n == 3 {
					r = configure("restarted")
				}
			}
			if !strings.Contains(tr.LastError, fmt.Sprintf("HTTP %d", tc.status)) ||
				!strings.Contains(tr.LastError, tc.guidance) || tr.Holds[0].RetryAccessAt != nil || tr.Holds[0].Round != 1 {
				t.Fatalf("terminal access diagnostic or preserved hold: %+v", tr)
			}
			searches := len(gh.calls)
			tr = advance(r, c, t, tr.ID)
			probes := 0
			for _, call := range gh.rest {
				if call == probe {
					probes++
				}
			}
			if len(gh.calls) <= searches || probes != 5 ||
				!strings.Contains(tr.LastError, tc.guidance) {
				t.Fatalf("terminal failure retried, stopped discovery or disappeared: %+v", tr)
			}
			delete(gh.status, probe)
			if _, err := r.Triggers.Resume(t.Context(), tr.ID, "admin"); err != nil {
				t.Fatal(err)
			}
			tr = advance(r, c, t, tr.ID)
			if len(tr.Holds) != 1 || tr.Holds[0].AccessFailures != 0 || tr.Holds[0].Round != 1 ||
				tr.LastError != "" || !strings.Contains(tr.Holds[0].Eligibility, "awaiting confirmation") {
				t.Fatalf("reviewed resume did not recover the original hold: %+v", tr)
			}
		})
	}
}

func TestRepeatedRateLimitedRepositoryProbesKeepTheHoldRetryable(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.pageSearch = true
	gh.issues = []map[string]any{ghIssue(1, "Waiting", "weft")}
	r := runner(triggerStore(t), c, "one", gh)
	r.Tasks = &tasks{}
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	const issueRead = "GET /repos/acme/app/issues/1"
	const probe = "GET /repos/acme/app"
	gh.status[issueRead], gh.status[probe] = 404, 403
	for n := 1; n <= 5; n++ {
		reset := c.t.Add(time.Hour + 45*time.Minute)
		gh.responseErrors[probe] = repoRateLimit{reset: reset}
		tr = advance(r, c, t, tr.ID)
		if len(tr.Holds) != 1 || tr.LastError != "" || len(tr.HoldResolutions) != 0 {
			t.Fatalf("rate limit failed the trigger or released the hold: %+v", tr)
		}
		h := tr.Holds[0]
		if h.AccessFailures != 0 || h.AccessError != "" || h.RetryAccessAt == nil ||
			!h.RetryAccessAt.Equal(reset) || !strings.Contains(h.Eligibility, "rate-limited") {
			t.Fatalf("probe %d changed the retry budget or lost its deadline: %+v", n, h)
		}
	}
	delete(gh.responseErrors, probe)
	delete(gh.status, probe)
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || tr.Holds[0].RetryAccessAt != nil || !strings.Contains(tr.Holds[0].Eligibility, "awaiting confirmation") {
		t.Fatalf("a successful probe after five rate limits did not resume confirmation: %+v", tr)
	}
}

func TestTriggerStateReportsEveryHoldWithUnverifiedAccess(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.pageSearch = true
	gh.issues = []map[string]any{ghIssue(1, "First", "weft"), ghIssue(2, "Second", "weft")}
	r := runner(triggerStore(t), c, "one", gh)
	r.Tasks = &tasks{}
	r.Assessor = assessFunc(func(context.Context, *trigger.Trigger, string) trigger.ReadinessAssessment {
		return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
	})
	tr, err := r.Triggers.Create(t.Context(), input(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		tr = advance(r, c, t, tr.ID)
	}
	if len(tr.Holds) != 2 {
		t.Fatalf("fixture did not hold both issues: %+v", tr)
	}
	gh.status["GET /repos/acme/app/issues/1"] = 404
	gh.status["GET /repos/acme/app/issues/2"] = 404
	gh.status["GET /repos/acme/app"] = 503
	for range 5 {
		tr = advance(r, c, t, tr.ID)
	}
	if len(tr.Holds) != 2 {
		t.Fatalf("access failures released an issue: %+v", tr)
	}
	for _, hold := range tr.Holds {
		if !strings.Contains(hold.AccessError, "HTTP 503") || !strings.Contains(tr.LastError, hold.Key+": "+hold.AccessError) {
			t.Fatalf("the error for %s was missing from trigger state: %+v", hold.Key, tr)
		}
	}
}
