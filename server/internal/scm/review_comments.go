package scm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

func (g *github) FindComment(ctx context.Context, number int, body string) (bool, error) {
	h, err := g.reviewHTTP()
	if err != nil {
		return false, err
	}
	for page := 1; page <= adapterPageLimit; page++ {
		var comments []struct {
			Body string `json:"body"`
		}
		err = h.do(ctx, "GET", fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", g.c.Repository, number, page), nil, &comments)
		if err != nil {
			return false, err
		}
		if comments == nil {
			return false, errors.New("SCM returned an incomplete comment collection")
		}
		for _, comment := range comments {
			if comment.Body == body {
				return true, nil
			}
		}
		if len(comments) < 100 {
			return false, nil
		}
	}
	return false, errors.New("SCM comment search exceeded the page limit")
}
func (b *bitbucketCloud) FindComment(ctx context.Context, number int, body string) (bool, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/comments?pagelen=100", b.path(), number)
	for page := 0; page < adapterPageLimit; page++ {
		var result struct {
			Next   string `json:"next"`
			Values []struct {
				Deleted bool  `json:"deleted"`
				Pending *bool `json:"pending"`
				Content struct {
					Raw string `json:"raw"`
				} `json:"content"`
			} `json:"values"`
		}
		if err := b.http.do(ctx, "GET", path, nil, &result); err != nil {
			return false, err
		}
		if result.Values == nil {
			return false, errors.New("SCM returned an incomplete comment collection")
		}
		for _, comment := range result.Values {
			if !comment.Deleted && comment.Pending != nil && !*comment.Pending && comment.Content.Raw == body {
				return true, nil
			}
		}
		if result.Next == "" {
			return false, nil
		}
		var err error
		path, err = b.http.nextPage(path, result.Next)
		if err != nil {
			return false, err
		}
	}
	return false, errors.New("SCM comment search exceeded the page limit")
}
func (b *bitbucketDC) FindComment(ctx context.Context, number int, body string) (bool, error) {
	start := 0
	for page := 0; page < adapterPageLimit; page++ {
		var result dcPage[struct {
			Text  string `json:"text"`
			State string `json:"state"`
		}]
		path := fmt.Sprintf("%s/pull-requests/%d/comments?limit=100&start=%s", b.path(), number, strconv.Itoa(start))
		if err := b.http.do(ctx, "GET", path, nil, &result); err != nil {
			return false, err
		}
		next, err := result.next(start)
		if err != nil {
			return false, err
		}
		for _, comment := range result.Values {
			if (comment.State == "OPEN" || comment.State == "RESOLVED") && comment.Text == body {
				return true, nil
			}
		}
		if next < 0 {
			return false, nil
		}
		start = next
	}
	return false, errors.New("SCM comment search exceeded the page limit")
}
