package scm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	RegisterKind("bitbucket-dc", func(c Connection, token string) (Provider, error) {
		if c.APIURL == "" {
			return nil, errors.New("Bitbucket Data Center needs its site base URL")
		}
		h, err := newProviderHTTP(c.APIURL, token)
		if err != nil {
			return nil, err
		}
		return &bitbucketDC{c: c, http: h}, nil
	})
}

type bitbucketDC struct {
	c    Connection
	http *providerHTTP
}

func (b *bitbucketDC) RemoteURL() string {
	if b.c.GitURL != "" {
		return b.c.GitURL
	}
	project, slug, _ := strings.Cut(b.c.Repository, "/")
	return strings.TrimRight(b.http.base.String(), "/") + "/scm/" + strings.ToLower(project) + "/" + slug + ".git"
}
func (b *bitbucketDC) GitAuthHeader() string { return "Bearer " + b.http.token }
func (b *bitbucketDC) path() string {
	project, slug, _ := strings.Cut(b.c.Repository, "/")
	return "/rest/api/1.0/projects/" + project + "/repos/" + slug
}

type dcRepo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
}

func (r dcRepo) matches(repository string) bool {
	return r.ID > 0 && strings.EqualFold(r.Project.Key+"/"+r.Slug, repository)
}

type dcPage[T any] struct {
	Values []T   `json:"values"`
	Last   *bool `json:"isLastPage"`
	Next   *int  `json:"nextPageStart"`
}

func (p dcPage[T]) next(start int) (int, error) {
	if p.Values == nil || p.Last == nil {
		return 0, errors.New("SCM returned an incomplete page")
	}
	if *p.Last {
		return -1, nil
	}
	if p.Next == nil || *p.Next <= start {
		return 0, errors.New("SCM returned an invalid pagination cursor")
	}
	return *p.Next, nil
}

func (b *bitbucketDC) Identity(ctx context.Context) (Identity, error) {
	var repo dcRepo
	if err := b.http.do(ctx, "GET", b.path(), nil, &repo); err != nil {
		return Identity{}, err
	}
	if !repo.matches(b.c.Repository) || repo.Name == "" {
		return Identity{}, errors.New("SCM returned a different or invalid repository")
	}
	read, err := b.permission(ctx, repo, "REPO_READ")
	if err != nil {
		return Identity{}, err
	}
	if !read {
		return Identity{}, errors.New("the credential has no authenticated repository access")
	}
	write, err := b.permission(ctx, repo, "REPO_WRITE")
	if err != nil {
		return Identity{}, err
	}
	var branch struct {
		ID        string `json:"id"`
		DisplayID string `json:"displayId"`
	}
	if err = b.http.do(ctx, "GET", b.path()+"/branches/default", nil, &branch); err != nil {
		return Identity{}, err
	}
	if !strings.HasPrefix(branch.ID, "refs/heads/") || strings.TrimPrefix(branch.ID, "refs/heads/") != branch.DisplayID || !validBranch(branch.DisplayID) {
		return Identity{}, errors.New("SCM returned an invalid default branch")
	}
	return Identity{User: "authenticated repository credential", Repository: repo.Project.Key + "/" + repo.Slug, DefaultBranch: branch.DisplayID, CanPush: write}, nil
}
func (b *bitbucketDC) permission(ctx context.Context, repo dcRepo, permission string) (bool, error) {
	start := 0
	for page := 0; page < adapterPageLimit; page++ {
		q := url.Values{"permission": {permission}, "name": {repo.Name}, "limit": {"100"}, "start": {strconv.Itoa(start)}}
		var result dcPage[dcRepo]
		if err := b.http.do(ctx, "GET", "/rest/api/1.0/repos?"+q.Encode(), nil, &result); err != nil {
			return false, err
		}
		next, err := result.next(start)
		if err != nil {
			return false, err
		}
		for _, r := range result.Values {
			if r.ID == repo.ID && r.matches(b.c.Repository) {
				return true, nil
			}
		}
		if next < 0 {
			return false, nil
		}
		start = next
	}
	return false, errors.New("SCM repository permission search exceeded the page limit")
}

