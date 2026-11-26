package chat

import (
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"testing"
	"time"
)

func TestTaskCorrectionLimitsFieldsAndCredentials(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"task.edit","task":"demo"}`,
		`{"kind":"task.edit","task":"demo","newRequest":"password=example"}`,
		`{"kind":"task.edit","task":"demo","newRequest":" ","newTitle":"Demo"}`,
		`{"kind":"task.edit","task":"demo","newOutcome":"publish"}`,
		`{"kind":"task.watch","task":"demo","newTitle":"Changed"}`,
	} {
		if _, err := decodePlan(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	p, err := decodePlan(`{"kind":"task.edit","task":"demo","newRequest":"Inspect the config"}`)
	if err != nil {
		t.Fatal(err)
	}
	d := &taskconfig.Draft{ID: "tsk_one", Revision: 1, Status: "draft", Input: taskconfig.Input{Title: "Demo", Request: "Inspect", Outcome: "review", GraphID: "g", GraphVersion: 1, NodeID: "n"}}
	review, err := NewTaskCorrection("owner", "s", d, p.Correction, "v1", time.Now())
	if err != nil || review.TaskCorrection.After.Title != d.Title || review.TaskCorrection.After.Request != "Inspect the config" {
		t.Fatal(review, err)
	}
	d.Request = "token=synthetic"
	if _, err := NewTaskCorrection("owner", "s", d, p.Correction, "v1", time.Now()); err == nil {
		t.Fatal("exposed sensitive source")
	}
}
