package engine_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/runtime/echo"
	"github.com/woliveiras/weft/server/internal/storage"
)

type reviewCaptureRuntime struct {
	runtime.Runtime
	inputs chan *runtime.TurnInput
}

func (r *reviewCaptureRuntime) Turn(_ context.Context, in *runtime.TurnInput, _ runtime.TurnEnv) (*runtime.TurnResult, error) {
	r.inputs <- in
	return &runtime.TurnResult{Summary: "review fixture completed"}, nil
}

func reviewIntegrationRig(t *testing.T, gated string) (*rig, *domain.Graph, *reviewCaptureRuntime) {
	t.Helper()
	dir := t.TempDir()
	capture := &reviewCaptureRuntime{Runtime: echo.New(), inputs: make(chan *runtime.TurnInput, 10)}
	r := newRig(t, openStore(t, dir), memory.New(), dir, "review-dispatch", func(o *engine.Options) {
		o.Runtimes[domain.RuntimeEcho] = capture
	})
	gate, before := "", ""
	if gated == "channel" {
		gate = ", approval: true"
	}
	if gated == "turn" {
		before = ", approval: {beforeTurn: true}"
	}
	g := r.deploy(`metadata: {id: review-dispatch, name: Review dispatch}
spec:
  channels:
    - {id: requests, to: [reviewer]` + gate + `}
  nodes:
    - {id: reviewer, runtime: {kind: echo}, loop: {mode: event}` + before + `}
`)
	eventually(t, 5*time.Second, "loaded review event replica", func() bool {
		replicas, _ := r.store.ListReplicaStatus(t.Context(), g.Metadata.ID)
		return len(replicas) == 1 && replicas[0].State == domain.ReplicaIdle
	})
	return r, g, capture
}

