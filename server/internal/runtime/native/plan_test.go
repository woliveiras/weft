package native

import (
	"context"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/runtime"
)

type planEnv struct {
	scriptedEnv
	plans [][]runtime.PlanItem
}

func (e *planEnv) RecordPlan(_ context.Context, items []runtime.PlanItem) {
	e.plans = append(e.plans, items)
}

func TestPlanUpdateIsRecordedAndReturnedToTheModel(t *testing.T) {
	env := &planEnv{scriptedEnv: scriptedEnv{replies: []*llm.ChatResponse{
		toolCall("plan.update", `{"items":[{"step":"read calc.go","status":"done"},{"step":"fix Add","status":"in_progress"},{"step":"run tests","status":"pending"}]}`),
		text("done"),
	}}}
	if _, err := New().Turn(context.Background(), &runtime.TurnInput{Prompt: "fix it"}, env); err != nil {
		t.Fatal(err)
	}
	if len(env.plans) != 1 || len(env.plans[0]) != 3 || env.plans[0][1].Status != "in_progress" {
		t.Fatalf("%+v", env.plans)
	}
	last := env.asked[len(env.asked)-1].Messages
	got := last[len(last)-1].Content
	if got != "plan updated (1/3 done):\n[x] read calc.go\n[>] fix Add\n[ ] run tests" {
		t.Fatalf("%q", got)
	}
	if len(env.tools) != 0 {
		t.Fatalf("plan.update must not go to the tool gateway: %v", env.tools)
	}
	var offered bool
	for _, tool := range env.asked[0].Tools {
		offered = offered || tool.Name == "plan.update"
	}
	if !offered {
		t.Fatal("plan.update not offered")
	}
}

func TestInvalidPlansAreRejectedWithTheReason(t *testing.T) {
	for args, want := range map[string]string{
		`{"items":[]}`: "1 to 20 steps",
		`{"items":[{"step":"a","status":"started"}]}`:                                         `status "started"`,
		`{"items":[{"step":" ","status":"done"}]}`:                                            "step 1 is empty",
		`{"items":[{"step":"a","status":"in_progress"},{"step":"b","status":"in_progress"}]}`: "only one step",
		`{"items":"x"}`: "invalid plan",
	} {
		env := &planEnv{}
		if out := updatePlan(context.Background(), env, []byte(args)); !strings.HasPrefix(out, "error: ") || !strings.Contains(out, want) || len(env.plans) != 0 {
			t.Fatalf("%s: %q %v", args, out, env.plans)
		}
	}
	// Without the engine extension the plan still works inside the turn.
	if out := updatePlan(context.Background(), &scriptedEnv{}, []byte(`{"items":[{"step":"`+strings.Repeat("é", 300)+`","status":"pending"}]}`)); !strings.HasPrefix(out, "plan updated (0/1 done)") || !strings.HasSuffix(out, "…") {
		t.Fatalf("%q", out)
	}
}
