package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestTaskPayloadNativePlanAcceptsOneCompleteSafeReplacement(t *testing.T) {
	const payload = `{"sections":["upgrade","compatibility"],"review":true}`
	cases := []struct {
		name string
		args map[string]any
		ok   bool
	}{
		{"edit_task_draft", map[string]any{"task": "tsk_release", "newPayloadJson": payload}, true},
		{"edit_task_draft", map[string]any{"task": "tsk_release", "newPayloadJson": `{incomplete}`}, false},
		{"edit_task_draft", map[string]any{"task": "tsk_release", "newPayloadJson": `{"p\u0061ssword":"fixture-value"}`}, false},
		{"edit_task_draft", map[string]any{"task": "tsk_release", "newPayloadJson": `{"notes":"\u0041uthorization: Bearer fixture-private-value"}`}, false},
		{"edit_task_draft", map[string]any{"task": "tsk_release", "newPayloadJson": payload, "newRequest": "Edit this too"}, false},
		{"edit_task_draft", map[string]any{"task": "tsk_release", "newPayloadJson": payload, "messageType": "different"}, false},
		{"watch_task", map[string]any{"task": "tsk_release", "newPayloadJson": payload}, false},
		{"edit_task_draft", map[string]any{"task": "tsk_release", "newPayloadJson": `{"section":{"p\u0061ssword":"synthetic"},"section":"docs"}`}, false},
	}
	for _, tc := range cases {
		args, err := json.Marshal(tc.args)
		if err != nil {
			t.Fatal(err)
		}
		p, recognized, err := NativeOperationPlan(llm.ToolCall{Name: tc.name, Arguments: args})
		if !recognized || tc.ok && (err != nil || p.Kind != CorrectTask || p.CorrectionPayload != payload) || !tc.ok && !errors.Is(err, ErrInvalidPlan) {
			t.Errorf("%s %s: plan=%+v, error=%v", tc.name, args, p, err)
		}
	}
}

func TestTaskPayloadReviewBindsOldAndNewJSONAndTheChannel(t *testing.T) {
	const replacement = `{"section":"upgrade"}`
	d := taskconfig.Draft{ID: "tsk_release", Revision: 3, Status: "draft", Input: taskconfig.Input{
		Title: "Release", Request: strings.Repeat("Detailed instructions. ", 350), Outcome: "review",
		GraphID: "workflow", GraphVersion: 2, NodeID: "reader", ChannelID: "requests", MessageType: "review.request", PayloadJSON: `{"section":"summary"}`,
	}}
	now := time.Now()
	r, err := NewTaskPayloadCorrection("owner", "session", &d, replacement, "workflow-digest", now)
	if err != nil || r.DecisionRole() != config.RoleOperator || r.TaskCorrection.Before != nil || r.TaskCorrection.After != nil {
		t.Fatalf("payload preview included unrelated text or had incorrect role: %+v, %v", r, err)
	}
	p := r.TaskCorrection.Payload
	if p == nil || p.Before != d.PayloadJSON || p.After != replacement || p.Channel != "requests" || p.MessageType != "review.request" {
		t.Fatalf("incorrect payload preview: %+v", p)
	}
	if err := r.ValidateConfirmation("owner", "session", r.Digest, now); err != nil {
		t.Fatal(err)
	}
	p.Channel = "different"
	if err := r.ValidateConfirmation("owner", "session", r.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed channel retained original digest: %v", err)
	}
	p.Channel = d.ChannelID
	p.After = `{"section":"other"}`
	if err := r.ValidateConfirmation("owner", "session", r.Digest, now); !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("changed replacement retained original digest: %v", err)
	}
	d.PayloadJSON = `{"password":"fixture-value"}`
	if r, err := NewTaskPayloadCorrection("owner", "session", &d, replacement, "workflow-digest", now); err == nil || r != nil {
		t.Fatal("sensitive original became visible in a review")
	}
}
