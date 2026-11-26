package scm

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestReviewedCommentCapabilityRequiresExactGrantAndControlPlane(t *testing.T) {
	reg := tools.NewRegistry()
	reg.MustRegister(reviewCommentTool{})
	gateway := tools.NewGateway(reg, nil, nil)
	for _, name := range []string{"*", "scm.pull_request.*", CapReviewSummary, CapReviewComment} {
		p := &tools.Principal{Grants: []domain.ToolGrant{{Name: name, Repositories: []string{"origin"}}}}
		if schemas := reg.Schemas(p); len(schemas) != 0 {
			t.Fatalf("runtime exposure for %q: %+v", name, schemas)
		}
		if _, err := gateway.Invoke(t.Context(), p, CapReviewComment, json.RawMessage(`{}`)); !errors.Is(err, tools.ErrDenied) {
			t.Fatalf("unreviewed invocation allowed for %q: %v", name, err)
		}
		if name != CapReviewComment {
			if _, err := InvokeReviewedComments(t.Context(), gateway, p, ReviewCommentsRequest{}); !errors.Is(err, tools.ErrDenied) {
				t.Fatalf("implicit grant %q allowed reviewed invocation: %v", name, err)
			}
		}
	}
}

func TestInlineReconciliationMatchesOriginalAnchorAndMarker(t *testing.T) {
	comment := ReviewComment{Path: "file.md", Side: "RIGHT", Line: 7, Body: "Fixture note."}
	body := reviewCommentBody(comment.Body, "exact")
	receipt := githubInlineReceipt{ID: 42, Body: body, Path: comment.Path, Side: "RIGHT", Line: 20, OriginalLine: 7, CommitID: "new", OriginalCommitID: "reviewed"}
	if !receipt.matches("reviewed", comment, body) {
		t.Fatal("original reviewed line was not recognized after it moved")
	}
	if receipt.matches("new", comment, body) || receipt.matches("reviewed", comment, reviewCommentBody(comment.Body, "other")) {
		t.Fatal("another revision or marker could confirm the reviewed write")
	}
}
