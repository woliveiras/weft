// Package jiradc implements the native Jira Data Center REST 2 boundary.
// It is independent of MCP credentials and SCM connections. Callers own grants,
// connection snapshots and durable write reconciliation; this client never retries
// a write or follows redirects. Live version qualification is separate.
package jiradc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const bodyLimit = 1 << 20

var ErrWriteDisabled = errors.New("native Jira writes require a confirmed project-restricted technical account")
var ErrHistoryLimit = errors.New("Jira comments exceed the page bound")

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var issueKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}-[1-9][0-9]{0,9}$`)

// Client is bound to one reviewed site and immutable numeric project ID.
// Its token belongs to this native connection, never to an MCP connection.
type Client struct {
	site          string
	project       string
	token         string
	http          *http.Client
	writesEnabled bool
	writeCheck    func(context.Context) error
}

// ValidateScope applies the same site/project contract to connections, trigger
// snapshots and trusted task origins without constructing a credentialed client.
func ValidateScope(site, project string) error {
	_, err := parseScope(site, project)
	return err
}

func parseScope(site, project string) (string, error) {
	u, err := url.Parse(strings.TrimRight(site, "/"))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || strings.Contains(u.Path, "/rest/") {
		return "", errors.New("Jira Data Center requires an HTTPS site URL without credentials, query, fragment or REST path")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("invalid Jira site context path")
		}
	}
	if !numericID.MatchString(project) {
		return "", errors.New("select a numeric Jira project ID")
	}
	return u.String(), nil
}

func New(site, project, token string) (*Client, error) {
	canonical, err := parseScope(site, project)
	if err != nil {
		return nil, err
	}
	if token == "" || len(token) > 4096 || strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("a single-line native Jira personal access token is required")
	}
	return &Client{site: canonical, project: project, token: token, http: &http.Client{Timeout: 10 * time.Second}}, nil
}

// HTTPError retains status and bounded retry metadata, never response content.
type HTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("Jira Data Center returned HTTP %d", e.Status) }
func (e *HTTPError) TrackerRateLimit() (bool, time.Duration, time.Time) {
	return e.Status == 429, e.RetryAfter, time.Time{}
}

func (c *Client) authorizeWrite(ctx context.Context) error {
	if !c.writesEnabled {
		return ErrWriteDisabled
	}
	if c.writeCheck != nil {
		return c.writeCheck(ctx)
	}
	return nil
}
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	if method != http.MethodGet {
		if err := c.authorizeWrite(ctx); err != nil {
			return err
		}
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return errors.New("invalid Jira request body")
		}
		if len(raw) > bodyLimit {
			return errors.New("Jira request exceeds size limit")
		}
		body = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.site+"/rest/api/2"+path, body)
	if err != nil {
		return errors.New("invalid Jira request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return errors.New("Jira Data Center request failed; write outcome may be unknown")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		e := &HTTPError{Status: res.StatusCode}
		if res.StatusCode == 429 {
			if n, err := strconv.ParseUint(res.Header.Get("Retry-After"), 10, 32); err == nil {
				e.RetryAfter = time.Duration(min(n, 3600)) * time.Second
			}
		}
		return e
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, bodyLimit+1))
	if err != nil || len(raw) > bodyLimit {
		return errors.New("Jira response is incomplete or exceeds size limit")
	}
	if len(raw) == 0 {
		if out != nil {
			return errors.New("Jira response is empty")
		}
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || containsSecret(value, c.token, 0) {
		return errors.New("Jira response is invalid or contains protected credentials")
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return errors.New("unexpected Jira response shape")
	}
	return nil
}
func containsSecret(value any, token string, depth int) bool {
	if depth > 64 {
		return true
	}
	switch v := value.(type) {
	case string:
		decoded, err := url.QueryUnescape(v)
		return strings.Contains(v, token) || (err == nil && strings.Contains(decoded, token))
	case []any:
		for _, item := range v {
			if containsSecret(item, token, depth+1) {
				return true
			}
		}
	case map[string]any:
		for k, item := range v {
			if containsSecret(k, token, depth+1) || containsSecret(item, token, depth+1) {
				return true
			}
		}
	}
	return false
}

// Actor keeps the provider's stable key distinct from the account login used in
// assignment requests. Names are presentation data, never authorization.
type Actor struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

func (c *Client) Identity(ctx context.Context) (Actor, error) {
	var a Actor
	err := c.do(ctx, "GET", "/myself", nil, &a)
	if err == nil && (a.Key == "" || a.Name == "") {
		err = errors.New("Jira identity is incomplete")
	}
	if err != nil {
		return Actor{}, err
	}
	return a, nil
}

type Issue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary     string   `json:"summary"`
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
		Project     struct {
			ID string `json:"id"`
		} `json:"project"`
		IssueType struct {
			ID string `json:"id"`
		} `json:"issuetype"`
		Status struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Assignee   *Actor      `json:"assignee"`
		Reporter   *Actor      `json:"reporter"`
		IssueLinks []IssueLink `json:"issuelinks"`
		Updated    string      `json:"updated"`
		Created    string      `json:"created"`
	} `json:"fields"`
}

type IssueLink struct {
	Type struct {
		Inward string `json:"inward"`
	} `json:"type"`
	InwardIssue *struct {
		Fields struct {
			Status struct {
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"status"`
		} `json:"fields"`
	} `json:"inwardIssue"`
}

