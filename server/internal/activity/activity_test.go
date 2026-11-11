package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

var now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *fsstore.Store {
	t.Helper()
	s, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func saveGraph(t *testing.T, s storage.Store, id, name string, labels map[string]string) {
	t.Helper()
	_, err := s.SaveGraph(context.Background(), &domain.Graph{APIVersion: "weft.io/v1", Kind: "Graph", Metadata: domain.GraphMetadata{ID: id, Name: name, Labels: labels}}, 0)
	must(t, err)
}

func exec(t *testing.T, s storage.Store, id, graph, node string, st domain.ExecutionStatus, started time.Time, done *time.Time) {
	t.Helper()
	must(t, s.PutExecution(context.Background(), &domain.Execution{ID: id, GraphID: graph, NodeID: node, Status: st, StartedAt: started, CompletedAt: done,
		ToolCalls: 2, Usage: domain.Usage{InputTokens: 10, OutputTokens: 5}}))
}

func event(t *testing.T, s storage.Store, typ domain.EventType, graph, execution string, at time.Time, data any) {
	t.Helper()
	ev := domain.NewEvent(typ, graph, data).WithExecution(execution)
	ev.Time = at
	must(t, s.AppendEvent(context.Background(), ev))
}

func ptr(t time.Time) *time.Time { return &t }

func errorCodes(v *View) map[string]string {
	out := map[string]string{}
	for _, e := range v.Errors {
		out[e.Section] = e.Code
	}
	return out
}

func TestParseQuery(t *testing.T) {
	for in, want := range map[string]time.Duration{"": DefaultWindow, "24h": 24 * time.Hour, "90m": 90 * time.Minute, "7d": MaxWindow, "5m": MinWindow} {
		got, err := ParseWindow(in)
		if err != nil || got != want {
			t.Errorf("ParseWindow(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"4m", "8d", "abc", "-1h", "xd", "0"} {
		if _, err := ParseWindow(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseWindow(%q) accepted: %v", in, err)
		}
	}
	q, err := ParseQuery(url.Values{"window": {"6h"}, "limit": {"20"}, "status": {"failed"}, "workflow": {" wf "}, "agent": {"coder"}})
	if err != nil || q.Window != 6*time.Hour || q.HistoryLimit != 20 || q.Status != "failed" || q.Workflow != "wf" || q.Agent != "coder" {
		t.Fatalf("ParseQuery = %+v, %v", q, err)
	}
	for _, v := range []url.Values{{"limit": {"0"}}, {"limit": {"201"}}, {"limit": {"x"}}, {"status": {"running"}}, {"agent": {strings.Repeat("a", 201)}}} {
		if _, err := ParseQuery(v); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseQuery(%v) accepted: %v", v, err)
		}
	}
	if q, _ := ParseQuery(url.Values{}); q.HistoryLimit != DefaultHistory || q.Window != DefaultWindow {
		t.Fatalf("defaults: %+v", q)
	}
}

func TestCurrentStep(t *testing.T) {
	cases := map[string]string{
		`{"items":[{"step":"read","status":"done"},{"step":"edit files","status":"in_progress"},{"step":"test","status":"pending"}]}`: "edit files",
		`{"items":[{"step":"read","status":"done"}]}`: "",
		`not json`: "",
		`{"items":[{"step":"` + strings.Repeat("é", 400) + `","status":"in_progress"}]}`: strings.Repeat("é", maxErrorRunes) + "…",
	}
	for in, want := range cases {
		if got := CurrentStep(json.RawMessage(in)); got != want {
			t.Errorf("CurrentStep(%.40q) = %.40q, want %.40q", in, got, want)
		}
	}
}

func TestPhase(t *testing.T) {
	cases := []struct {
		st     domain.ReplicaState
		detail string
		want   string
	}{
		{domain.ReplicaRunning, "waiting for sandbox capacity to resume", "waiting_for_capacity"},
		{domain.ReplicaAwaitingApproval, "", "awaiting_approval"},
		{domain.ReplicaRunning, "waiting for durable human review", "awaiting_review"},
		{domain.ReplicaIdle, "waiting for an operator message", "waiting_for_message"},
		{domain.ReplicaRunning, "", "running"},
		{domain.ReplicaPaused, "", "paused"},
	}
	for _, c := range cases {
		if got := Phase(c.st, c.detail); got != c.want {
			t.Errorf("Phase(%s, %q) = %s, want %s", c.st, c.detail, got, c.want)
		}
	}
}

func TestHistoryWindowCountersFiltersAndCost(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	saveGraph(t, s, "wf", "Software factory", nil)
	saveGraph(t, s, "task-run-1", "Fix login", map[string]string{domain.LabelTask: "task-1", domain.LabelTaskWorkflow: "wf"})
	exec(t, s, "x-old", "wf", "coder", domain.ExecFailed, now.Add(-25*time.Hour), ptr(now.Add(-25*time.Hour+time.Minute)))
	exec(t, s, "x-ok", "wf", "coder", domain.ExecSucceeded, now.Add(-3*time.Hour), ptr(now.Add(-3*time.Hour+90*time.Second)))
	exec(t, s, "x-fail", "task-run-1", "tester", domain.ExecFailed, now.Add(-2*time.Hour), ptr(now.Add(-2*time.Hour+time.Second)))
	exec(t, s, "x-int", "wf", "reviewer", domain.ExecInterrupted, now.Add(-90*time.Minute), ptr(now.Add(-80*time.Minute)))
	exec(t, s, "x-can", "wf", "coder", domain.ExecCancelled, now.Add(-time.Hour), ptr(now.Add(-59*time.Minute)))
	exec(t, s, "x-run", "task-run-1", "coder", domain.ExecRunning, now.Add(-10*time.Minute), nil)
	must(t, s.PutExecution(ctx, &domain.Execution{ID: "x-err", GraphID: "gone", NodeID: "coder", Status: domain.ExecFailed, StartedAt: now.Add(-5 * time.Minute),
		CompletedAt: ptr(now.Add(-4 * time.Minute)), Error: strings.Repeat("x", 500)}))
	event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-26*time.Hour), cost.Record{Execution: "x-ok", CostMicros: 999, Priced: true, Currency: "USD"})
	event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-3*time.Hour), cost.Record{Execution: "x-ok", CostMicros: 1500, Priced: true, Currency: "USD"})
	event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-2*time.Hour), cost.Record{Execution: "x-ok", CostMicros: 500, Priced: true, Currency: "USD"})
	event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-2*time.Hour), cost.Record{Execution: "x-ok", CostMicros: 7, Priced: true, Currency: "EUR"})

	b := Builder{Store: s, Now: func() time.Time { return now }}
	v, err := b.Build(ctx, Query{}, Viewer{Name: "op", CanReview: true})
	must(t, err)
	if *v.Counters.Failed != 3 || *v.Counters.Finished != 1 || *v.Counters.Cancelled != 1 {
		t.Fatalf("counters: failed %d finished %d cancelled %d", *v.Counters.Failed, *v.Counters.Finished, *v.Counters.Cancelled)
	}
	if len(v.History) != 6 || v.History[0].ID != "x-err" || v.History[5].ID != "x-ok" {
		t.Fatalf("history order/window: %+v", ids(v.History))
	}
	byID := map[string]HistoryRow{}
	for _, r := range v.History {
		byID[r.ID] = r
	}
	if r := byID["x-ok"]; r.DurationMs != 90_000 || *r.CostMicros != 2000 || r.GraphName != "Software factory" || r.Workflow != "wf" || r.ToolCalls != 2 || r.Usage.InputTokens != 10 {
		t.Fatalf("x-ok row: %+v cost %d", r, *r.CostMicros)
	}
	if r := byID["x-fail"]; r.Task != "task-1" || r.Workflow != "wf" || *r.CostMicros != 0 {
		t.Fatalf("task row: %+v", r)
	}
	if r := byID["x-run"]; r.DurationMs != (10*time.Minute).Milliseconds() || r.CompletedAt != nil {
		t.Fatalf("running row duration: %+v", r)
	}
	if r := byID["x-err"]; len([]rune(r.Error)) != maxErrorRunes+1 || r.Workflow != "gone" {
		t.Fatalf("error summary must be bounded and a deleted graph kept by id: %d %q", len(r.Error), r.Workflow)
	}
	if v.Currency != "USD" || v.Window != "24h" || !v.Since.Equal(now.Add(-24*time.Hour)) || len(v.Errors) != 1 || errorCodes(v)["reviews"] != "unavailable" {
		t.Fatalf("meta: %s %s %v %+v", v.Currency, v.Window, v.Since, v.Errors)
	}
	if got := fmt.Sprint(v.Options.Agents); got != "[coder reviewer tester]" {
		t.Fatalf("agent options: %s", got)
	}
	if len(v.Options.Workflows) != 2 || v.Options.Workflows[1] != (Option{ID: "wf", Name: "Software factory"}) {
		t.Fatalf("workflow options: %+v", v.Options.Workflows)
	}

	// Filters narrow history only; the counters keep describing the window.
	for _, c := range []struct {
		q    Query
		want string
	}{
		{Query{Status: "failed"}, "[x-err x-int x-fail]"},
		{Query{Status: "active"}, "[x-run]"},
		{Query{Workflow: "wf"}, "[x-run x-can x-int x-fail x-ok]"},
		{Query{Workflow: "task-run-1"}, "[x-run x-fail]"},
		{Query{Agent: "coder", Status: "succeeded"}, "[x-ok]"},
		{Query{Window: time.Hour}, "[x-err x-run x-can]"},
	} {
		v, err := b.Build(ctx, c.q, Viewer{})
		must(t, err)
		if got := fmt.Sprint(ids(v.History)); got != c.want {
			t.Errorf("%+v: history %s, want %s", c.q, got, c.want)
		}
		if c.q.Window == 0 && *v.Counters.Failed != 3 {
			t.Errorf("%+v changed the counters", c.q)
		}
	}
	v, err = b.Build(ctx, Query{HistoryLimit: 2}, Viewer{})
	must(t, err)
	if len(v.History) != 2 || !v.Truncated.History || v.Truncated.Executions {
		t.Fatalf("limit: %d rows, truncated %+v", len(v.History), v.Truncated)
	}
}

