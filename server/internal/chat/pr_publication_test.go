package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/scm"
)

func TestPRDraftSelectionBindsExactComments(t *testing.T) {
	summary, comments := parsePRDraft(`{"summary":"Use a guard.","comments":[{"path":"app.txt","side":"RIGHT","line":2,"body":"Add a guard."},{"path":"../outside","side":"RIGHT","line":4,"body":"Invalid."},{"path":"app.txt","side":"LEFT","line":3,"body":"Explain this removal."}]}`)
	if summary != "Use a guard." || len(comments) != 2 || comments[0].ID != 1 || comments[1].ID != 3 {
		t.Fatalf("invalid draft projection: %q %+v", summary, comments)
	}
	analysis := &OperationReview{ID: "analysis", State: ReviewApplied, ResultID: "execution", PR: &PRAnalysis{Number: 418}, PRComments: comments}
	for _, invalid := range [][]int{{}, {1, 1}, {2}, {1, 3, 19}} {
		if _, err := analysis.SelectPRComments(invalid); err == nil {
			t.Fatalf("accepted unknown or repeated selection %v", invalid)
		}
	}
	proposal, err := analysis.SelectPRComments([]int{3})
	if err != nil || len(proposal.Comments) != 1 || proposal.Comments[0] != comments[1] || proposal.ExecutionID != "execution" {
		t.Fatalf("incorrect reviewed selection: %+v %v", proposal, err)
	}
	now := time.Now().UTC()
	review, err := newReview("owner", "session", now, OperationReview{PRPublication: &proposal})
	if err != nil {
		t.Fatal(err)
	}
	if err := review.ValidateConfirmation("owner", "session", review.Digest, now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PRCommentPublication){
		func(p *PRCommentPublication) { p.Analysis.Head = strings.Repeat("a", 40) },
		func(p *PRCommentPublication) { p.Comments[0].Body = "Changed body." },
		func(p *PRCommentPublication) { p.ExecutionID = "another-execution" },
	} {
		changed := proposal
		changed.Comments = append([]PRDraftComment(nil), proposal.Comments...)
		mutate(&changed)
		tampered := *review
		tampered.PRPublication = &changed
		if err := tampered.ValidateConfirmation("owner", "session", review.Digest, now); err == nil {
			t.Fatal("digest authorized a different scope or comment")
		}
	}
}

func TestPRCommentAnchorsRequireChangedLinesInCompleteHunks(t *testing.T) {
	diff := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -5,3 +7,4 @@\n stable context\n-removed\n+added\n+extra\n stable context\n" +
		"diff --git a/gone.txt b/gone.txt\n--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-removed file\n" +
		"diff --git a/truncated b/truncated\n--- a/truncated\n+++ b/truncated\n@@ -0,0 +1,2 @@\n+not enough lines\n"
	for _, tc := range []struct {
		path, side string
		line       int
		valid      bool
	}{
		{"file.txt", "LEFT", 6, true}, {"file.txt", "RIGHT", 8, true}, {"file.txt", "RIGHT", 9, true},
		{"gone.txt", "LEFT", 1, true}, {"file.txt", "RIGHT", 7, false}, {"file.txt", "LEFT", 8, false},
		{"truncated", "RIGHT", 1, false}, {"otro", "RIGHT", 8, false},
	} {
		if got := AnchorsPRComments(diff, []PRDraftComment{{ReviewComment: scm.ReviewComment{Path: tc.path, Side: tc.side, Line: tc.line}}}); got != tc.valid {
			t.Errorf("%s %s %d = %v, want %v", tc.path, tc.side, tc.line, got, tc.valid)
		}
	}
}