func reviewIntegrationInput(t *testing.T, r *rig, g *domain.Graph) engine.ReviewDispatch {
	t.Helper()
	rec, err := r.store.GetGraphRecord(t.Context(), g.Metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	m, err := domain.NewMessage("review.requested", "", "", map[string]any{"number": 7})
	if err != nil {
		t.Fatal(err)
	}
	return engine.ReviewDispatch{GraphID: g.Metadata.ID, Version: g.Metadata.Version, Channel: "requests", Node: "reviewer", ExpectedRunID: rec.RunID, Message: m}
}

func pendingReviewApproval(t *testing.T, r *rig, kind domain.ApprovalKind) *domain.Approval {
	t.Helper()
	var found *domain.Approval
	eventually(t, 5*time.Second, "pending review approval", func() bool {
		rows, err := r.store.ListApprovals(t.Context(), storage.ApprovalFilter{GraphID: "review-dispatch", Status: domain.ApprovalPending})
		if err == nil && len(rows) == 1 && rows[0].Kind == kind {
			found = rows[0]
			return true
		}
		return false
	})
	return found
}

func TestReviewDispatchIntegrationPreservesChannelAndTurnGates(t *testing.T) {
	for _, gate := range []string{"channel", "turn"} {
		t.Run(gate, func(t *testing.T) {
			r, g, capture := reviewIntegrationRig(t, gate)
			in := reviewIntegrationInput(t, r, g)
			if run, err := r.eng.DispatchReview(t.Context(), in); err != nil || run != in.ExpectedRunID {
				t.Fatal(run, err)
			}
			kind := domain.ApprovalMessage
			if gate == "turn" {
				kind = domain.ApprovalTurn
			}
			a := pendingReviewApproval(t, r, kind)
			if len(capture.inputs) != 0 {
				t.Fatal("runtime ran before approval")
			}
			if _, err := r.apr.Decide(t.Context(), a.ID, true, "operator", "review fixture", ""); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-capture.inputs:
				if len(got.Inputs) != 1 || got.Inputs[0].ID != in.Message.ID || got.Inputs[0].Target != "reviewer" || got.Inputs[0].MetaString(domain.MetaRunID) != in.ExpectedRunID || got.Inputs[0].MetaInt(domain.MetaGraphVersion) != in.Version {
					t.Fatalf("wrong reviewed input: %+v", got.Inputs)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("approved review did not run")
			}
		})
	}
}

func TestReviewDispatchIntegrationDoesNotStartPausedOrStoppedGraph(t *testing.T) {
	for _, action := range []string{"pause", "stop"} {
		t.Run(action, func(t *testing.T) {
			r, g, capture := reviewIntegrationRig(t, "")
			in := reviewIntegrationInput(t, r, g)
			if err := r.eng.GraphAction(t.Context(), g.Metadata.ID, action, "operator"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.eng.DispatchReview(t.Context(), in); !errors.Is(err, engine.ErrReviewDispatchInactive) {
				t.Fatalf("inactive dispatch = %v", err)
			}
			rec, err := r.store.GetGraphRecord(t.Context(), g.Metadata.ID)
			want := domain.GraphPaused
			if action == "stop" {
				want = domain.GraphStopped
			}
			if err != nil || rec.DesiredState != want || rec.RunID != in.ExpectedRunID || len(capture.inputs) != 0 {
				t.Fatalf("dispatch changed graph lifecycle: %+v %v", rec, err)
			}
		})
	}
}

func TestReviewDispatchIntegrationRejectsStaleQueuedVersionAndRun(t *testing.T) {
	for _, change := range []string{"version", "run"} {
		t.Run(change, func(t *testing.T) {
			r, g, capture := reviewIntegrationRig(t, "channel")
			in := reviewIntegrationInput(t, r, g)
			if _, err := r.eng.DispatchReview(t.Context(), in); err != nil {
				t.Fatal(err)
			}
			a := pendingReviewApproval(t, r, domain.ApprovalMessage)
			if change == "version" {
				g.Metadata.Description = "A separately selected workflow version"
				if _, _, err := r.eng.SaveGraph(t.Context(), g, g.Metadata.Version, "operator"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := r.eng.GraphAction(t.Context(), g.Metadata.ID, "stop", "operator"); err != nil {
					t.Fatal(err)
				}
				eventually(t, 5*time.Second, "stopped old review run", func() bool {
					rec, _ := r.store.GetGraphRecord(t.Context(), g.Metadata.ID)
					return rec != nil && rec.State == domain.GraphStopped
				})
				if err := r.eng.GraphAction(t.Context(), g.Metadata.ID, "start", "operator"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.apr.Decide(t.Context(), a.ID, true, "operator", "review old request", ""); err != nil {
				t.Fatal(err)
			}
			eventually(t, 5*time.Second, "stale queued review is rejected", func() bool {
				rows, _ := r.store.ListMessages(t.Context(), storage.MessageFilter{GraphID: g.Metadata.ID, Status: domain.MsgDeadLettered, Limit: 100})
				for _, row := range rows {
					if row.Message.ID == in.Message.ID {
						return true
					}
				}
				return false
			})
			if len(capture.inputs) != 0 {
				t.Fatal("stale review reached runtime")
			}
		})
	}
}

func TestReviewDispatchIntegrationRechecksAfterBeforeTurnApproval(t *testing.T) {
	r, g, capture := reviewIntegrationRig(t, "turn")
	in := reviewIntegrationInput(t, r, g)
	if _, err := r.eng.DispatchReview(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	a := pendingReviewApproval(t, r, domain.ApprovalTurn)
	if err := r.eng.GraphAction(t.Context(), g.Metadata.ID, "pause", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.apr.Decide(t.Context(), a.ID, true, "operator", "approve after pause", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "review admission cancelled after pause", func() bool {
		x, _ := r.store.GetExecution(t.Context(), a.ExecutionID)
		return x != nil && x.Status == domain.ExecCancelled && x.RuntimeStartedAt == nil
	})
	if len(capture.inputs) != 0 {
		t.Fatal("review ran after its approval outlived the running state")
	}
}

func TestPublicInjectionCannotForgeReviewDispatchMetadata(t *testing.T) {
	r, g, capture := reviewIntegrationRig(t, "")
	if err := r.eng.GraphAction(t.Context(), g.Metadata.ID, "pause", "operator"); err != nil {
		t.Fatal(err)
	}
	m, err := r.eng.Inject(t.Context(), g.Metadata.ID, "requests", "ordinary.request", nil, "", map[string]any{"review_dispatch": true}, "operator")
	if err != nil || m.Metadata["review_dispatch"] != nil {
		t.Fatalf("public injection forged internal review input: %+v %v", m, err)
	}
	if err := r.eng.GraphAction(t.Context(), g.Metadata.ID, "resume", "operator"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-capture.inputs:
		if len(got.Inputs) != 1 || got.Inputs[0].ID != m.ID || got.Inputs[0].Metadata["review_dispatch"] != nil {
			t.Fatal("ordinary injection semantics changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary queued input did not run after resume")
	}
}

type reviewFailingReadStore struct {
	storage.Store
	fail atomic.Bool
}

func (s *reviewFailingReadStore) GetGraphRecord(ctx context.Context, id string) (*domain.GraphRecord, error) {
	if s.fail.Load() {
		return nil, errors.New("review record read unavailable")
	}
	return s.Store.GetGraphRecord(ctx, id)
}

type reviewFailingPublishBroker struct {
	queue.Broker
	fail  atomic.Bool
	calls atomic.Int64
}

func (b *reviewFailingPublishBroker) Publish(ctx context.Context, subject string, m *domain.Message) error {
	b.calls.Add(1)
	if b.fail.Load() {
		return errors.New("review publish unavailable")
	}
	return b.Broker.Publish(ctx, subject, m)
}

func TestReviewDispatchIntegrationStorageAndBrokerFailuresDoNotDeliver(t *testing.T) {
	dir := t.TempDir()
	store := &reviewFailingReadStore{Store: openStore(t, dir)}
	broker := &reviewFailingPublishBroker{Broker: memory.New()}
	capture := &reviewCaptureRuntime{Runtime: echo.New(), inputs: make(chan *runtime.TurnInput, 10)}
	r := newRig(t, store, broker, dir, "review-failures", func(o *engine.Options) { o.Runtimes[domain.RuntimeEcho] = capture })
	g := r.deploy(`metadata: {id: review-dispatch}
spec:
  channels: [{id: requests, to: [reviewer]}]
  nodes: [{id: reviewer, runtime: {kind: echo}, loop: {mode: event}}]
`)
	eventually(t, 5*time.Second, "loaded review event replica", func() bool {
		replicas, _ := store.ListReplicaStatus(t.Context(), g.Metadata.ID)
		return len(replicas) == 1 && replicas[0].State == domain.ReplicaIdle
	})
	in := reviewIntegrationInput(t, r, g)
	store.fail.Store(true)
	if _, err := r.eng.DispatchReview(t.Context(), in); err == nil {
		t.Fatal("storage failure was ignored")
	}
	if broker.calls.Load() != 0 {
		t.Fatal("storage failure reached publication")
	}
	store.fail.Store(false)
	broker.fail.Store(true)
	if _, err := r.eng.DispatchReview(t.Context(), in); err == nil {
		t.Fatal("broker failure was ignored")
	}
	broker.fail.Store(false)
	stats, err := broker.Stats(t.Context(), queue.Subject(g.Metadata.ID, "requests"), "requests")
	if err != nil || stats.Pending != 0 || stats.InFlight != 0 || len(capture.inputs) != 0 || broker.calls.Load() != 1 {
		t.Fatalf("failed publication delivered work: %+v %v", stats, err)
	}
}

func TestReviewDispatchIntegrationRefusesRecordAheadOfLoadedGraph(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, dir)
	r := newRig(t, store, memory.New(), dir, "review-loaded-version", func(o *engine.Options) {
		o.ReconcileInterval = time.Hour
	})
	g := r.deploy(`metadata: {id: review-dispatch}
spec:
  channels: [{id: requests, to: [reviewer]}]
  nodes: [{id: reviewer, runtime: {kind: echo}, loop: {mode: event}}]
`)
	eventually(t, 5*time.Second, "initial review version loaded", func() bool {
		replicas, _ := store.ListReplicaStatus(t.Context(), g.Metadata.ID)
		return len(replicas) == 1 && replicas[0].State == domain.ReplicaIdle
	})
	// Reproduce the durable-save/reconciliation gap without nudging the engine:
	// a valid new document/control version exists, but only v1 is loaded.
	if _, err := store.SaveGraph(t.Context(), g, g.Metadata.Version); err != nil {
		t.Fatal(err)
	}
	rec, err := store.GetGraphRecord(t.Context(), g.Metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Version = g.Metadata.Version
	if err := store.PutGraphRecord(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
	in := reviewIntegrationInput(t, r, g)
	if _, err := r.eng.DispatchReview(t.Context(), in); !errors.Is(err, engine.ErrReviewDispatchStale) {
		t.Fatalf("record alone admitted an unloaded workflow version: %v", err)
	}
	rows, err := store.ListExecutions(t.Context(), storage.ExecutionFilter{GraphID: g.Metadata.ID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("unloaded review version executed: %+v %v", rows, err)
	}
}
