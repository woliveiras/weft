package jiradc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fixtureIssue = `{"id":"10007","key":"APP-7","fields":{"summary":"Fix parser","description":"Expected result","project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Review","statusCategory":{"key":"indeterminate"}},"labels":["weft:todo"],"assignee":null}}`
const fixtureComment = `{"id":"10009","body":"Review summary","author":{"key":"bot-id","name":"bot"},"created":"2026-11-20T10:00:00.000+0000"}`

func fixtureClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	c, err := New(s.URL+"/jira", "10001", "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.http = s.Client()
	// Synthetic fixture represents an explicitly project-restricted remote account.
	c.writesEnabled = true
	return c, s
}
func TestDataCenterReadSearchAndCommentsOverTLS(t *testing.T) {
	posts := 0
	gets := 0
	c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		gets++
		if r.Header.Get("Authorization") != "Bearer fixture-secret" || r.Header.Get("Accept") != "application/json" {
			t.Error("credential/accept")
		}
		switch r.URL.Path {
		case "/jira/rest/api/2/myself":
			fmt.Fprint(w, `{"key":"bot-id","name":"bot"}`)
		case "/jira/rest/api/2/issue/APP-7":
			fmt.Fprint(w, fixtureIssue)
		case "/jira/rest/api/2/search":
			if r.URL.Query().Get("jql") != `project = 10001 AND (labels = "weft:todo") ORDER BY created ASC` || r.URL.Query().Get("maxResults") != "50" {
				t.Error("search scope")
			}
			fmt.Fprintf(w, `{"startAt":0,"maxResults":50,"total":1,"issues":[%s]}`, fixtureIssue)
		case "/jira/rest/api/2/issue/10007/comment":
			if r.Method == "POST" {
				posts++
				var in struct{ Body string }
				if json.NewDecoder(r.Body).Decode(&in) != nil || in.Body != "Review summary" {
					t.Error("comment input")
				}
				w.WriteHeader(201)
				fmt.Fprint(w, fixtureComment)
				return
			}
			if r.URL.Query().Get("startAt") == "0" {
				fmt.Fprintf(w, `{"startAt":0,"maxResults":1,"total":2,"comments":[%s]}`, fixtureComment)
			} else if r.URL.Query().Get("startAt") == "1" {
				fmt.Fprintf(w, `{"startAt":1,"maxResults":1,"total":2,"comments":[%s]}`, strings.Replace(fixtureComment, "10009", "10010", 1))
			} else {
				t.Error("wrong cursor")
			}
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	actor, err := c.Identity(t.Context())
	if err != nil || actor.Key != "bot-id" {
		t.Fatal(actor, err)
	}
	issue, err := c.Issue(t.Context(), "APP-7")
	if err != nil || issue.Fields.Status.Name != "Review" || !strings.HasSuffix(c.IssueURL(issue), "/jira/browse/APP-7") {
		t.Fatal(issue, err)
	}
	page, next, err := c.Search(t.Context(), `labels = "weft:todo"`, 0)
	if err != nil || len(page.Issues) != 1 || next != -1 {
		t.Fatal(page, next, err)
	}
	comments, err := c.Comments(t.Context(), "APP-7")
	if err != nil || len(comments) != 2 || comments[1].ID != "10010" {
		t.Fatal(comments, err)
	}
	out, err := c.AddComment(t.Context(), "APP-7", "Review summary")
	if err != nil || out.ID != "10009" || posts != 1 {
		t.Fatal(out, err, posts)
	}
	before := gets
	if _, err = c.Issue(t.Context(), "../APP-7"); err == nil {
		t.Fatal("invalid key accepted")
	}
	if _, _, err = c.Search(t.Context(), "project=x ORDER BY key", 0); err == nil {
		t.Fatal("unbounded search accepted")
	}
	if _, err = c.AddComment(t.Context(), "APP-7", "fixture-secret"); err == nil {
		t.Fatal("credential accepted as comment")
	}
	if before != gets {
		t.Fatal("invalid inputs touched remote")
	}
}
func TestDataCenterBoundaryFailures(t *testing.T) {
	for _, mode := range []string{"401", "403", "404", "429", "500", "redirect", "invalid", "oversize", "foreign-project", "wrong-key", "unknown-status", "reflected", "encoded-reflected", "network"} {
		t.Run(mode, func(t *testing.T) {
			targetCalls := 0
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
			defer target.Close()
			c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "401", "403", "404", "429", "500":
					var status int
					fmt.Sscan(mode, &status)
					w.Header().Set("Retry-After", "99999")
					w.WriteHeader(status)
					fmt.Fprint(w, "fixture-secret")
					return
				case "redirect":
					w.Header().Set("Location", target.URL)
					w.WriteHeader(302)
					return
				case "invalid":
					fmt.Fprint(w, `{`)
					return
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", bodyLimit+1))
					return
				case "network":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				body := fixtureIssue
				switch mode {
				case "foreign-project":
					body = strings.Replace(body, `"project":{"id":"10001"}`, `"project":{"id":"10099"}`, 1)
				case "wrong-key":
					body = strings.Replace(body, "APP-7", "APP-8", 1)
				case "unknown-status":
					body = strings.Replace(body, "indeterminate", "unknown", 1)
				case "reflected":
					body = strings.Replace(body, "Expected result", "fixture-secret", 1)
				case "encoded-reflected":
					body = strings.Replace(body, "Expected result", "fixture%2Dsecret", 1)
				}
				fmt.Fprint(w, body)
			})
			issue, err := c.Issue(t.Context(), "APP-7")
			if err == nil || issue.Key != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal(issue, err)
			}
			if mode == "429" {
				var e *HTTPError
				if !errors.As(err, &e) || e.RetryAfter != time.Hour {
					t.Fatal(err)
				}
			}
			if targetCalls != 0 {
				t.Fatal("redirect followed")
			}
		})
	}
}
func TestDataCenterWriteIsNotRetriedOnUnknownOutcome(t *testing.T) {
	posts := 0
	c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, fixtureIssue)
			return
		}
		posts++
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	})
	if _, err := c.AddComment(t.Context(), "APP-7", "Review summary"); err == nil || posts != 1 {
		t.Fatal(err, posts)
	}
}
func TestDataCenterForeignProjectRefusesWrites(t *testing.T) {
	posts := 0
	c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			return
		}
		fmt.Fprint(w, strings.Replace(fixtureIssue, `"project":{"id":"10001"}`, `"project":{"id":"10099"}`, 1))
	})
	if _, err := c.AddComment(t.Context(), "APP-7", "Review summary"); err == nil || posts != 0 {
		t.Fatal(err, posts)
	}
}

