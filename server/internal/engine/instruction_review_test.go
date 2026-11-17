package engine_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/runtime/echo"
	"github.com/woliveiras/weft/server/internal/tools"
)

type instructionReviewRuntime struct {
	*reviewRuntime
	inputs chan *runtime.TurnInput
}

func (r *instructionReviewRuntime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	r.inputs <- in
	return r.reviewRuntime.Turn(ctx, in, env)
}

// Cross-feature regression: the once-loop guard must not consume or prevent the
// review continuation, and a later skill edit cannot rewrite the earlier manifest.
func TestOnceReviewWithVersionedInstructions(t *testing.T) {
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			dir := t.TempDir()
			store := openStore(t, dir)
			guidance := instructions.Manager{Store: store}
			skill := instructions.Input{ID: "review-guidance", Title: "Review guidance", Body: "Follow the first revision.", Enabled: true}
			if _, err := guidance.Save(t.Context(), skill, 0, "admin", false); err != nil {
				t.Fatal(err)
			}
			reviews := review.New(review.Options{Store: store, Root: filepath.Join(dir, "reviews"), Instance: "combined"})
			rt := &instructionReviewRuntime{reviewRuntime: &reviewRuntime{Runtime: echo.New()}, inputs: make(chan *runtime.TurnInput, 4)}
			rig := newRig(t, store, memory.New(), dir, "combined", func(opt *engine.Options) {
				opt.Reviews = reviews
				reg := tools.Builtin(tools.BuiltinConfig{})
				reg.MustRegister(reviews.Tool())
				opt.Tools = gwAdapter{reg, tools.NewGateway(reg, opt.Approvals, opt.Bus)}
				opt.Runtimes[domain.RuntimeEcho] = rt
				opt.Sandboxes = func(_ context.Context, s engine.SandboxSpec) (tools.Sandbox, error) {
					return &tools.ProcessSandbox{Root: s.Workdir}, nil
				}
			})
			go reviews.Run(rig.ctx)
			rig.deploy(`metadata: {name: combined-review}
spec:
  instructions: {skillIds: [review-guidance]}
  nodes:
    - id: worker
      prompt: Preserve the original task.
      runtime: {kind: echo}
      isolation: {mode: docker}
      tools: [review.request]
      loop: {mode: once}
`)
			receive := func() *runtime.TurnInput {
				t.Helper()
				select {
				case in := <-rt.inputs:
					return in
				case <-time.After(5 * time.Second):
					t.Fatal("runtime did not receive turn")
					return nil
				}
			}
			first := receive()
			var gate *review.Record
			eventually(t, 5*time.Second, "waiting for human", func() bool {
				records, err := reviews.List(rig.ctx)
				replicas, repErr := store.ListReplicaStatus(rig.ctx, "combined-review")
				if err != nil || repErr != nil || len(records) != 1 || len(replicas) != 1 {
					return false
				}
				gate = records[0]
				return replicas[0].State == domain.ReplicaAwaitingApproval
			})
			if rt.calls.Load() != 1 {
				t.Fatal("continued before decision")
			}
			before, err := guidance.Snapshot(rig.ctx, first.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(first.Prompt, skill.Body) || !strings.Contains(first.Prompt, "Preserve the original task.") {
				t.Fatal("first task context lost")
			}
			skill.Body = "Follow the second revision."
			if _, err = guidance.Save(rig.ctx, skill, 1, "admin", false); err != nil {
				t.Fatal(err)
			}
			if _, err = reviews.Decide(rig.ctx, gate.ID, decision, "without_execution", "operator", "Candidate inspected", int64(gate.Generation)); err != nil {
				t.Fatal(err)
			}
			expected := domain.ReplicaFailed
			if decision == "approved" {
				second := receive()
				if second.ExecutionID == first.ExecutionID || !strings.Contains(second.Prompt, skill.Body) {
					t.Fatal("continuation did not resolve new guidance")
				}
				if len(second.OperatorMessages) != 1 || !strings.Contains(second.OperatorMessages[0], "Human review result:") || !strings.Contains(second.OperatorMessages[0], gate.ID) {
					t.Fatal("human decision lost")
				}
				snap, err := guidance.Snapshot(rig.ctx, second.ExecutionID)
				if err != nil {
					t.Fatal(err)
				}
				skillFound, taskFound := false, false
				for _, entry := range snap.Entries {
					if entry.Scope == "graph" && entry.Revision == 2 && entry.Included {
						skillFound = true
					}
					if entry.Scope == "task" && entry.Body == second.OperatorMessages[0] && entry.Included {
						taskFound = true
					}
				}
				if !skillFound || !taskFound {
					t.Fatal("continuation manifest missing guidance or decision")
				}
				expected = domain.ReplicaCompleted
			}
			eventually(t, 5*time.Second, "once task terminal state", func() bool {
				replicas, err := store.ListReplicaStatus(rig.ctx, "combined-review")
				return err == nil && len(replicas) == 1 && replicas[0].State == expected
			})
			calls := int32(1)
			if decision == "approved" {
				calls = 2
			}
			if rt.calls.Load() != calls {
				t.Fatal("unexpected continuation count")
			}
			after, err := guidance.Snapshot(rig.ctx, first.ExecutionID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("earlier manifest changed", err)
			}
		})
	}
}