type dcRef struct {
	ID           string `json:"id"`
	DisplayID    string `json:"displayId"`
	LatestCommit string `json:"latestCommit"`
	Repository   dcRepo `json:"repository"`
}
type dcPull struct {
	ID      int    `json:"id"`
	State   string `json:"state"`
	Version int    `json:"version"`
	From    dcRef  `json:"fromRef"`
	To      dcRef  `json:"toRef"`
	Links   struct {
		Self []struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"links"`
}

func (p dcPull) matches(repository, head, base string) bool {
	return p.State == "OPEN" && strings.EqualFold(p.From.Repository.Project.Key+"/"+p.From.Repository.Slug, repository) && strings.EqualFold(p.To.Repository.Project.Key+"/"+p.To.Repository.Slug, repository) && p.From.ID == "refs/heads/"+head && p.To.ID == "refs/heads/"+base
}
func (p dcPull) pr(created bool) (*PullRequest, error) {
	if p.ID <= 0 || len(p.Links.Self) == 0 || !validPullRequestURL(p.Links.Self[0].Href) {
		return nil, errors.New("SCM pull request has no identity")
	}
	return &PullRequest{Number: p.ID, URL: p.Links.Self[0].Href, Head: strings.TrimPrefix(p.From.ID, "refs/heads/"), Base: strings.TrimPrefix(p.To.ID, "refs/heads/"), State: strings.ToLower(p.State), Created: created}, nil
}
func (b *bitbucketDC) FindPullRequest(ctx context.Context, head, base string) (*PullRequest, error) {
	start := 0
	for page := 0; page < adapterPageLimit; page++ {
		q := url.Values{"state": {"OPEN"}, "direction": {"OUTGOING"}, "at": {"refs/heads/" + head}, "limit": {"100"}, "start": {strconv.Itoa(start)}}
		var result dcPage[dcPull]
		if err := b.http.do(ctx, "GET", b.path()+"/pull-requests?"+q.Encode(), nil, &result); err != nil {
			return nil, err
		}
		next, err := result.next(start)
		if err != nil {
			return nil, err
		}
		for _, p := range result.Values {
			if p.matches(b.c.Repository, head, base) {
				return p.pr(false)
			}
		}
		if next < 0 {
			return nil, nil
		}
		start = next
	}
	return nil, errors.New("SCM pull request search exceeded the page limit; creation refused")
}
func (b *bitbucketDC) CreatePullRequest(ctx context.Context, in PullRequestInput) (*PullRequest, error) {
	project, slug, _ := strings.Cut(b.c.Repository, "/")
	ref := func(branch string) any {
		return map[string]any{"id": "refs/heads/" + branch, "repository": map[string]any{"slug": slug, "project": map[string]string{"key": project}}}
	}
	var p dcPull
	err := b.http.do(ctx, "POST", b.path()+"/pull-requests", map[string]any{"title": in.Title, "description": in.Body, "fromRef": ref(in.Head), "toRef": ref(in.Base)}, &p)
	if err != nil {
		existing, lookupErr := b.FindPullRequest(ctx, in.Head, in.Base)
		if lookupErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	if !p.matches(b.c.Repository, in.Head, in.Base) {
		return nil, errors.New("SCM returned a mismatched or closed pull request")
	}
	return p.pr(true)
}
func (b *bitbucketDC) Comment(ctx context.Context, number int, body string) error {
	if number <= 0 || strings.TrimSpace(body) == "" || len(body) > MaxBody {
		return errors.New("invalid pull request comment")
	}
	return b.http.do(ctx, "POST", fmt.Sprintf("%s/pull-requests/%d/comments", b.path(), number), map[string]string{"text": body}, nil)
}
