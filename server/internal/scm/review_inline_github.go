package scm

import (
	"context"
	"errors"
	"fmt"
)

type githubInlineReceipt struct {
	ID               int64  `json:"id"`
	Body             string `json:"body"`
	Path             string `json:"path"`
	Side             string `json:"side"`
	Line             int    `json:"line"`
	OriginalLine     int    `json:"original_line"`
	CommitID         string `json:"commit_id"`
	OriginalCommitID string `json:"original_commit_id"`
}

func (r githubInlineReceipt) matches(head string, comment ReviewComment, body string) bool {
	commit, line := r.OriginalCommitID, r.OriginalLine
	if commit == "" && line == 0 {
		commit, line = r.CommitID, r.Line
	}
	return r.ID > 0 && r.Body == body && r.Path == comment.Path && r.Side == comment.Side && line == comment.Line && commit == head
}

func (g *github) writeInlineComment(ctx context.Context, number int, head string, comment ReviewComment, body string) (int64, error) {
	h, err := g.reviewHTTP()
	if err != nil {
		return 0, err
	}
	var received githubInlineReceipt
	path := fmt.Sprintf("/repos/%s/pulls/%d/comments", g.c.Repository, number)
	args := map[string]any{"body": body, "path": comment.Path, "side": comment.Side, "line": comment.Line, "commit_id": head}
	if err := h.do(ctx, "POST", path, args, &received); err != nil {
		return 0, err
	}
	if !received.matches(head, comment, body) {
		return 0, errors.New("SCM did not identify the published line comment")
	}
	return received.ID, nil
}

func (g *github) findInlineComment(ctx context.Context, number int, head string, comment ReviewComment, body string) (int64, error) {
	h, err := g.reviewHTTP()
	if err != nil {
		return 0, err
	}
	for page := 1; page <= adapterPageLimit; page++ {
		var comments []githubInlineReceipt
		path := fmt.Sprintf("/repos/%s/pulls/%d/comments?per_page=100&page=%d", g.c.Repository, number, page)
		if err := h.do(ctx, "GET", path, nil, &comments); err != nil {
			return 0, err
		}
		for _, r := range comments {
			if r.matches(head, comment, body) {
				return r.ID, nil
			}
		}
		if len(comments) < 100 {
			return 0, nil
		}
	}
	return 0, errors.New("SCM comment search exceeded the page limit")
}
