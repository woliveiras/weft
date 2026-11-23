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
	"strconv"
	"strings"
	"time"
)

const adapterPageLimit = 10
const adapterBodyLimit = 2 << 20

// providerHTTP confines pagination and credentials to the reviewed API origin.
// Redirects are refused even on the same host: a redirect is not an API result.
type providerHTTP struct {
	base          *url.URL
	token         string
	authorization string
	client        *http.Client
	headers       http.Header
}

func newProviderHTTP(raw, token string) (*providerHTTP, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return nil, errors.New("SCM API URL must use HTTPS without credentials, query or fragment (HTTP is allowed only for loopback fixtures)")
	}
	return &providerHTTP{base: u, token: token, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (h *providerHTTP) do(ctx context.Context, method, path string, in, out any) error {
	return h.doMedia(ctx, method, path, in, out, "application/json")
}
func (h *providerHTTP) doMedia(ctx context.Context, method, path string, in, out any, accept string) error {
	u, err := url.Parse(path)
	if err != nil {
		return errors.New("invalid SCM API path")
	}
	if !u.IsAbs() {
		u, err = url.Parse(strings.TrimRight(h.base.String(), "/") + "/" + strings.TrimLeft(path, "/"))
		if err != nil {
			return errors.New("invalid SCM API path")
		}
	}
	if u.Scheme != h.base.Scheme || u.Host != h.base.Host || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, strings.TrimRight(h.base.Path, "/")+"/") {
		return errors.New("SCM pagination escaped the configured API")
	}
	var body io.Reader
	if in != nil {
		b, e := json.Marshal(in)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return errors.New("invalid SCM request")
	}
	for key, values := range h.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	authorization := h.authorization
	if authorization == "" {
		authorization = "Bearer " + h.token
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Accept", accept)
	for key, values := range h.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := h.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("SCM API could not be reached; verify its URL, TLS trust and availability")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &providerHTTPError{Status: res.StatusCode, RetryAfter: res.Header.Get("Retry-After")}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, adapterBodyLimit+1))
	if err != nil {
		return fmt.Errorf("read SCM response: %w", err)
	}
	if len(raw) > adapterBodyLimit {
		return errors.New("SCM response exceeded the size limit")
	}
	redact := func(value string) string {
		if h.authorization == "" {
			return value
		}
		value = redactAuthMaterial(value, h.token, h.authorization)
		// Git uses a separate documented username with the same API token.
		return redactAuthMaterial(value, "", BasicAuth("x-bitbucket-api-token-auth", h.token))
	}
	if text, ok := out.(*string); ok {
		*text = redact(string(raw))
		return nil
	}
	if out != nil {
		if h.authorization != "" {
			// Normalize JSON strings before redacting: equivalent Unicode or slash
			// escapes must not restore credential bytes after the security boundary.
			var decoded any
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&decoded); err != nil {
				return errors.New("SCM API returned an invalid JSON response")
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				return errors.New("SCM API returned an invalid JSON response")
			}
			decoded = redactJSONStrings(decoded, redact)
			raw, err = json.Marshal(decoded)
			if err != nil {
				return errors.New("SCM API returned an invalid JSON response")
			}
		}
		if err = json.Unmarshal(raw, out); err != nil {
			return errors.New("SCM API returned an invalid JSON response")
		}
	}
	return nil
}

// nextPage accepts only a query change on the same resource collection.
func (h *providerHTTP) nextPage(current, next string) (string, error) {
	resolve := func(raw string) (*url.URL, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		if !u.IsAbs() {
			return url.Parse(strings.TrimRight(h.base.String(), "/") + "/" + strings.TrimLeft(raw, "/"))
		}
		return u, nil
	}
	from, err := resolve(current)
	if err != nil {
		return "", errors.New("invalid SCM pagination")
	}
	to, err := resolve(next)
	if err != nil || to.Scheme != from.Scheme || to.Host != from.Host || to.Path != from.Path || to.User != nil || to.Fragment != "" {
		return "", errors.New("SCM pagination left the reviewed resource collection")
	}
	return to.String(), nil
}

type providerHTTPError struct {
	Status     int
	RetryAfter string
}

func (e *providerHTTPError) Error() string {
	return fmt.Sprintf("SCM API: HTTP %d (%s); verify credential permissions and service availability", e.Status, http.StatusText(e.Status))
}