func ids(rows []HistoryRow) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestRunningGroupsPlanStepsAndPhases(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	saveGraph(t, s, "wf", "Software factory", nil)
	saveGraph(t, s, "idle", "Stopped graph", nil)
	must(t, s.PutGraphRecord(ctx, &domain.GraphRecord{ID: "wf", State: domain.GraphRunning, DesiredState: domain.GraphRunning, StartedAt: ptr(now.Add(-time.Hour))}))
	must(t, s.PutGraphRecord(ctx, &domain.GraphRecord{ID: "idle", State: domain.GraphStopped, DesiredState: domain.GraphStopped}))
	exec(t, s, "x1", "wf", "coder", domain.ExecRunning, now.Add(-2*time.Minute), nil)
	exec(t, s, "x-done", "wf", "tester", domain.ExecSucceeded, now.Add(-30*time.Minute), ptr(now.Add(-29*time.Minute)))
	for _, r := range []*domain.ReplicaStatus{
		{GraphID: "wf", NodeID: "coder", Replica: 0, State: domain.ReplicaRunning, Execution: "x1"},
		{GraphID: "wf", NodeID: "coder", Replica: 1, State: domain.ReplicaRunning, Detail: "waiting for sandbox capacity to resume"},
		{GraphID: "wf", NodeID: "tester", Replica: 0, State: domain.ReplicaIdle, Execution: "x-done", Detail: "waiting for an operator message"},
		{GraphID: "wf", NodeID: "old", Replica: 0, State: domain.ReplicaStopped},
		{GraphID: "idle", NodeID: "a", Replica: 0, State: domain.ReplicaRunning},
	} {
		must(t, s.PutReplicaStatus(ctx, r))
	}
	plan := func(items ...string) map[string]any {
		var out []map[string]string
		for i := 0; i < len(items); i += 2 {
			out = append(out, map[string]string{"step": items[i], "status": items[i+1]})
		}
		return map[string]any{"items": out}
	}
	event(t, s, domain.EvAgentPlan, "wf", "x1", now.Add(-90*time.Second), plan("read code", "in_progress", "edit", "pending"))
	event(t, s, domain.EvAgentPlan, "wf", "x1", now.Add(-60*time.Second), plan("read code", "done", "edit", "in_progress"))
	event(t, s, domain.EvAgentPlan, "wf", "x-other", now.Add(-30*time.Second), plan("unrelated", "in_progress"))
	event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-time.Minute), cost.Record{Execution: "x1", CostMicros: 42, Priced: true, Currency: "USD"})

	v, err := Builder{Store: s, Now: func() time.Time { return now }}.Build(ctx, Query{}, Viewer{})
	must(t, err)
	if len(v.Running) != 1 || v.Running[0].GraphID != "wf" || v.Running[0].GraphName != "Software factory" || v.Running[0].State != domain.GraphRunning {
		t.Fatalf("groups: %+v", v.Running)
	}
	reps := v.Running[0].Replicas
	if len(reps) != 3 || *v.Counters.Running != 2 {
		t.Fatalf("live replicas %d, running counter %d", len(reps), *v.Counters.Running)
	}
	c0, c1, idle := reps[0], reps[1], reps[2]
	if c0.ExecutionID != "x1" || c0.PlanStep != "edit" || c0.ElapsedMs != (2*time.Minute).Milliseconds() || *c0.CostMicros != 42 || c0.ToolCalls != 2 || c0.Phase != "running" {
		t.Fatalf("coder#0: %+v", c0)
	}
	if c1.Phase != "waiting_for_capacity" || c1.ExecutionID != "" || c1.CostMicros != nil {
		t.Fatalf("coder#1: %+v", c1)
	}
	if idle.Phase != "waiting_for_message" || idle.ExecutionID != "" || idle.ElapsedMs != 0 {
		t.Fatalf("a finished execution must not show as the idle replica's turn: %+v", idle)
	}
}

