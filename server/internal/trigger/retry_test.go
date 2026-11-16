package trigger_test

import (
	"errors"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestFailedAdmissionRequiresExplicitRetryAndFreshMaintainerRequeue(t *testing.T) {
	st := triggerStore(t)
	c := &clock{t: time.Now().UTC()}
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "First", "weft")}
	r := runner(st, c, "retry", gh)
	starter := &tasks{fail: errors.New("fixture failure")}
	r.Tasks = starter
	tr, err := r.Triggers.Create(t.Context(), input(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	d, err := (taskconfig.Manager{Store: st}).SaveTriggered(t.Context(), taskconfig.Input{Title: "Issue", Request: "Fix it", Outcome: "implement", GraphID: "factory", GraphVersion: 1, NodeID: "planner"}, "alice", "acme/app#1")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	admissions, err := st.ListSettings(t.Context(), "trigger-admissions/")
	if err != nil || len(admissions) != 1 {
		t.Fatal(admissions, err)
	}
	original := admissions[0]
	if _, err := r.Triggers.Resume(t.Context(), tr.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	gh.find("#1")["labels"] = []map[string]any{{"name": "weft"}, {"name": trigger.LabelTodo}}
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 0 || tr.CurrentTask != "" {
		t.Fatal("resume/relabel bypassed deduplication")
	}
	if _, err := r.Triggers.Pause(t.Context(), tr.ID, "reviewed", "alice"); err != nil {
		t.Fatal(err)
	}
	tr, err = r.Triggers.RetryFailed(t.Context(), tr.ID, "acme/app#1", d.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Triggers.RetryFailed(t.Context(), tr.ID, "acme/app#1", d.ID, "alice"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("duplicate retry", err)
	}
	d, err = (taskconfig.Manager{Store: st}).Get(t.Context(), d.ID)
	if err != nil || d.Status != "cancelled" {
		t.Fatal("original draft can still start", d, err)
	}
	if _, err := r.Triggers.Resume(t.Context(), tr.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	starter.fail = nil
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 0 {
		t.Fatal("old label accepted")
	}
	gh.roles["contributor"] = "write"
	gh.labelEvents["#1"] = []map[string]any{ghTodoEvent(1, "contributor", "User", c.t.Add(time.Second))}
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 0 {
		t.Fatal("unprivileged requeue accepted")
	}
	gh.roles["maintainer"] = "maintain"
	gh.labelEvents["#1"] = append(gh.labelEvents["#1"], ghTodoEvent(2, "maintainer", "User", c.t.Add(time.Second)))
	tr = advance(r, c, t, tr.ID)
	if len(starter.started) != 1 || tr.CurrentTask == "" || !tr.FailedRetries["github:acme/app#1@factory"].Consumed {
		t.Fatalf("retry did not start once: %+v %+v", tr, starter)
	}
	retained, err := st.GetSetting(t.Context(), original.Key)
	if err != nil || string(retained.Value) != string(original.Value) || retained.Revision != original.Revision {
		t.Fatal("original admission changed", retained, err)
	}
	all, err := st.ListSettings(t.Context(), "trigger-admissions/")
	if err != nil || len(all) != 2 {
		t.Fatal(all, err)
	}
	events, err := st.ListEvents(t.Context(), storage.EventFilter{Types: []domain.EventType{"trigger.failed_retry_requested"}})
	if err != nil || len(events) != 1 || events[0].Actor != "alice" {
		t.Fatal("missing retry audit", events, err)
	}
}
