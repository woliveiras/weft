package chat

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/llm"
)

func TestTaskLinkRequiresAUserSuppliedCanonicalID(t *testing.T) {
	for _, test := range []struct {
		input string
		want  bool
	}{
		{"Me de o link para a task `tsk_example`.", true},
		{"Open tsk_example (tsk_example).", true},
		{"Open TSK_EXAMPLE.", true},
		{"Me manda o link para a outra task.", false},
		{"Me manda o link tsk_another.", false},
		{"Open tsk_example or tsk_another.", false},
		{"Open tsk_example-missing", false},
		{"Open tsk_example-", false},
		{"Open prepreftsk_example", false},
	} {
		if got := MatchesTaskLinkTarget(test.input, "tsk_example", ""); got != test.want {
			t.Errorf("%q: matched=%t want %t", test.input, got, test.want)
		}
	}
}

func TestTaskLinkUsesOnlyThePreviousVerifiedTaskForReferences(t *testing.T) {
	session := Session{Messages: []Message{
		{ID: "earlier", Role: "assistant", Task: &TaskSummary{ID: "tsk_older"}},
		{ID: "prior", Role: "assistant", Task: &TaskSummary{ID: "tsk_current"}},
		{ID: "follow-up", Role: "user", Text: "Me envia o link dela?"},
	}}
	previous := session.PriorTaskLinkTarget("follow-up")
	if previous != "tsk_current" || !MatchesTaskLinkTarget(session.Messages[2].Text, "tsk_current", previous) || MatchesTaskLinkTarget(session.Messages[2].Text, "tsk_older", previous) {
		t.Fatalf("follow-up was not bound to the immediately preceding task: %q", previous)
	}
	if MatchesTaskLinkTarget("Me envia o link da tsk_missing?", "tsk_current", previous) {
		t.Fatal("a previous task overrode an explicit but different ID")
	}
	session.Messages[1].Task = nil
	if previous := session.PriorTaskLinkTarget("follow-up"); previous != "" {
		t.Fatalf("an unrelated preceding reply reused an older task: %q", previous)
	}
	session.Messages[1].Selection = &Selection{Area: "tasks", ID: "tsk_selected"}
	if previous := session.PriorTaskLinkTarget("follow-up"); previous != "tsk_selected" {
		t.Fatalf("did not use the immediately preceding verified selection: %q", previous)
	}
}

func TestTaskLinkIsAnExactInspectTaskMode(t *testing.T) {
	for _, test := range []struct {
		name, args string
		view       string
		valid      bool
	}{
		{"inspect_task", `{"task":"tsk_example","taskView":"link"}`, TaskLinkView, true},
		{"inspect_task", `{"task":"A saved task name"}`, "", true},
		{"inspect_task", `{"task":"A saved task name","taskView":"link"}`, "", false},
		{"inspect_task", `{"task":"tsk_example","taskView":"results"}`, "", false},
		{"inspect_task", `{"task":"tsk_example","taskView":"link","href":"#/tasks/tsk_forged"}`, "", false},
		{"watch_task", `{"task":"tsk_example","taskView":"link"}`, "", false},
	} {
		plan, recognized, err := NativeOperationPlan(llm.ToolCall{Name: test.name, Arguments: json.RawMessage(test.args)})
		if !recognized || test.valid && (err != nil || plan.Kind != InspectTask || plan.TaskView != test.view) || !test.valid && !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("%s %s: plan=%+v error=%v", test.name, test.args, plan, err)
		}
	}
}