type fakeReviews struct {
	rs  []*review.Record
	err error
}

func (f fakeReviews) List(context.Context) ([]*review.Record, error) { return f.rs, f.err }

func TestNeedsYouOldestFirstAndRoleFiltered(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	saveGraph(t, s, "wf", "Software factory", nil)
	for _, a := range []*domain.Approval{
		{ID: "a-new", GraphID: "wf", NodeID: "coder", Kind: domain.ApprovalTool, Title: "write file", CreatedAt: now.Add(-time.Minute)},
		{ID: "a-q", GraphID: "wf", NodeID: "coder", Kind: domain.ApprovalQuestion, Title: "which branch?", CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "a-done", GraphID: "wf", Kind: domain.ApprovalTool, Status: domain.ApprovalApproved, CreatedAt: now.Add(-4 * time.Hour)},
		{ID: "a-old", GraphID: "wf", Kind: domain.ApprovalMessage, Title: "hand-off", CreatedAt: now.Add(-48 * time.Hour)},
	} {
		must(t, s.PutApproval(ctx, a))
	}
	reviews := fakeReviews{rs: []*review.Record{
		{ID: "r-open", GraphID: "wf", NodeID: "coder", State: "pending", Request: review.Request{Title: "check the preview"}, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour)},
		{ID: "r-other", GraphID: "wf", State: "pending", Policy: review.Policy{Reviewers: []string{"alice"}}, CreatedAt: now.Add(-5 * time.Hour)},
		{ID: "r-done", GraphID: "wf", State: "approved", CreatedAt: now.Add(-6 * time.Hour)},
	}}
	b := Builder{Store: s, Reviews: reviews, Now: func() time.Time { return now }}

	v, err := b.Build(ctx, Query{}, Viewer{Name: "bob", CanReview: true})
	must(t, err)
	got := []string{}
	for _, n := range v.NeedsYou {
		got = append(got, n.Kind+":"+n.ID)
	}
	if fmt.Sprint(got) != "[message:a-old question:a-q review:r-open tool:a-new]" || *v.Counters.Waiting != 4 || len(v.Errors) != 0 {
		t.Fatalf("operator needs-you: %v waiting %d errors %+v", got, *v.Counters.Waiting, v.Errors)
	}
	if v.NeedsYou[0].GraphName != "Software factory" || v.NeedsYou[2].ExpiresAt == nil {
		t.Fatalf("item detail: %+v", v.NeedsYou)
	}

	// The named reviewer also sees the review restricted to them.
	v, err = b.Build(ctx, Query{}, Viewer{Name: "alice", CanReview: true})
	must(t, err)
	if *v.Counters.Waiting != 5 || v.NeedsYou[1].ID != "r-other" {
		t.Fatalf("reviewer: waiting %d first %+v", *v.Counters.Waiting, v.NeedsYou[1])
	}

	// A viewer cannot see reviews at all; the omission is explicit.
	v, err = b.Build(ctx, Query{}, Viewer{Name: "viewer"})
	must(t, err)
	if *v.Counters.Waiting != 3 || errorCodes(v)["reviews"] != "forbidden" {
		t.Fatalf("viewer: waiting %d errors %+v", *v.Counters.Waiting, v.Errors)
	}

	// Not configured and failing review sources keep approvals visible.
	v, err = Builder{Store: s, Now: b.Now}.Build(ctx, Query{}, Viewer{Name: "bob", CanReview: true})
	must(t, err)
	if errorCodes(v)["reviews"] != "unavailable" || len(v.NeedsYou) != 3 {
		t.Fatalf("unconfigured: %+v", v.Errors)
	}
	v, err = Builder{Store: s, Reviews: fakeReviews{err: errors.New("disk")}, Now: b.Now}.Build(ctx, Query{}, Viewer{Name: "bob", CanReview: true})
	must(t, err)
	if errorCodes(v)["reviews"] != "failed" || len(v.NeedsYou) != 3 || strings.Contains(v.Errors[0].Message, "disk") {
		t.Fatalf("failing reviews: %+v", v.Errors)
	}
}

