package trigger_test

import (
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestEligibilityRules(t *testing.T) {
	tr := &trigger.Trigger{Workflow: "factory", Label: "weft", Owner: "weft-bot", Processed: map[string]string{"github:acme/app#9@factory": "tsk_old"}}
	base := trigger.Issue{Source: "github", Key: "acme/app#1", Labels: []string{"Weft"}, Open: true}
	for _, tc := range []struct {
		change func(*trigger.Issue)
		want   string
	}{
		{func(*trigger.Issue) {}, ""},
		{func(i *trigger.Issue) { i.Assignee = "WEFT-BOT" }, ""},
		{func(i *trigger.Issue) { i.Labels = []string{"bug"} }, "does not have the label"},
		{func(i *trigger.Issue) { i.Open = false }, "is not open"},
		{func(i *trigger.Issue) { i.Assignee = "someone" }, "assigned to someone else"},
		{func(i *trigger.Issue) { i.Blocked = true }, "is blocked"},
		{func(i *trigger.Issue) { i.Key = "acme/app#9" }, "already ran with this workflow (task tsk_old)"},
	} {
		i := base
		tc.change(&i)
		if got := trigger.Reason(tr, i); (tc.want == "" && got != "") || !strings.Contains(got, tc.want) {
			t.Errorf("%+v: %q, want %q", i, got, tc.want)
		}
	}
	// The same issue with another workflow is a different key.
	other := *tr
	other.Workflow = "other"
	i := base
	i.Key = "acme/app#9"
	if trigger.Reason(&other, i) != "" {
		t.Fatal("dedup is per workflow")
	}
}

func TestMatchSortsIssuesAndWaitsForTheCurrentTask(t *testing.T) {
	st := triggerStore(t)
	gh := newTracker("gh")
	assigned := ghIssue(3, "Taken", "weft")
	assigned["assignees"] = []map[string]any{{"login": "someone"}}
	gh.issues = []map[string]any{ghIssue(1, "First", "weft"), assigned, ghIssue(4, "Second", "weft"), ghIssue(5, "Blocked", "weft", "blocked")}
	r := runner(st, &clock{t: time.Now()}, "a", gh)
	tr, _ := r.Triggers.Create(t.Context(), input(), "alice")
	m, _, _, err := r.Match(t.Context(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if m.Next == nil || m.Next.Key != "acme/app#1" || len(m.Waiting) != 1 || m.Waiting[0].Key != "acme/app#4" || len(m.Skipped) != 2 {
		t.Fatalf("matches = %+v", m)
	}
	if m.Skipped[0].Key != "acme/app#3" || m.Skipped[1].Reason != "is blocked" {
		t.Fatalf("skipped = %+v", m.Skipped)
	}
	// While a task runs, nothing is next.
	tr.CurrentTask = "tsk_running"
	m, _, _, _ = r.Match(t.Context(), tr)
	if m.Next != nil || m.Busy != "tsk_running" || len(m.Waiting) != 2 {
		t.Fatalf("busy = %+v", m)
	}
	// A failing search is an error, not an empty match.
	gh.fail["search_issues"] = true
	if _, _, _, err := r.Match(t.Context(), tr); err == nil {
		t.Fatal("search failure")
	}
	// The scheduled run records the skipped issues on the trigger.
	gh.fail["search_issues"] = false
	r.Tasks = &tasks{}
	r.Tick(t.Context())
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	if len(got.Skipped) != 2 {
		t.Fatalf("recorded skips = %+v", got.Skipped)
	}
}
