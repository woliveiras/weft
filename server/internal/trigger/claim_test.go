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

func TestJiraClaimAndCompletionUseLabelsWithoutTransitions(t *testing.T) {
	st := triggerStore(t)
	atl := newTracker("atl")
	atl.issues = []map[string]any{jiraIssue("APP-1", "Export", "weft", "bug")}
	atl.transitions = []map[string]any{{"id": "31", "name": "In Progress"}}
	reg := tools.NewRegistry()
	reg.MustRegister(atl.tools("searchJiraIssuesUsingJql", "getTransitionsForJiraIssue", "transitionJiraIssue")...)
	reg.MustRegister(trigger.TrackerTools(atl.request)...)
	bus := events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c := &clock{t: time.Now().UTC()}
	fake := &tasks{status: map[string]trigger.TaskStatus{}}
	r := &trigger.Runner{Triggers: trigger.Manager{Store: st, Bus: bus, Now: c.now}, Store: st, Instance: "a", Gateway: tools.NewGateway(reg, nil, bus), Tasks: fake, Assessor: assessFunc(ready)}
	in := input()
	in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "atl", CloudID: cloud}
	in.Owner = "acc-1"
	tr, err := r.Triggers.Create(t.Context(), in, "alice")
	if err != nil {
		t.Fatal(err)
	}
	r.Tick(t.Context())
	got, _ := r.Triggers.Get(t.Context(), tr.ID)
	fields := atl.find("APP-1")["fields"].(map[string]any)
	if len(fake.started) != 1 || got.LastError != "" || len(atl.calls) != 1 ||
		fields["status"].(map[string]any)["name"] != "To Do" ||
		strings.Join(fields["labels"].([]string), ",") != "bug,weft:wip" ||
		fields["assignee"].(map[string]string)["accountId"] != "acc-1" {
		t.Fatalf("claim = %+v, fields = %+v, tracker calls = %+v", got, fields, atl.calls)
	}
	fake.status[got.CurrentTask] = trigger.TaskStatus{State: "succeeded", PullRequestURL: "https://example.test/pull/9"}
	c.t = c.t.Add(time.Hour)
	r.Tick(t.Context())
	if fields["status"].(map[string]any)["name"] != "To Do" ||
		strings.Join(fields["labels"].([]string), ",") != "bug,weft:done" || len(atl.comments["APP-1"]) != 2 ||
		len(atl.calls) != 1 || strings.Contains(strings.Join(atl.rest, "\n"), "transition") {
		t.Fatalf("completion = %+v, comments = %+v, calls = %+v", fields, atl.comments, atl.rest)
	}
}

func TestJiraReadinessHoldsAndReturnsWithoutStatusTransition(t *testing.T) {
	st := triggerStore(t)
	atl := newTracker("atl")
	issue := jiraIssue("APP-1", "Export", trigger.LabelTodo, "bug")
	fields := issue["fields"].(map[string]any)
	actor := map[string]any{"accountId": "reporter", "accountType": "atlassian", "active": true}
	fields["reporter"] = actor
	atl.issues = []map[string]any{issue}
	reg := tools.NewRegistry()
	reg.MustRegister(atl.tools("searchJiraIssuesUsingJql")...)
	reg.MustRegister(trigger.TrackerTools(atl.request)...)
	c := &clock{t: time.Now().UTC()}
	starter := &tasks{}
	r := &trigger.Runner{Triggers: trigger.Manager{Store: st, Now: c.now}, Store: st, Instance: "jira", Gateway: tools.NewGateway(reg, nil, nil), Tasks: starter,
		Assessor: assessFunc(func(_ context.Context, _ *trigger.Trigger, input string) trigger.ReadinessAssessment {
			if strings.Contains(input, "expected CSV header") {
				return trigger.ReadinessAssessment{}
			}
			return trigger.ReadinessAssessment{Missing: []string{"acceptance"}}
		})}
	in := input()
	in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "atl", CloudID: cloud}
	in.Label, in.Owner = "", ""
	tr, err := r.Triggers.Create(t.Context(), in, "alice")
	if err != nil {
		t.Fatal(err)
	}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 1 || strings.Join(fields["labels"].([]string), ",") != "bug,weft:needs-attention" || len(starter.started) != 0 {
		t.Fatalf("Jira hold: %+v, fields %+v", tr.Holds, fields)
	}
	atl.replies["APP-1"] = []map[string]any{{"id": "88", "created": c.t.Add(time.Minute).Format(time.RFC3339Nano), "author": actor,
		"body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "text", "text": "expected CSV header"}}}}}
	tr = advance(r, c, t, tr.ID)
	if len(tr.Holds) != 0 || len(starter.started) != 1 || !strings.Contains(trigger.Request(starter.started[0]), "expected CSV header") ||
		strings.Join(fields["labels"].([]string), ",") != "bug,weft:wip" ||
		fields["status"].(map[string]any)["name"] != "To Do" || strings.Contains(strings.Join(atl.rest, "\n"), "transition") {
		t.Fatalf("Jira reply: %+v, fields %+v, calls %+v", tr, fields, atl.rest)
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
