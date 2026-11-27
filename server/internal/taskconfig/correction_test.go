package taskconfig

import (
	"reflect"
	"testing"
)

func TestCorrectedInputBindsGeneratedPayloadAndPreservesCustomPayload(t *testing.T) {
	d := Draft{Status: "draft", Input: Input{Title: "Original", Request: "Old request", Outcome: "review", GraphID: "sample", GraphVersion: 2, NodeID: "editor", ChannelID: "in", MessageType: "issue", PayloadJSON: taskRequestPayload("Original", "Old request")}}
	next := d.Text()
	next.Request = "New request"
	in, err := d.CorrectedInput(next)
	if err != nil || in.PayloadJSON != taskRequestPayload(d.Title, next.Request) || in.GraphVersion != 2 || in.NodeID != "editor" {
		t.Fatal(in, err)
	}
	d.PayloadJSON = `{"attachment":"local.txt"}`
	if _, err := d.CorrectedInput(next); err == nil {
		t.Fatal("rewrote a custom payload")
	}
	next = d.Text()
	next.Outcome = "implement"
	in, err = d.CorrectedInput(next)
	if err != nil || in.PayloadJSON != d.PayloadJSON {
		t.Fatal(in, err)
	}
	d.Status = "running"
	if _, err = d.CorrectedInput(next); err == nil {
		t.Fatal("edited running task")
	}
}

func TestReplacedPayloadInputPreservesAllOtherInput(t *testing.T) {
	d := Draft{Status: "draft", Input: Input{Title: "Original", Request: "Original request", Outcome: "review", GraphID: "sample", GraphVersion: 2,
		NodeID: "editor", ResultNodeID: "result", ChannelID: "in", MessageType: "issue", PayloadJSON: `{"source":"original"}`, ExecutionMode: "workflow"}}
	const replacement = `{"source":"new","items":[1,2]}`
	want := d.Input
	want.PayloadJSON = replacement
	got, err := d.ReplacedPayloadInput(replacement)
	if err != nil || !reflect.DeepEqual(got, want) || d.PayloadJSON == replacement {
		t.Fatalf("payload replacement changed the saved input beyond the selected field: %+v, %v", got, err)
	}
	if _, err := d.ReplacedPayloadInput(`missing-json`); err == nil {
		t.Fatal("accepted an invalid replacement")
	}
	d.EnvironmentApprovalID = "pending-environment-decision"
	if _, err := d.ReplacedPayloadInput(replacement); err == nil {
		t.Fatal("edited a draft with an environment approval dependency")
	}
	d.EnvironmentApprovalID = ""
	d.ChannelID = ""
	if _, err := d.ReplacedPayloadInput(replacement); err == nil {
		t.Fatal("replaced a payload without an input channel")
	}
}