func (c *Client) validateIssue(i Issue, key string) error {
	if !numericID.MatchString(i.ID) || !issueKey.MatchString(i.Key) || (key != "" && i.Key != key) || i.Fields.Project.ID != c.project || !numericID.MatchString(i.Fields.Status.ID) || !numericID.MatchString(i.Fields.IssueType.ID) {
		return errors.New("Jira issue is incomplete or outside the reviewed project")
	}
	if i.Fields.IssueLinks == nil || len(i.Fields.IssueLinks) > 1000 {
		return errors.New("Jira issue link metadata is unavailable or exceeds the bound")
	}
	switch i.Fields.Status.StatusCategory.Key {
	case "new", "indeterminate", "done":
	default:
		return errors.New("unknown Jira status category")
	}
	return nil
}
func (c *Client) Issue(ctx context.Context, key string) (Issue, error) {
	var i Issue
	if !issueKey.MatchString(key) {
		return Issue{}, errors.New("invalid Jira issue key")
	}
	err := c.do(ctx, "GET", "/issue/"+key+"?fields=summary,description,labels,project,issuetype,status,assignee,reporter,updated,created,issuelinks", nil, &i)
	if err == nil {
		err = c.validateIssue(i, key)
	}
	if err != nil {
		return Issue{}, err
	}
	return i, nil
}
func (c *Client) IssueURL(i Issue) string { return c.site + "/browse/" + i.Key }

type SearchPage struct {
	Issues     []Issue `json:"issues"`
	StartAt    *int    `json:"startAt"`
	MaxResults *int    `json:"maxResults"`
	Total      *int    `json:"total"`
}

