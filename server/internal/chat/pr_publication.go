package chat

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

const PublishPRComments = "pr.publish_comments"

type PRDraftComment struct {
	ID int `json:"id"`
	scm.ReviewComment
}

type PRCommentPublication struct {
	Analysis    PRAnalysis       `json:"analysis"`
	AnalysisID  string           `json:"analysisId"`
	ExecutionID string           `json:"executionId"`
	Comments    []PRDraftComment `json:"comments"`
}

func parsePRDraft(output string) (string, []PRDraftComment) {
	const unavailable = "Open the execution to inspect the review result."
	if !safeText(output, 4000) {
		return unavailable, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		return output, nil
	}
	var draft struct {
		Summary  string
		Comments []scm.ReviewComment
	}
	if json.Unmarshal([]byte(output), &draft) != nil || strings.TrimSpace(draft.Summary) == "" || !safeText(draft.Summary, 800) {
		return unavailable, nil
	}
	var comments []PRDraftComment
	seen := make(map[scm.ReviewComment]bool)
	for i, c := range draft.Comments {
		if i == scm.MaxReviewComments {
			break
		}
		if c.Valid() && safeText(c.Body, 480) && shortLabel(c.Path) && !seen[c] {
			comments = append(comments, PRDraftComment{ID: i + 1, ReviewComment: c})
			seen[c] = true
		}
	}
	return draft.Summary, comments
}

func (p PRCommentPublication) Request(reconcile bool) scm.ReviewCommentsRequest {
	a := p.Analysis
	comments := make([]scm.ReviewComment, 0, len(p.Comments))
	for _, comment := range p.Comments {
		comments = append(comments, comment.ReviewComment)
	}
	return scm.ReviewCommentsRequest{Connection: a.Connection, ConnectionRevision: a.ConnectionRevision, Number: a.Number,
		HeadSHA: a.Head, BaseSHA: a.Base, Comments: comments, Reconcile: reconcile}
}

func (r OperationReview) SelectPRComments(ids []int) (PRCommentPublication, error) {
	if r.PR == nil || r.State != ReviewApplied || r.ResultID == "" || len(ids) == 0 || len(ids) > scm.MaxReviewComments {
		return PRCommentPublication{}, ErrInvalidPlan
	}
	selected := make([]PRDraftComment, 0, len(ids))
	for _, draft := range r.PRComments {
		if slices.Contains(ids, draft.ID) {
			selected = append(selected, draft)
		}
	}
	if len(selected) != len(ids) {
		return PRCommentPublication{}, ErrInvalidPlan
	}
	return PRCommentPublication{Analysis: *r.PR, AnalysisID: r.ID, ExecutionID: r.ResultID, Comments: selected}, nil
}

func (s Sessions) AddPRCommentReview(ctx context.Context, owner, id string, publication PRCommentPublication, now time.Time) (*Session, error) {
	review, err := newReview(owner, id, now, OperationReview{PRPublication: &publication})
	if err != nil {
		return nil, err
	}
	return s.update(ctx, owner, id, func(session *Session) (bool, error) {
		for _, m := range session.Messages {
			if r := m.Review; r != nil && r.PRPublication != nil && r.PRPublication.AnalysisID == publication.AnalysisID &&
				slices.Equal(r.PRPublication.Comments, publication.Comments) && r.State != ReviewDeclined && r.State != ReviewConflict &&
				(r.State != ReviewAwaiting || now.Before(r.ExpiresAt)) {
				return false, nil
			}
		}
		session.Messages = append(session.Messages, Message{ID: review.ID, Role: "assistant", Text: "Review these line comments before publishing them to the pull request.", Review: review, Created: now.UTC()})
		session.LastActivity = now.UTC()
		return true, nil
	})
}

func (s Sessions) ObservePRCommentReview(ctx context.Context, owner, sessionID, id, state string, receipts []scm.ReviewCommentResult) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			m := &session.Messages[i]
			r := m.Review
			if r == nil || r.PRPublication == nil || r.ID != id {
				continue
			}
			if r.State != ReviewApplying && r.State != ReviewUnknown && r.State != ReviewIncomplete {
				return false, nil
			}
			joined := slices.Clone(receipts)
			for i, previous := range r.PRReceipts {
				if i < len(joined) && previous.State != "unknown" {
					joined[i] = previous
				}
			}
			observed := state
			if observed == "" {
				observed = prCommentReviewState(joined)
			}
			if r.State == observed && slices.Equal(r.PRReceipts, joined) {
				return false, nil
			}
			r.State, r.PRReceipts = observed, joined
			m.Unread = true
			session.LastActivity = time.Now().UTC()
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}

func prCommentReviewState(results []scm.ReviewCommentResult) string {
	var published, unknown int
	for _, result := range results {
		switch result.State {
		case "published":
			published++
		case "unknown":
			unknown++
		}
	}
	if published == len(results) {
		return ReviewApplied
	}
	if published == 0 && unknown == len(results) {
		return ReviewUnknown
	}
	return ReviewIncomplete
}
