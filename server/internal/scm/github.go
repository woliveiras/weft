package scm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub over the REST API with a fine-grained token (contents: write,
// pull requests: write) or an OAuth token.
type github struct {
	c      Connection
	token  string
	api    string
	client *http.Client
}

func init() {
	RegisterKind("github", func(c Connection, token string) (Provider, error) {
		api := strings.TrimRight(c.APIURL, "/")
		if api == "" {
			api = "https://api.github.com"
		}
		return &github{c: c, token: token, api: api, client: &http.Client{Timeout: 30 * time.Second}}, nil
	})
}

func (g *github) RemoteURL() string {
	if g.c.GitURL != "" {
		return g.c.GitURL
	}
	return "https://github.com/" + g.c.Repository + ".git"
}

// GitAuthHeader: GitHub accepts a token as the password of x-access-token.
func (g *github) GitAuthHeader() string { return BasicAuth("x-access-token", g.token) }

// apiError is a GitHub API failure with an actionable message.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	hint := ""
	switch e.Status {
	case http.StatusUnauthorized:
		hint = " (the token is invalid or expired)"
	case http.StatusForbidden:
		hint = " (the token lacks a permission, or an organization policy blocks it)"
	case http.StatusNotFound:
		hint = " (the repository does not exist or the token cannot see it)"
	}
	return fmt.Sprintf("GitHub API: HTTP %d: %s%s", e.Status, e.Message, hint)
}

func (g *github) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.api+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Message
		for _, x := range e.Errors {
			msg += "; " + x.Message
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return &apiError{Status: resp.StatusCode, Message: redactToken(msg, g.token)}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("GitHub API: unexpected response: %w", err)
		}
	}
	return nil
}

func redactToken(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "[redacted]")
}

func (g *github) Identity(ctx context.Context) (Identity, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := g.do(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return Identity{}, err
	}
	var repo struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Permissions   struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if err := g.do(ctx, http.MethodGet, "/repos/"+g.c.Repository, nil, &repo); err != nil {
		return Identity{}, err
	}
	return Identity{User: user.Login, Repository: repo.FullName, DefaultBranch: repo.DefaultBranch, CanPush: repo.Permissions.Push}, nil
}

type ghPull struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p ghPull) pr(created bool) *PullRequest {
	return &PullRequest{Number: p.Number, URL: p.HTMLURL, Head: p.Head.Ref, Base: p.Base.Ref, State: p.State, Created: created}
}

func (g *github) owner() string {
	owner, _, _ := strings.Cut(g.c.Repository, "/")
	return owner
}

func (g *github) FindPullRequest(ctx context.Context, head, base string) (*PullRequest, error) {
	q := url.Values{"state": {"open"}, "head": {g.owner() + ":" + head}, "base": {base}}
	var pulls []ghPull
	if err := g.do(ctx, http.MethodGet, "/repos/"+g.c.Repository+"/pulls?"+q.Encode(), nil, &pulls); err != nil {
		return nil, err
	}
	for _, p := range pulls {
		if p.Head.Ref == head && p.Base.Ref == base {
			return p.pr(false), nil
		}
	}
	return nil, nil
}

func (g *github) CreatePullRequest(ctx context.Context, in PullRequestInput) (*PullRequest, error) {
	var p ghPull
	err := g.do(ctx, http.MethodPost, "/repos/"+g.c.Repository+"/pulls", map[string]string{"title": in.Title, "body": in.Body, "head": in.Head, "base": in.Base}, &p)
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusUnprocessableEntity && strings.Contains(ae.Message, "already exists") {
		// Opened concurrently: return that one.
		if existing, ferr := g.FindPullRequest(ctx, in.Head, in.Base); ferr == nil && existing != nil {
			return existing, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return p.pr(true), nil
}

func (g *github) Comment(ctx context.Context, number int, body string) error {
	return g.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", g.c.Repository, number), map[string]string{"body": body}, nil)
}