// Search returns one bounded page. Callers must not infer absence or completeness
// when Next is nonnegative. Scope is checked again on every returned issue.
func (c *Client) Search(ctx context.Context, query string, start int) (SearchPage, int, error) {
	var p SearchPage
	if len(query) > 1000 || strings.TrimSpace(query) == "" || strings.ContainsAny(query, "\r\n") || strings.Contains(strings.ToUpper(query), "ORDER BY") || start < 0 || start > 500 {
		return SearchPage{}, -1, errors.New("invalid bounded Jira search")
	}
	q := url.Values{"jql": {"project = " + c.project + " AND (" + query + ") ORDER BY created ASC"}, "startAt": {strconv.Itoa(start)}, "maxResults": {"50"}, "fields": {"summary,description,labels,project,issuetype,status,assignee,reporter,updated,created,issuelinks"}}
	if err := c.do(ctx, "GET", "/search?"+q.Encode(), nil, &p); err != nil {
		return SearchPage{}, -1, err
	}
	next, err := p.next(start)
	if err != nil {
		return SearchPage{}, -1, err
	}
	seen := map[string]bool{}
	for _, i := range p.Issues {
		if seen[i.ID] {
			return SearchPage{}, -1, errors.New("duplicate Jira search identity")
		}
		seen[i.ID] = true
		if err := c.validateIssue(i, ""); err != nil {
			return SearchPage{}, -1, err
		}
	}
	return p, next, nil
}
func (p SearchPage) next(start int) (int, error) {
	if p.Issues == nil || p.StartAt == nil || p.MaxResults == nil || p.Total == nil || *p.StartAt != start || *p.MaxResults < 1 || *p.MaxResults > 50 || len(p.Issues) > *p.MaxResults || *p.Total < start+len(p.Issues) {
		return -1, errors.New("incomplete Jira search page")
	}
	next := start + len(p.Issues)
	if next >= *p.Total {
		return -1, nil
	}
	if len(p.Issues) == 0 || next > 500 {
		return -1, errors.New("Jira search exceeds the page bound")
	}
	return next, nil
}

type Comment struct {
	ID      string `json:"id"`
	Body    string `json:"body"`
	Author  Actor  `json:"author"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

func (c *Client) Comments(ctx context.Context, key string) ([]Comment, error) {
	issue, err := c.Issue(ctx, key)
	if err != nil {
		return nil, err
	}
	out := []Comment{}
	seen := map[string]bool{}
	start := 0
	for page := 0; page < 10; page++ {
		var p struct {
			Comments   []Comment `json:"comments"`
			StartAt    *int      `json:"startAt"`
			MaxResults *int      `json:"maxResults"`
			Total      *int      `json:"total"`
		}
		if err := c.do(ctx, "GET", "/issue/"+issue.ID+"/comment?startAt="+strconv.Itoa(start)+"&maxResults=100", nil, &p); err != nil {
			return nil, err
		}
		if p.Comments == nil || p.StartAt == nil || *p.StartAt != start || p.MaxResults == nil || *p.MaxResults < 1 || *p.MaxResults > 100 || len(p.Comments) > *p.MaxResults || p.Total == nil || *p.Total < start+len(p.Comments) {
			return nil, errors.New("incomplete Jira comment page")
		}
		for _, v := range p.Comments {
			if seen[v.ID] {
				return nil, errors.New("Jira comment history changed during pagination")
			}
			seen[v.ID] = true
			if !numericID.MatchString(v.ID) || v.Author.Key == "" {
				return nil, errors.New("invalid Jira comment identity")
			}
		}
		out = append(out, p.Comments...)
		start += len(p.Comments)
		if start >= *p.Total {
			return out, nil
		}
		if len(p.Comments) == 0 {
			return nil, errors.New("Jira comment pagination made no progress")
		}
	}
	return nil, ErrHistoryLimit
}

// AddComment targets the resolved numeric issue ID and performs one mutation only.
// Project membership is checked before dispatch, not atomically with the remote
// mutation. Adoption requires a remotely project-restricted principal; this
// client cannot turn a broad PAT into a project-scoped credential. A timeout is not evidence that no comment
// was written; the caller must reserve and reconcile its durable marker.
func (c *Client) AddComment(ctx context.Context, key, body string) (Comment, error) {
	var out Comment
	if err := c.authorizeWrite(ctx); err != nil {
		return out, err
	}
	if strings.TrimSpace(body) == "" || len(body) > 16000 || strings.Contains(body, c.token) {
		return out, errors.New("invalid Jira comment body")
	}
	issue, err := c.Issue(ctx, key)
	if err != nil {
		return out, err
	}
	if err := c.do(ctx, "POST", "/issue/"+issue.ID+"/comment", map[string]string{"body": body}, &out); err != nil {
		return out, err
	}
	if !numericID.MatchString(out.ID) || out.Author.Key == "" {
		return Comment{}, errors.New("Jira did not confirm a comment identity")
	}
	return out, nil
}
