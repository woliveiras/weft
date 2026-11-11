package trigger_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
)

// tasks is a fake task starter.
type tasks struct {
	started []trigger.Issue
	fail    error
	status  map[string]trigger.TaskStatus
}

func (f *tasks) Start(_ context.Context, _ *trigger.Trigger, i trigger.Issue) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.started = append(f.started, i)
	return "tsk_" + strings.ReplaceAll(strings.ToLower(i.Key), "#", ""), nil
}
func (f *tasks) Status(_ context.Context, id string) (trigger.TaskStatus, error) {
	if s, ok := f.status[id]; ok {
		return s, nil
	}
	return trigger.TaskStatus{State: "running"}, nil
}

func TestGitHubClaimStartsOneTaskAndWritesBack(t *testing.T) {
	st := triggerStore(t)
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "First", "weft"), ghIssue(2, "Second", "weft")}
	c := &clock{t: time.Now().UTC()}
	r := runner(st, c, "a", gh)
	fake := &tasks{}
	r.Tasks, r.BaseURL = fake, "https://weft.example/"
	tr, _ := r.Triggers.Create(t.Context(), input(), "alice")
	r.Tick(t.Context())
	if len(fake.started) != 1 || fake.started[0].Key != "acme/app#1" {
		t.Fatalf("started = %+v", fake.started)
	}
	issue := gh.find("#1")
	labels := issue["labels"].([]map[string]any)
	if len(labels) != 1 || labels[0]["name"] != trigger.LabelInProgress || issue["assignees"].([]map[string]any)[0]["login"] != "weft-bot" {
		t.Fatalf("claimed issue = %+v", issue)
	}
	if got := gh.comments["#1"]; len(got) != 1 || got[0] != "Weft started working on this issue: https://weft.example/#/tasks/tsk_acme/app1" {
		t.Fatalf("comment = %v", got)
	}
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	if got.CurrentTask != "tsk_acme/app1" || got.CurrentIssue.Key != "acme/app#1" || got.Processed["github:acme/app#1@factory"] != got.CurrentTask {
		t.Fatalf("trigger = %+v", got)
	}
	// One issue at a time: the next run waits for the task.
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if len(fake.started) != 1 {
		t.Fatalf("a second task started while one runs: %+v", fake.started)
	}
	// Every tracker call went through the gateway as the trigger.
	evs, _ := st.ListEvents(t.Context(), storage.EventFilter{GraphID: "trigger-" + tr.ID, Types: []domain.EventType{domain.EvToolCompleted}})
	if len(evs) < 3 {
		t.Fatalf("audited tracker calls = %d", len(evs))
	}
}

func TestJiraClaimTransitionsAndNotesAMissingTransition(t *testing.T) {
	st := triggerStore(t)
	atl := newTracker("atl")
	atl.issues = []map[string]any{jiraIssue("APP-1", "Export", "weft")}
	reg := tools.NewRegistry()
	reg.MustRegister(atl.tools("searchJiraIssuesUsingJql", "editJiraIssue", "addCommentToJiraIssue", "getTransitionsForJiraIssue", "transitionJiraIssue")...)
	bus := events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c := &clock{t: time.Now().UTC()}
	fake := &tasks{}
	r := &trigger.Runner{Triggers: trigger.Manager{Store: st, Bus: bus, Now: c.now}, Store: st, Instance: "a", Gateway: tools.NewGateway(reg, nil, bus), Tasks: fake}
	in := input()
	in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "atl", CloudID: cloud}
	in.Owner = "acc-1"
	tr, _ := r.Triggers.Create(t.Context(), in, "alice")
	// No In Progress transition offered: noted, not a failure.
	r.Tick(t.Context())
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	if len(fake.started) != 1 || got.LastError != "" || len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "no In Progress transition") {
		t.Fatalf("missing transition: %+v", got)
	}
	notes, _ := st.ListEvents(t.Context(), storage.EventFilter{GraphID: "task-run-app-1", Types: []domain.EventType{domain.EvTriggerNote}})
	if len(notes) != 1 {
		t.Fatalf("task note = %+v", notes)
	}
	// With the transition, the issue moves.
	atl.issues = []map[string]any{jiraIssue("APP-2", "Import", "weft")}
	atl.transitions = []map[string]any{{"id": "31", "name": "In Progress"}}
	_, _ = r.Triggers.Update(t.Context(), tr.ID, "test", func(t *trigger.Trigger) error { t.CurrentTask = ""; return nil })
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	status := atl.find("APP-2")["fields"].(map[string]any)["status"].(map[string]any)["name"]
	if status != "In Progress" {
		t.Fatalf("status = %v", status)
	}
}

func TestStartFailureMarksTheIssueAndPausesTheTrigger(t *testing.T) {
	st := triggerStore(t)
	gh := newTracker("gh")
	gh.issues = []map[string]any{ghIssue(1, "First", "weft")}
	r := runner(st, &clock{t: time.Now().UTC()}, "a", gh)
	r.Tasks = &tasks{fail: errors.New("the workflow changed")}
	tr, _ := r.Triggers.Create(t.Context(), input(), "alice")
	r.Tick(t.Context())
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	if got.State != trigger.Paused || !strings.Contains(got.PausedReason, "could not start") || got.Processed["github:acme/app#1@factory"] != "failed" {
		t.Fatalf("trigger = %+v", got)
	}
	labels := gh.find("#1")["labels"].([]map[string]any)
	if labels[len(labels)-1]["name"] != trigger.LabelFailed || !strings.Contains(gh.comments["#1"][0], "could not start a task") {
		t.Fatalf("issue = %+v %v", labels, gh.comments)
	}
}
