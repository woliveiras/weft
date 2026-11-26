package scm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/woliveiras/weft/server/internal/tools"
)

func pullRequestMetadataResult(c Connection, status string, snapshot *ReviewSnapshot) (*tools.Result, error) {
	data, err := json.Marshal(struct {
		Connection         string          `json:"connection"`
		ConnectionRevision string          `json:"connectionRevision"`
		Provider           string          `json:"provider"`
		Repository         string          `json:"repository"`
		Status             string          `json:"status"`
		PullRequest        *ReviewSnapshot `json:"pullRequest,omitempty"`
	}{c.Name, c.UpdatedAt.UTC().Format(time.RFC3339Nano), c.Kind, c.Repository, status, snapshot})
	if err != nil {
		return nil, err
	}
	return &tools.Result{Content: string(data), Data: data}, nil
}

func (g *github) checkPullRequestListing(ctx context.Context) error {
	h, err := g.reviewHTTP()
	if err != nil {
		return err
	}
	var rows []json.RawMessage
	if err := h.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls?state=all&per_page=1", g.c.Repository), nil, &rows); err != nil {
		return err
	}
	if rows == nil {
		return errors.New("unexpected pull request listing")
	}
	return nil
}

func (b *bitbucketCloud) checkPullRequestListing(ctx context.Context) error {
	return checkPagedPullRequestListing(ctx, b.http, fmt.Sprintf("/repositories/%s/pullrequests?pagelen=1", b.c.Repository))
}

func (b *bitbucketDC) checkPullRequestListing(ctx context.Context) error {
	return checkPagedPullRequestListing(ctx, b.http, b.path()+"/pull-requests?state=ALL&limit=1")
}

func checkPagedPullRequestListing(ctx context.Context, h *providerHTTP, path string) error {
	var page struct {
		Values []json.RawMessage `json:"values"`
	}
	if err := h.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return err
	}
	if page.Values == nil {
		return errors.New("unexpected pull request listing")
	}
	return nil
}
