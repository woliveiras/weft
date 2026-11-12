package trigger_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestHeldIssueReturnsBeyondFirstQueryPage(t *testing.T) {
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
		gh.issues = append(gh.issues, ghIssue(n, "Older", trigger.LabelFailed))
	}
	gh.issues = append(gh.issues, target)
	gh.replies["#60"] = []map[string]any{ghReply(1, "author", "User", "expected output", c.t.Add(time.Second))}
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 1 || starter.started[0].Key != "acme/app#60" || len(tr.Holds) != 0 ||
		!slices.ContainsFunc(gh.args, func(a map[string]any) bool { return a["page"] == float64(2) }) {
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
			if len(gh.comments["#1"]) != 0 || tr.Holds[0].Round != 1 {
				t.Fatalf("first invalidation: %+v", tr.Holds)
			}
			first := tr.Holds[0]
			// Ensure the second assignment also changes the evaluated body.
			if duringClaim {
				row["body"] = "Edited before the second assessment."
			}
			for range 4 {
				tr = advance(r, c, t, tr.ID)
			}
			if calls != 2 || len(starter.started) != 0 || len(gh.comments["#1"]) != 1 ||
				tr.Holds[0].Reason != "snapshot_round_limit" || len(row["assignees"].([]map[string]any)) != 0 {
				t.Fatalf("stale snapshots were not bounded: %d calls, holds %+v, comments %+v", calls, tr.Holds, gh.comments)
			}
			if tr.Holds[0].Generation == first.Generation || !tr.Holds[0].OpenedAt.Equal(first.OpenedAt) {
				t.Fatalf("the second round did not rotate the same hold: %+v -> %+v", first, tr.Holds[0])
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