func TestDataCenterCommentHistoryFailsWithoutPartialEvidence(t *testing.T) {
	for _, mode := range []string{"missing", "wrong-cursor", "no-progress", "duplicate", "page-limit"} {
		t.Run(mode, func(t *testing.T) {
			pages := 0
			c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/APP-7") {
					fmt.Fprint(w, fixtureIssue)
					return
				}
				pages++
				switch mode {
				case "missing":
					fmt.Fprint(w, `{"comments":[]}`)
				case "wrong-cursor":
					fmt.Fprint(w, `{"startAt":99,"maxResults":100,"total":100,"comments":[]}`)
				case "no-progress":
					fmt.Fprint(w, `{"startAt":0,"maxResults":100,"total":1,"comments":[]}`)
				case "duplicate":
					fmt.Fprintf(w, `{"startAt":%d,"maxResults":1,"total":3,"comments":[%s]}`, pages-1, fixtureComment)
				case "page-limit":
					comments := make([]string, 100)
					for i := range comments {
						comments[i] = strings.Replace(fixtureComment, "10009", fmt.Sprint(pages*100+i), 1)
					}
					fmt.Fprintf(w, `{"startAt":%d,"maxResults":100,"total":1001,"comments":[%s]}`, (pages-1)*100, strings.Join(comments, ","))
				}
			})
			out, err := c.Comments(t.Context(), "APP-7")
			if err == nil || out != nil || pages > 10 {
				t.Fatal(len(out), err, pages)
			}
		})
	}
}
func TestDataCenterRequiresTrustedTLS(t *testing.T) {
	c, s := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, fixtureIssue) })
	c.http = &http.Client{}
	if _, err := c.Issue(t.Context(), "APP-7"); err == nil {
		t.Fatal("untrusted TLS certificate accepted", s.URL)
	}
}