// failing makes chosen store reads fail or return synthetic pages.
type failing struct {
	storage.Store
	executions, approvals, records, events error
	page                                   []*domain.Execution
}

func (f failing) ListExecutions(ctx context.Context, q storage.ExecutionFilter) ([]*domain.Execution, error) {
	if f.executions != nil {
		return nil, f.executions
	}
	if f.page != nil {
		return f.page, nil
	}
	return f.Store.ListExecutions(ctx, q)
}

func (f failing) ListApprovals(ctx context.Context, q storage.ApprovalFilter) ([]*domain.Approval, error) {
	if f.approvals != nil {
		return nil, f.approvals
	}
	return f.Store.ListApprovals(ctx, q)
}

func (f failing) ListGraphRecords(ctx context.Context) ([]*domain.GraphRecord, error) {
	if f.records != nil {
		return nil, f.records
	}
	return f.Store.ListGraphRecords(ctx)
}

func (f failing) ListEvents(ctx context.Context, q storage.EventFilter) ([]*domain.Event, error) {
	if f.events != nil {
		return nil, f.events
	}
	return f.Store.ListEvents(ctx, q)
}

func TestFailedSectionsAreReportedNotEmpty(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	exec(t, s, "x1", "wf", "coder", domain.ExecSucceeded, now.Add(-time.Hour), ptr(now))
	boom := errors.New("database is locked")
	b := Builder{Store: failing{Store: s, executions: boom, approvals: boom, records: boom, events: boom}, Now: func() time.Time { return now }}
	v, err := b.Build(ctx, Query{}, Viewer{CanReview: true})
	must(t, err)
	codes := errorCodes(v)
	for _, section := range []string{"cost", "needsYou", "running", "history"} {
		if codes[section] != "failed" {
			t.Errorf("section %s: %q", section, codes[section])
		}
	}
	if v.History != nil || v.NeedsYou != nil || v.Running != nil || v.Options != nil {
		t.Fatal("a failed section must be null, not an empty list")
	}
	if v.Counters.Failed != nil || v.Counters.Finished != nil || v.Counters.Waiting != nil || v.Counters.Running != nil {
		t.Fatalf("failed counters must be null: %+v", v.Counters)
	}
	b2, _ := json.Marshal(v)
	if strings.Contains(string(b2), "database is locked") {
		t.Fatal("internal errors must not reach the response")
	}

	// Only cost failing: history stays, cost is unknown rather than zero.
	v, err = Builder{Store: failing{Store: s, events: boom}, Now: b.Now}.Build(ctx, Query{}, Viewer{CanReview: true})
	must(t, err)
	if len(v.History) != 1 || v.History[0].CostMicros != nil || errorCodes(v)["cost"] != "failed" {
		t.Fatalf("cost failure: %+v %+v", v.History, v.Errors)
	}

	if _, err := b.Build(canceled(), Query{}, Viewer{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
}

func canceled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestFullExecutionPageIsTruncated(t *testing.T) {
	s := newStore(t)
	page := make([]*domain.Execution, maxExecutions)
	for i := range page {
		page[i] = &domain.Execution{ID: fmt.Sprintf("x%04d", i), GraphID: "wf", NodeID: "a", Status: domain.ExecFailed, StartedAt: now.Add(-time.Duration(i) * time.Second)}
	}
	v, err := Builder{Store: failing{Store: s, page: page}, Now: func() time.Time { return now }}.Build(context.Background(), Query{}, Viewer{})
	must(t, err)
	if !v.Truncated.Executions || !v.Truncated.History || *v.Counters.Failed != maxExecutions || len(v.History) != DefaultHistory {
		t.Fatalf("truncated %+v failed %d rows %d", v.Truncated, *v.Counters.Failed, len(v.History))
	}
	// A full page that already reaches past the window start is complete.
	page[len(page)-1].StartedAt = now.Add(-48 * time.Hour)
	v, err = Builder{Store: failing{Store: s, page: page}, Now: func() time.Time { return now }}.Build(context.Background(), Query{}, Viewer{})
	must(t, err)
	if v.Truncated.Executions || *v.Counters.Failed != maxExecutions-1 {
		t.Fatalf("complete page: %+v %d", v.Truncated, *v.Counters.Failed)
	}
}

func TestCostReadStopsAtWindowAndCap(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for i := 0; i < maxCostEvents+10; i++ {
		event(t, s, domain.EvCostRecorded, "wf", "", now.Add(-time.Minute), cost.Record{Execution: "x", CostMicros: 1})
	}
	exec(t, s, "x", "wf", "a", domain.ExecRunning, now.Add(-time.Hour), nil)
	v, err := Builder{Store: s, Now: func() time.Time { return now }}.Build(ctx, Query{}, Viewer{})
	must(t, err)
	if !v.Truncated.Cost || *v.History[0].CostMicros != maxCostEvents {
		t.Fatalf("cap: %+v cost %d", v.Truncated, *v.History[0].CostMicros)
	}
}
