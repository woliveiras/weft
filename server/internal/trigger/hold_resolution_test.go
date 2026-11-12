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
	recordHoldResolution(tr, 0, "Resolved", "operator", now)
	if len(tr.Holds) != 1 || tr.Holds[0].Key != "b" || tr.ReadinessBlock != "" ||
		len(tr.HoldResolutions) != 20 || tr.HoldResolutions[19] != (HoldResolution{Actor: "operator", Key: "a", Round: 2, Reason: "Resolved", ResolvedAt: now}) {
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

func TestPublishedCommentRequiresVerifiedAuthorAndKnownID(t *testing.T) {
	h := Hold{Marker: "hold_marker", State: "waiting", CommentAuthor: "700"}
	body := holdMessage(h)
	rows := []issueComment{{ID: "1", AuthorID: "other", Body: body}, {ID: "2", AuthorID: "700", Body: body}, {ID: "3", AuthorID: "700", Body: body}}
	if got := publishedCommentIndex(h, rows); got != 1 {
		t.Fatal(got)
	}
	h.CommentID = "3"
	if got := publishedCommentIndex(h, rows); got != 2 {
		t.Fatal(got)
	}
	h.CommentID = "1"
	if got := publishedCommentIndex(h, rows); got != -1 {
		t.Fatal(got)
	}
}

func TestHoldGenerationChangesWithRoundOrMarker(t *testing.T) {
	initial := Hold{Round: 1, Marker: "first"}
	initial.Generation = nextHoldGeneration(Hold{}, initial)
	retry := initial
	retry.CommentAttempts++
	if nextHoldGeneration(initial, retry) != initial.Generation {
		t.Fatal("publication retry changed the hold generation")
	}
	nextRound := initial
	nextRound.Round++
	intervention := initial
	intervention.Marker = "intervention"
	for _, h := range []Hold{nextRound, intervention} {
		if token := nextHoldGeneration(initial, h); token == "" || token == initial.Generation {
			t.Fatal("the previous generation remained valid")
		}
	}
}

func TestLegacyHoldHasStableGenerationWhileDisabled(t *testing.T) {
	old := &Trigger{ID: "legacy", ReadinessDisabled: true, Holds: []Hold{{Key: "acme/app#7"}}}
	otherRead := *old
	otherRead.Holds = []Hold{old.Holds[0]}
	old.fillHoldGenerations()
	otherRead.fillHoldGenerations()
	if token := old.Holds[0].Generation; token == "" || token != otherRead.Holds[0].Generation {
		t.Fatal("independent reads returned different generations for the legacy hold")
	}
}
