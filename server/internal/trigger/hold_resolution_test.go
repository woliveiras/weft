package trigger

import (
	"fmt"
	"strings"
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

func TestSnapshotInvalidationPreservesClarificationRounds(t *testing.T) {
	h := Hold{Round: 1}
	for edits := 1; edits < maxSnapshotEdits; edits++ {
		recordSnapshotInvalidation(&h, "snapshot_changed")
		if h.Round != 1 || h.SnapshotEdits != edits || h.State != "recheck" || h.Intervention {
			t.Fatal(h)
		}
	}
	recordSnapshotInvalidation(&h, "snapshot_changed")
	if h.Round != 1 || h.SnapshotEdits != maxSnapshotEdits || h.State != "operator" || h.Reason != "snapshot_round_limit" || !h.Intervention {
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

func TestPausedRollbackDueUsesBackoffAndRequiresPendingRollback(t *testing.T) {
	now := time.Now().UTC()
	next := now.Add(time.Minute)
	tr := &Trigger{State: Paused, Holds: []Hold{{Publication: "label_pending_only", Removals: []string{LabelInProgress}, RetryLabelAt: &next}}}
	if pausedRollbackDue(tr, now) || !pausedRollbackDue(tr, next) {
		t.Fatal("rollback ignored its backoff")
	}
	tr.Holds[0].Publication = "label_failed"
	if pausedRollbackDue(tr, next) {
		t.Fatal("terminal failure retried")
	}
	tr.Holds[0] = Hold{Publication: "label_pending", Removals: []string{LabelTodo}}
	if pausedRollbackDue(tr, next) {
		t.Fatal("paused trigger scheduled ordinary publication")
	}
}

func TestTransientReadinessFailuresUseTheirOwnBoundedBackoff(t *testing.T) {
	now := time.Now().UTC()
	h := Hold{Round: 1, SnapshotEdits: 2, CommentAttempts: 1}
	for i, delay := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute} {
		recordReadinessRetry(&h, now, "claim_busy", nil)
		if h.RetryFailures != i+1 || h.State != "recheck" || !h.RetryAt.Equal(now.Add(delay)) {
			t.Fatal(h)
		}
	}
	recordReadinessRetry(&h, now, "claim_busy", nil)
	if h.State != "retry_failed" || h.RetryAt != nil || h.Round != 1 || h.SnapshotEdits != 2 || h.CommentAttempts != 1 {
		t.Fatal(h)
	}
}

func TestClaimRemovesWeftWaitingLabelWithoutResolutionHistory(t *testing.T) {
	tr := &Trigger{Label: "custom"}
	i := Issue{Labels: []string{"custom", LabelTodo, LabelFailed, "bug"}}
	for _, tc := range []struct {
		requeued bool
		want     string
	}{
		{false, "custom," + LabelFailed},
		{true, "custom," + LabelTodo + "," + LabelFailed},
	} {
		if got := strings.Join(claimRemovals(tr, i, tc.requeued), ","); got != tc.want {
			t.Fatalf("wanted %s, got %s", tc.want, got)
		}
	}
}

func TestEligibilityChangesShareTheEditCounter(t *testing.T) {
	h := Hold{Round: 1, SnapshotEdits: 1}
	recordSnapshotInvalidation(&h, "eligibility_changed")
	if h.SnapshotEdits != 2 || h.Round != 1 || h.State != "recheck" || h.Intervention {
		t.Fatal(h)
	}
	recordSnapshotInvalidation(&h, "eligibility_changed")
	if h.SnapshotEdits != 3 || h.Round != 1 || h.Reason != "eligibility_edit_limit" || !requiresOperator(h) ||
		strings.Contains(holdMessage(h), "changed repeatedly") {
		t.Fatal(h)
	}
}

func TestRetainedReadinessExpiresAndKeepsNewestEntries(t *testing.T) {
	now := time.Now().UTC()
	tr := &Trigger{RetainedReadiness: map[string]ReadinessProgress{
		"expired": {RetainedAt: now.Add(-retainedReadinessAge)},
		"legacy":  {Round: 2},
	}}
	for i := range retainedReadinessLimit + 1 {
		tr.RetainedReadiness[fmt.Sprint(i)] = ReadinessProgress{RetainedAt: now.Add(time.Duration(i-retainedReadinessLimit) * time.Minute)}
	}
	if !tr.needsReadinessPrune(now) {
		t.Fatal("overflow and expired entries were not detected")
	}
	tr.pruneRetainedReadiness(now)
	if _, ok := tr.RetainedReadiness["expired"]; ok || len(tr.RetainedReadiness) != retainedReadinessLimit ||
		!tr.RetainedReadiness["legacy"].RetainedAt.Equal(now) {
		t.Fatalf("retention bounds or legacy age not applied: %+v", tr.RetainedReadiness)
	}
	for _, oldest := range []string{"0", "1"} {
		if _, ok := tr.RetainedReadiness[oldest]; ok {
			t.Fatalf("oldest entry %s was kept", oldest)
		}
	}
}
