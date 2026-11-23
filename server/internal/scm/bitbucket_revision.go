package scm

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var bitbucketFullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)
var bitbucketShortCommit = regexp.MustCompile(`^[0-9a-f]{12}$`)

// Bitbucket's embedded PR commits may use twelve-character identifiers. Resolve
// only those immutable prefixes, never a mutable branch or a provider-supplied URL.
func (b *bitbucketCloud) fullCommit(ctx context.Context, hash string) (string, error) {
	if bitbucketFullCommit.MatchString(hash) {
		return hash, nil
	}
	if !bitbucketShortCommit.MatchString(hash) {
		return "", errors.New("pull request commit identity is missing or invalid")
	}
	var commit struct {
		Hash       string `json:"hash"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := b.http.do(ctx, "GET", b.path()+"/commit/"+hash, nil, &commit); err != nil {
		return "", errors.New("pull request commit identity could not be resolved")
	}
	if !bitbucketFullCommit.MatchString(commit.Hash) || !strings.HasPrefix(commit.Hash, hash) || !strings.EqualFold(commit.Repository.FullName, b.c.Repository) {
		return "", errors.New("resolved pull request commit identity does not match")
	}
	return commit.Hash, nil
}

func (b *bitbucketCloud) pullRevisions(ctx context.Context, p *bbCloudPull) error {
	head, err := b.fullCommit(ctx, p.Source.Commit.Hash)
	if err != nil {
		return err
	}
	base, err := b.fullCommit(ctx, p.Destination.Commit.Hash)
	if err != nil {
		return err
	}
	p.Source.Commit.Hash, p.Destination.Commit.Hash = head, base
	return nil
}

func (b *bitbucketCloud) deliveredPull(ctx context.Context, p bbCloudPull, created bool) *PullRequest {
	if err := b.pullRevisions(ctx, &p); err != nil {
		// The PR already exists. Retain that delivery without falsely advertising
		// a reviewed revision or returning an error that invites another POST.
		pr := p.pr(created)
		pr.HeadSHA, pr.BaseSHA = "", ""
		pr.reviewRevisionError = "commit_resolution_failed"
		return pr
	}
	return p.pr(created)
}