func init() {
	RegisterKind("bitbucket-cloud", func(c Connection, token string) (Provider, error) {
		if issues := validateAuth(c.Kind, c.AuthType, c.Username); len(issues) != 0 {
			return nil, &ValidationError{Errors: issues}
		}
		if c.AuthType == AuthAPIToken && !validAPIToken(token) {
			return nil, &ValidationError{Errors: []Issue{{"token", "enter an API token without control characters"}}}
		}
		base := c.APIURL
		if base == "" {
			base = "https://api.bitbucket.org/2.0"
		}
		h, err := newProviderHTTP(base, token)
		if err != nil {
			return nil, err
		}
		if c.AuthType == AuthAPIToken {
			h.authorization = BasicAuth(c.Username, token)
		}
		return &bitbucketCloud{c: c, http: h}, nil
	})
}

type bitbucketCloud struct {
	c    Connection
	http *providerHTTP
}

func (b *bitbucketCloud) RemoteURL() string {
	if b.c.GitURL != "" {
		return b.c.GitURL
	}
	return "https://bitbucket.org/" + b.c.Repository + ".git"
}
func (b *bitbucketCloud) GitAuthHeader() string {
	if b.c.AuthType == AuthAPIToken {
		return BasicAuth("x-bitbucket-api-token-auth", b.http.token)
	}
	return BasicAuth("x-token-auth", b.http.token)
}
func (b *bitbucketCloud) path() string { return "/repositories/" + b.c.Repository }
func (b *bitbucketCloud) Identity(ctx context.Context) (Identity, error) {
	var repo struct {
		FullName   string `json:"full_name"`
		MainBranch struct {
			Name string `json:"name"`
		} `json:"mainbranch"`
	}
	if err := b.http.do(ctx, "GET", b.path(), nil, &repo); err != nil {
		return Identity{}, err
	}
	if !strings.EqualFold(repo.FullName, b.c.Repository) {
		return Identity{}, errors.New("SCM returned a different repository")
	}
	workspace, _, _ := strings.Cut(b.c.Repository, "/")
	path := "/user/workspaces/" + workspace + "/permissions/repositories?" + url.Values{"q": {`repository.full_name="` + b.c.Repository + `"`}, "pagelen": {"100"}}.Encode()
	for page := 0; page < adapterPageLimit; page++ {
		var result struct {
			Next   string `json:"next"`
			Values []struct {
				Repository struct {
					FullName string `json:"full_name"`
				} `json:"repository"`
				User struct {
					Nickname string `json:"nickname"`
					UUID     string `json:"uuid"`
				} `json:"user"`
				Permission string `json:"permission"`
			} `json:"values"`
		}
		if err := b.http.do(ctx, "GET", path, nil, &result); err != nil {
			return Identity{}, err
		}
		if result.Values == nil {
			return Identity{}, errors.New("SCM permission response has no values collection")
		}
		for _, row := range result.Values {
			if strings.EqualFold(row.Repository.FullName, b.c.Repository) {
				user := row.User.Nickname
				if user == "" {
					user = row.User.UUID
				}
				if user == "" {
					return Identity{}, errors.New("SCM permission response has no authenticated identity")
				}
				return Identity{User: user, Repository: repo.FullName, DefaultBranch: repo.MainBranch.Name, CanPush: row.Permission == "write" || row.Permission == "admin"}, nil
			}
		}
		if result.Next == "" {
			return Identity{}, errors.New("the credential has no explicit access to the configured repository")
		}
		var err error
		path, err = b.http.nextPage(path, result.Next)
		if err != nil {
			return Identity{}, err
		}
	}
	return Identity{}, errors.New("SCM permission search exceeded the page limit")
}

