package scm_test

import (
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/scm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBitbucketDataCenterDelivery(t *testing.T) {
	var readOnly atomic.Bool
	var denyRead atomic.Bool
	created := false
	posts := 0
	pull := `{"id":9,"state":"OPEN","fromRef":{"id":"refs/heads/weft/fix","displayId":"weft/fix","latestCommit":"abcdef","repository":{"slug":"app","project":{"key":"ACME"}}},"toRef":{"id":"refs/heads/main","displayId":"main","repository":{"slug":"app","project":{"key":"ACME"}}},"links":{"self":[{"href":"https://git.example/projects/ACME/repos/app/pull-requests/9"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "denied", 401)
			return
		}
		switch r.URL.Path {
		case "/context/rest/api/1.0/projects/ACME/repos/app":
			fmt.Fprint(w, `{"id":42,"slug":"app","name":"Application","project":{"key":"ACME"}}`)
		case "/context/rest/api/1.0/projects/ACME/repos/app/branches/default":
			fmt.Fprint(w, `{"id":"refs/heads/main","displayId":"main"}`)
		case "/context/rest/api/1.0/repos":
			if denyRead.Load() {
				fmt.Fprint(w, `{"isLastPage":true,"values":[{"id":42,"slug":"app","name":"Application","project":{"key":"OTHER"}}]}`)
				return
			}
			if readOnly.Load() && r.URL.Query().Get("permission") == "REPO_WRITE" {
				fmt.Fprint(w, `{"isLastPage":true,"values":[]}`)
				return
			}
			if r.URL.Query().Get("permission") == "" || r.URL.Query().Get("name") != "Application" {
				t.Error("missing permission/name")
			}
			fmt.Fprint(w, `{"isLastPage":true,"values":[{"id":42,"slug":"app","name":"Application","project":{"key":"ACME"}}]}`)
		case "/context/rest/api/1.0/projects/ACME/repos/app/pull-requests":
			if r.Method == "POST" {
				posts++
				var in map[string]any
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				if in["title"] != "Fix" {
					t.Error("wrong title")
				}
				created = true
				w.WriteHeader(201)
				fmt.Fprint(w, pull)
			} else if created {
				fmt.Fprintf(w, `{"isLastPage":true,"values":[%s]}`, pull)
			} else {
				fmt.Fprint(w, `{"isLastPage":true,"values":[]}`)
			}
		case "/context/rest/api/1.0/projects/ACME/repos/app/pull-requests/9/comments":
			fmt.Fprint(w, `{"id":11}`)
		default:
			http.Error(w, r.URL.Path, 404)
		}
	}))
	defer srv.Close()
	m, _ := manager(t)
	work, remote := repo(t)
	_, err := m.Save(ctx, scm.Input{Name: "dc", Kind: "bitbucket-dc", Repository: "ACME/app", APIURL: srv.URL + "/context", GitURL: "file://" + remote, Token: ptr(token)}, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.Test(ctx, "dc")
	if err != nil || !id.CanPush || id.Repository != "ACME/app" || id.DefaultBranch != "main" {
		t.Fatalf("identity: %+v %v", id, err)
	}
	readOnly.Store(true)
	limited, err := m.Test(ctx, "dc")
	if err != nil || limited.CanPush {
		t.Fatalf("read-only identity: %+v %v", limited, err)
	}
	readOnly.Store(false)
	denyRead.Store(true)
	if _, err := m.Test(ctx, "dc"); err == nil {
		t.Fatal("foreign repository granted access")
	}
	denyRead.Store(false)
	c, _ := m.Get(ctx, "dc")
	p, _, err := m.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	git(t, work, "checkout", "-q", "-b", "weft/fix")
	if _, err = scm.Push(ctx, c, p, token, work, "weft/fix", "main"); err != nil {
		t.Fatal(err)
	}
	if pr, err := p.FindPullRequest(ctx, "weft/fix", "main"); err != nil || pr != nil {
		t.Fatalf("initial lookup: %+v %v", pr, err)
	}
	pr, err := p.CreatePullRequest(ctx, scm.PullRequestInput{Title: "Fix", Head: "weft/fix", Base: "main"})
	if err != nil || pr == nil || !pr.Created || pr.Number != 9 {
		t.Fatalf("create: %+v %v", pr, err)
	}
	pr, err = p.FindPullRequest(ctx, "weft/fix", "main")
	if err != nil || pr == nil || pr.Created || posts != 1 {
		t.Fatalf("lookup: %+v %v", pr, err)
	}
	if err = p.Comment(ctx, 9, "Summary"); err != nil {
		t.Fatal(err)
	}
	if git(t, remote, "rev-parse", "refs/heads/weft/fix") != git(t, work, "rev-parse", "HEAD") {
		t.Fatal("push missing")
	}
}
func TestBitbucketDataCenterRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ repo, api, git string }{{"ACME/app", "", ""}, {"ACME/.", "https://git.example", ""}, {"ACME/app", "http://git.example", ""}, {"ACME/app", "https://git.example/rest/api/1.0", ""}, {"ACME/app", "https://git.example", "http://git.example/scm/acme/app.git"}} {
		t.Run(tc.repo+tc.api+tc.git, func(t *testing.T) {
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "dc", Kind: "bitbucket-dc", Repository: tc.repo, APIURL: tc.api, GitURL: tc.git, Token: ptr(token)}, "admin", false)
			if err == nil {
				t.Fatal("invalid DC connection saved")
			}
		})
	}
}
func TestBitbucketDataCenterLookupFailure(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"values":[],"isLastPage":false}`, `{"values":[],"isLastPage":false,"nextPageStart":0}`, `broken`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer srv.Close()
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "dc", Kind: "bitbucket-dc", Repository: "ACME/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := m.Get(ctx, "dc")
			p, _, err := m.Open(c)
			if err != nil {
				t.Fatal(err)
			}
			if pr, err := p.FindPullRequest(ctx, "weft/fix", "main"); err == nil || pr != nil {
				t.Fatalf("incomplete lookup accepted: %+v %v", pr, err)
			}
		})
	}
}
func TestBitbucketDataCenterErrorRedaction(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); fmt.Fprint(w, token) }))
			defer srv.Close()
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "dc", Kind: "bitbucket-dc", Repository: "ACME/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Test(ctx, "dc")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), token) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestBitbucketDataCenterSearchBoundsAndScope(t *testing.T) {
	for _, mode := range []string{"second-page", "foreign-source", "broken-page", "loop", "conflict", "closed"} {
		t.Run(mode, func(t *testing.T) {
			reads, posts := 0, 0
			pull := `{"id":9,"state":"OPEN","fromRef":{"id":"refs/heads/weft/fix","repository":{"slug":"app","project":{"key":"ACME"}}},"toRef":{"id":"refs/heads/main","repository":{"slug":"app","project":{"key":"ACME"}}},"links":{"self":[{"href":"https://git.example/pr/9"}]}}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					if mode == "conflict" {
						w.WriteHeader(409)
					} else {
						fmt.Fprint(w, strings.Replace(pull, `"OPEN"`, `"MERGED"`, 1))
					}
					return
				}
				reads++
				switch mode {
				case "second-page":
					if r.URL.Query().Get("start") == "3" {
						fmt.Fprintf(w, `{"isLastPage":true,"values":[%s]}`, pull)
					} else {
						fmt.Fprint(w, `{"isLastPage":false,"nextPageStart":3,"values":[]}`)
					}
				case "foreign-source":
					fmt.Fprintf(w, `{"isLastPage":true,"values":[%s]}`, strings.Replace(pull, `"ACME"`, `"OTHER"`, 1))
				case "broken-page":
					fmt.Fprint(w, `{}`)
				case "loop":
					fmt.Fprint(w, `{"isLastPage":false,"nextPageStart":0,"values":[]}`)
				default:
					fmt.Fprintf(w, `{"isLastPage":true,"values":[%s]}`, pull)
				}
			}))
			defer srv.Close()
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "dc", Kind: "bitbucket-dc", Repository: "ACME/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := m.Get(ctx, "dc")
			p, _, err := m.Open(c)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "conflict" || mode == "closed" {
				pr, err := p.CreatePullRequest(ctx, scm.PullRequestInput{Title: "Fix", Head: "weft/fix", Base: "main"})
				if mode == "conflict" {
					if err != nil || pr == nil || pr.Created || reads != 1 {
						t.Fatalf("reconciliation: %+v %v", pr, err)
					}
				} else if err == nil || pr != nil {
					t.Fatal("closed PR accepted")
				}
				if posts != 1 {
					t.Fatal("POST replayed")
				}
				return
			}
			pr, err := p.FindPullRequest(ctx, "weft/fix", "main")
			switch mode {
			case "second-page":
				if err != nil || pr == nil || reads != 2 {
					t.Fatalf("page: %+v %v", pr, err)
				}
			case "foreign-source":
				if err != nil || pr != nil {
					t.Fatal("foreign source matched")
				}
			default:
				if err == nil || pr != nil || reads > 10 {
					t.Fatalf("invalid page: %+v %v", pr, err)
				}
			}
		})
	}
}
