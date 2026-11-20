package app

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

// Exercise the app bridge through the real task view, engine status and durable
// store. Execution allocation and approval records are not proof of runtime work.
func TestTriggerTaskLifecycleWorkRequiresDurableRuntimeStart(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   domain.ExecutionStatus
		approval domain.ApprovalStatus
		started  bool
	}{
		{name: "before-turn approval pending", status: domain.ExecRunning, approval: domain.ApprovalPending},
		{name: "before-turn approval rejected", status: domain.ExecCancelled, approval: domain.ApprovalRejected},
		{name: "legacy terminal execution without marker", status: domain.ExecSucceeded},
		{name: "runtime completed before observation", status: domain.ExecSucceeded, approval: domain.ApprovalApproved, started: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, draft, bridge := triggerLifecycleFixture(t, "")
			now := time.Now().UTC()
			execution := &domain.Execution{ID: "exec-lifecycle", GraphID: draft.ExecutionGraphID,
				GraphVer: 1, RunID: "fixture-run", NodeID: "worker", Status: tc.status, StartedAt: now}
			if tc.status.Terminal() {
				execution.CompletedAt = &now
			}
			if tc.started {
				// A fast runtime can start and complete between trigger polls.
				execution.RuntimeStartedAt = &now
			}
			if err := store.PutExecution(t.Context(), execution); err != nil {
				t.Fatal(err)
			}
			if tc.approval != "" {
				if err := store.PutApproval(t.Context(), &domain.Approval{ID: "approval-lifecycle",
					GraphID: draft.ExecutionGraphID, NodeID: "worker", ExecutionID: execution.ID,
					Kind: domain.ApprovalTurn, Status: tc.approval, CreatedAt: now}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := bridge.Status(t.Context(), draft.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantID := ""
			if tc.started {
				wantID = execution.ID
			}
			if got.WorkExecutionID != wantID {
				t.Fatalf("work evidence = %q, want %q; status = %+v", got.WorkExecutionID, wantID, got)
			}
			if tc.started && got.State != "succeeded" {
				t.Fatalf("fast terminal execution was not observed: %+v", got)
			}
		})
	}
}

func TestTriggerTaskLifecycleDeliveryRequiresDurablePullRequestEvent(t *testing.T) {
	store, draft, bridge := triggerLifecycleFixture(t, "fixture-scm")
	now := time.Now().UTC()
	url := "https://github.example/acme/app/pull/7"
	if err := store.PutExecution(t.Context(), &domain.Execution{ID: "exec-delivery", GraphID: draft.ExecutionGraphID,
		GraphVer: 1, RunID: "fixture-run", NodeID: "worker", Status: domain.ExecSucceeded,
		StartedAt: now, RuntimeStartedAt: &now, CompletedAt: &now, Summary: "Opened pull request " + url}); err != nil {
		t.Fatal(err)
	}
	// A URL in the summary, a generic successful tool result, and another
	// task's real delivery event must not become this task's delivery evidence.
	for _, event := range []*domain.Event{
		domain.NewEvent(domain.EvToolCompleted, draft.ExecutionGraphID, map[string]any{"tool": "scm.pull_request.create", "outcome": "ok", "url": url}),
		domain.NewEvent(domain.EvPullRequestOpened, "another-task-graph", map[string]any{"url": url, "number": 7}),
	} {
		if err := store.AppendEvent(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	got, err := bridge.Status(t.Context(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeliveryEventID != "" || got.PullRequestURL != "" || got.State != "blocked" {
		t.Fatalf("unverified URL became delivery evidence: %+v", got)
	}
	opened := domain.NewEvent(domain.EvPullRequestOpened, draft.ExecutionGraphID, map[string]any{"url": url, "number": 7})
	if err := store.AppendEvent(t.Context(), opened); err != nil {
		t.Fatal(err)
	}
	got, err = bridge.Status(t.Context(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeliveryEventID != opened.ID || got.PullRequestURL != url || got.State != "succeeded" || got.WorkExecutionID != "exec-delivery" {
		t.Fatalf("durable delivery and runtime evidence were not propagated: %+v", got)
	}
}

func triggerLifecycleFixture(t *testing.T, scm string) (*fsstore.Store, *taskconfig.Draft, triggerTasks) {
	t.Helper()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := t.Context()
	source := &domain.Graph{Metadata: domain.GraphMetadata{ID: "lifecycle-source"}, Spec: domain.GraphSpec{
		Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Loop: domain.LoopSpec{Mode: domain.LoopOnce}}},
	}}
	if _, err := store.SaveGraph(ctx, source, 0); err != nil {
		t.Fatal(err)
	}
	tasks := taskconfig.Manager{Store: store}
	draft, err := tasks.Save(ctx, "", taskconfig.Input{Title: "Lifecycle fixture", Request: "Exercise tracker lifecycle evidence",
		Outcome: "implement", GraphID: source.Metadata.ID, GraphVersion: source.Metadata.Version, NodeID: "worker", SCMConnection: scm}, 0, "operator")
	if err != nil {
		t.Fatal(err)
	}
	draft.ExecutionGraphID = taskrun.ExecutionID(draft.ID)
	draft, err = tasks.Transition(ctx, draft, "running", "operator")
	if err != nil {
		t.Fatal(err)
	}
	executionGraph := &domain.Graph{Metadata: domain.GraphMetadata{ID: draft.ExecutionGraphID, Labels: map[string]string{domain.LabelTask: draft.ID}}, Spec: source.Spec}
	if _, err := store.SaveGraph(ctx, executionGraph, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.PutGraphRecord(ctx, &domain.GraphRecord{ID: draft.ExecutionGraphID, Version: 1,
		RunID: "fixture-run", State: domain.GraphRunning, DesiredState: domain.GraphRunning, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(store, log)
	runtimeEngine, err := engine.New(engine.Options{Store: store, Broker: memory.New(), Bus: bus, Approvals: hitl.New(store, bus, log), Log: log})
	if err != nil {
		t.Fatal(err)
	}
	manager := taskrun.Manager{Store: store, Engine: runtimeEngine}
	return store, draft, triggerTasks{a: &App{Store: store}, runner: func() taskrun.Manager { return manager }}
}
