package taskconfig

import "testing"

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
