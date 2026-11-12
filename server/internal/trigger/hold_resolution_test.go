package trigger

import (
	"testing"
	"time"
)

func TestHoldReleaseReasonKeepsWaitingAndRollbackLabels(t *testing.T) {
	trigger := &Trigger{Label: "custom", Owner: "owner"}
	for _, tc := range []struct {
		hold     Hold
		issue    Issue
		released bool
	}{
		{Hold{}, Issue{Open: true, Labels: []string{LabelFailed}}, false},
		{Hold{}, Issue{Open: true, Labels: []string{LabelTodo}}, false},
		{Hold{}, Issue{Open: true, Labels: []string{"unrelated"}}, true},
		{Hold{}, Issue{Open: true, Labels: []string{LabelFailed}, Assignee: "other"}, true},
		{Hold{Publication: "label_pending", Removals: []string{LabelInProgress}}, Issue{Open: true, Labels: []string{LabelInProgress}}, false},
	} {
		if reason := holdReleaseReason(trigger, tc.hold, tc.issue); (reason != "") != tc.released {
			t.Errorf("unexpected release reason %q for %+v", reason, tc)
		}
	}
}

func TestRecordHoldResolutionBoundsHistoryAndFreesCapacity(t *testing.T) {
	now := time.Now().UTC()
	tr := &Trigger{Holds: []Hold{{Key: "a", Round: 2}, {Key: "b"}}, HoldResolutions: make([]HoldResolution, 20), ReadinessBlock: readinessCapacity}
	recordHoldResolution(tr, 0, "Resolved", now)
	if len(tr.Holds) != 1 || tr.Holds[0].Key != "b" || tr.ReadinessBlock != "" ||
		len(tr.HoldResolutions) != 20 || tr.HoldResolutions[19] != (HoldResolution{Key: "a", Round: 2, Reason: "Resolved", ResolvedAt: now}) {
		t.Fatalf("resolution not recorded: %+v", tr)
	}
}

func TestEvaluationFailuresShareTheRoundAllowance(t *testing.T) {
	for _, start := range []int{0, 1, 2} {
		h := Hold{Round: start}
		recordEvaluationFailure(&h, "timeout")
		if h.Round != min(start+1, maxReadinessRounds) || h.Intervention != (start >= 1) {
			t.Fatalf("wrong round or intervention: %+v", h)
		}
		if start >= 1 && h.Reason != "failure_round_limit" {
			t.Fatal(h)
		}
	}
}

func TestHoldHasPendingTrackerWrite(t *testing.T) {
	for _, h := range []Hold{{UndoOwner: "owner"}, {Publication: "label_pending"}, {Publication: "label_pending_only"}} {
		if !holdHasPendingWrite(h) {
			t.Fatal(h)
		}
	}
	for _, h := range []Hold{{}, {Publication: "confirmed"}, {Publication: "comment_failed"}} {
		if holdHasPendingWrite(h) {
			t.Fatal(h)
		}
	}
}

func TestSnapshotInvalidationUsesTheSharedRoundAllowance(t *testing.T) {
	h := Hold{}
	recordSnapshotInvalidation(&h)
	if h.Round != 1 || h.State != "recheck" || h.Intervention {
		t.Fatal(h)
	}
	recordSnapshotInvalidation(&h)
	if h.Round != maxReadinessRounds || h.State != "operator" || h.Reason != "snapshot_round_limit" || !h.Intervention {
		t.Fatal(h)
	}
}
