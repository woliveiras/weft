package chat

import (
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
)

func TestExecutionSummaryDistinguishesRequestedAndObserved(t *testing.T) {
	tree := executioncontrol.Tree{SelectedID: "child", RootID: "root", Nodes: []executioncontrol.Node{
		{Execution: &domain.Execution{ID: "root", GraphID: "g", Status: domain.ExecSucceeded}, Cancellation: &executioncontrol.Request{Scope: "tree"}},
		{Execution: &domain.Execution{ID: "child", GraphID: "g", Status: domain.ExecRunning}},
	}}
	summary, ok := SummarizeExecution(tree)
	if !ok || summary.Active != 1 || summary.Nodes != 2 || summary.State != "running" || !summary.CancellationRequested {
		t.Fatalf("summary %+v", summary)
	}
	tree.Nodes[1].Execution.GraphID = "unsafe/path"
	if _, ok := SummarizeExecution(tree); ok {
		t.Fatal("accepted untrusted graph link")
	}
	for _, body := range []string{`{"kind":"execution.inspect","executionId":"../g"}`, `{"kind":"manual.open","executionId":"exec-1","destination":"agents"}`, `{"kind":"execution.inspect"}`} {
		if _, err := decodePlan(body); err == nil {
			t.Fatal(body)
		}
	}
	if _, err := decodePlan(`{"kind":"execution.inspect","executionId":"exec-1"}`); err != nil {
		t.Fatal(err)
	}
}

func TestCancelIntentRequiresScopeAndBindsTheExactRun(t *testing.T) {
	for _, raw := range []string{`{"kind":"execution.cancel","executionId":"child"}`, `{"kind":"task.cancel","task":"task","executionScope":"tree"}`, `{"kind":"execution.cancel","executionId":"child","executionScope":"all"}`} {
		if _, err := decodePlan(raw); err == nil {
			t.Fatal(raw)
		}
	}
	if _, err := decodePlan(`{"kind":"execution.cancel","executionId":"child","executionScope":"child"}`); err != nil {
		t.Fatal(err)
	}
	s := executioncontrol.CancelScope{Selected: "child", Root: "root", Mode: "child", Run: "first", Graph: "workflow"}
	r, err := NewExecutionCancelReview("owner", "session", s, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r.ExecutionCancel.Run = "second"
	digest, err := r.digest("owner", "session")
	if err != nil || digest == r.Digest {
		t.Fatal("unsealed run", err)
	}
}