type bbCloudRef struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Commit struct {
		Hash string `json:"hash"`
	} `json:"commit"`
}
type bbCloudPull struct {
	ID          int        `json:"id"`
	State       string     `json:"state"`
	Source      bbCloudRef `json:"source"`
	Destination bbCloudRef `json:"destination"`
	Links       struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

func (p bbCloudPull) pr(created bool) *PullRequest {
	return &PullRequest{HeadSHA: p.Source.Commit.Hash, BaseSHA: p.Destination.Commit.Hash, Number: p.ID, URL: p.Links.HTML.Href, State: strings.ToLower(p.State), Head: p.Source.Branch.Name, Base: p.Destination.Branch.Name, Created: created}
}
func (b *bitbucketCloud) FindPullRequest(ctx context.Context, head, base string) (*PullRequest, error) {
	// Pull requests cap pagelen at 50, below Bitbucket's global maximum of 100.
	q := url.Values{"state": {"OPEN"}, "q": {`source.branch.name=` + strconv.Quote(head) + ` AND destination.branch.name=` + strconv.Quote(base)}, "pagelen": {"50"}}
	path := b.path() + "/pullrequests?" + q.Encode()
	for page := 0; page < adapterPageLimit; page++ {
		var result struct {
			Next   string        `json:"next"`
			Values []bbCloudPull `json:"values"`
		}
		if err := b.http.do(ctx, "GET", path, nil, &result); err != nil {
			return nil, err
		}
		if result.Values == nil {
			return nil, errors.New("SCM pull request response has no values collection")
		}
		for _, p := range result.Values {
			if p.State == "OPEN" && p.Source.Branch.Name == head && p.Destination.Branch.Name == base && strings.EqualFold(p.Source.Repository.FullName, b.c.Repository) && strings.EqualFold(p.Destination.Repository.FullName, b.c.Repository) {
				if p.ID <= 0 || !validPullRequestURL(p.Links.HTML.Href) {
					return nil, errors.New("SCM pull request has no identity")
				}
				return b.deliveredPull(ctx, p, false), nil
			}
		}
		if result.Next == "" {
			return nil, nil
		}
		var err error
		path, err = b.http.nextPage(path, result.Next)
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("SCM pull request search exceeded the page limit; creation refused")
}
func (b *bitbucketCloud) CreatePullRequest(ctx context.Context, in PullRequestInput) (*PullRequest, error) {
	if containsCredentialMaterial(b.c, b.http.token, in.Title+"\n"+in.Body) {
		return nil, errors.New("pull request contains protected credential material")
	}
	var p bbCloudPull
	ref := func(branch string) any {
		return map[string]any{"branch": map[string]string{"name": branch}, "repository": map[string]string{"full_name": b.c.Repository}}
	}
	err := b.http.do(ctx, "POST", b.path()+"/pullrequests", map[string]any{"title": in.Title, "description": in.Body, "source": ref(in.Head), "destination": ref(in.Base), "close_source_branch": false}, &p)
	// Do not replay an uncertain POST. Reconcile against the same branch pair.
	if err != nil {
		existing, lookupErr := b.FindPullRequest(ctx, in.Head, in.Base)
		if lookupErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	if p.State != "OPEN" || p.ID <= 0 || !validPullRequestURL(p.Links.HTML.Href) || p.Source.Branch.Name != in.Head || p.Destination.Branch.Name != in.Base || !strings.EqualFold(p.Source.Repository.FullName, b.c.Repository) || !strings.EqualFold(p.Destination.Repository.FullName, b.c.Repository) {
		return nil, errors.New("SCM returned an invalid or mismatched pull request")
	}
	return b.deliveredPull(ctx, p, true), nil
}
func (b *bitbucketCloud) Comment(ctx context.Context, number int, body string) error {
	if number <= 0 || strings.TrimSpace(body) == "" || len(body) > MaxBody {
		return errors.New("invalid pull request comment")
	}
	if containsCredentialMaterial(b.c, b.http.token, body) {
		return errors.New("pull request comment contains protected credential material")
	}
	return b.http.do(ctx, "POST", fmt.Sprintf("%s/pullrequests/%d/comments", b.path(), number), map[string]any{"content": map[string]string{"raw": body}}, nil)
}

func validBitbucketRepository(repository string) bool {
	parts := strings.Split(repository, "/")
	return len(parts) == 2 && parts[0] != "." && parts[0] != ".." && parts[1] != "." && parts[1] != ".."
}
func bitbucketGitURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "use a Git URL without credentials, query or fragment"
	}
	if u.Scheme == "file" && u.Host == "" && strings.HasPrefix(u.Path, "/") {
		return ""
	}
	if u.Scheme == "https" && u.Host != "" {
		return ""
	}
	if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		return ""
	}
	return "use HTTPS for Git (HTTP is limited to loopback and file URLs to local fixtures)"
}

// Links are displayed in the console; never accept executable/credential URLs.
func validPullRequestURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")))
}
