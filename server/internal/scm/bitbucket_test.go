package scm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestBitbucketCloudDelivery(t *testing.T) {
	var created bool
	var comments int
	var lookups, posts int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/2.0/repositories/acme/app":
			fmt.Fprint(w, `{"full_name":"acme/app","mainbranch":{"name":"main"}}`)
		case "/2.0/user/workspaces/acme/permissions/repositories":
			if r.URL.Query().Get("q") != `repository.full_name="acme/app"` {
				t.Error("missing exact repository filter")
			}
			fmt.Fprint(w, `{"values":[{"repository":{"full_name":"acme/app"},"user":{"nickname":"bot"},"permission":"write"}]}`)
		case "/2.0/repositories/acme/app/pullrequests":
			if r.Method == "GET" {
				lookups++
				q := r.URL.Query()
				if q.Get("pagelen") != "50" {
					// Bitbucket rejects this endpoint's values above 50 with HTTP 400.
					http.Error(w, "invalid pagelen", http.StatusBadRequest)
					return
				}
				if q.Get("state") != "OPEN" || q.Get("q") != `source.branch.name="weft/fix" AND destination.branch.name="main"` {
					t.Error("missing exact open branch-pair filter")
					http.Error(w, "invalid filter", http.StatusBadRequest)
					return
				}
			}
			pull := `{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"repository":{"full_name":"acme/app"},"commit":{"hash":"abcdef"}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"}},"links":{"html":{"href":"https://bitbucket.org/acme/app/pull-requests/7"}}}`
			if r.Method == "POST" {
				posts++
				var in map[string]any
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				if in["title"] != "Fix" {
					t.Errorf("title: %v", in)
				}
				created = true
				w.WriteHeader(201)
				fmt.Fprint(w, pull)
			} else if created {
				fmt.Fprintf(w, `{"values":[%s]}`, pull)
			} else {
				fmt.Fprint(w, `{"values":[]}`)
			}
		case "/2.0/repositories/acme/app/pullrequests/7/comments":
			comments++
			fmt.Fprint(w, `{"id":1}`)
		default:
			http.Error(w, "unexpected path", 404)
		}
	}))
	defer server.Close()
	m, _ := manager(t)
	work, remote := repo(t)
	_, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: server.URL + "/2.0", GitURL: "file://" + remote, Token: ptr(token)}, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.Test(ctx, "bb")
	if err != nil || !id.CanPush || id.User != "bot" || id.DefaultBranch != "main" {
		t.Fatalf("identity: %+v %v", id, err)
	}
	git(t, work, "checkout", "-q", "-b", "weft/fix")
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	ap := &approver{approve: true}
	gw := tools.NewGateway(reg, ap, nil)
	p := principal(work, domain.ToolGrant{Name: "git.push", Repositories: []string{"bb"}}, domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"bb"}})
	out, err := gw.Invoke(ctx, p, "git.push", json.RawMessage(`{"connection":"bb"}`))
	if err != nil || out.IsError {
		t.Fatalf("push: %+v %v", out, err)
	}
	for i := 0; i < 2; i++ {
		out, err = gw.Invoke(ctx, p, "scm.pull_request.create", json.RawMessage(`{"connection":"bb","title":"Fix"}`))
		if err != nil || out.IsError {
			t.Fatalf("create: %+v %v", out, err)
		}
		var pr scm.PullRequest
		if err = json.Unmarshal(out.Data, &pr); err != nil || pr.Number != 7 || pr.Created != (i == 0) {
			t.Fatalf("PR: %+v %v", pr, err)
		}
	}
	if lookups != 2 || posts != 1 || len(ap.asked) != 1 {
		t.Fatalf("lookup/create/approval contract: lookups=%d posts=%d approvals=%d", lookups, posts, len(ap.asked))
	}
	if git(t, remote, "rev-parse", "refs/heads/weft/fix") != git(t, work, "rev-parse", "HEAD") {
		t.Fatal("remote revision differs")
	}
	c, _ := m.Get(ctx, "bb")
	provider, _, err := m.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = provider.Comment(ctx, 7, "Summary"); err != nil || comments != 1 {
		t.Fatalf("comment: %d %v", comments, err)
	}
}

func TestBitbucketCloudFailures(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"`+token+`"}}`)
			}))
			defer srv.Close()
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Test(ctx, "bb")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), token) {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
}

func TestBitbucketCloudLookupFailureDoesNotApproveOrCreate(t *testing.T) {
	for _, mode := range []string{"bad-request", "malformed", "page-limit"} {
		t.Run(mode, func(t *testing.T) {
			lookups, posts := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") != "Bearer "+token {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case "/2.0/repositories/acme/app":
					fmt.Fprint(w, `{"full_name":"acme/app","mainbranch":{"name":"main"}}`)
				case "/2.0/user/workspaces/acme/permissions/repositories":
					fmt.Fprint(w, `{"values":[{"repository":{"full_name":"acme/app"},"user":{"nickname":"bot"},"permission":"write"}]}`)
				case "/2.0/repositories/acme/app/pullrequests":
					if r.Method != "GET" {
						posts++
						http.Error(w, "unexpected write", http.StatusInternalServerError)
						return
					}
					lookups++
					if r.URL.Query().Get("pagelen") != "50" {
						t.Error("lookup did not use the endpoint page limit")
					}
					switch mode {
					case "bad-request":
						w.WriteHeader(http.StatusBadRequest)
						fmt.Fprintf(w, `{"error":{"message":%q}}`, token)
					case "malformed":
						fmt.Fprint(w, `{"values":null}`)
					case "page-limit":
						fmt.Fprintf(w, `{"values":[],"next":"http://%s%s?pagelen=50&page=%d"}`, r.Host, r.URL.Path, lookups+1)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			m, _ := manager(t)
			if _, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: srv.URL + "/2.0", Token: ptr(token)}, "admin", false); err != nil {
				t.Fatal(err)
			}
			ap := &approver{approve: true}
			reg := tools.NewRegistry()
			reg.MustRegister(scm.Tools(m)...)
			gw := tools.NewGateway(reg, ap, nil)
			out, err := gw.Invoke(ctx, principal("", domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"bb"}}), "scm.pull_request.create", json.RawMessage(`{"connection":"bb","title":"Fix","branch":"weft/fix"}`))
			if err != nil || out == nil || !out.IsError || !strings.Contains(out.Content, "could not look for an existing pull request") || strings.Contains(out.Content, token) {
				t.Fatalf("missing or unsafe lookup failure: %+v %v", out, err)
			}
			wantLookups := 1
			if mode == "page-limit" {
				wantLookups = 10
			}
			if lookups != wantLookups || posts != 0 || len(ap.asked) != 0 {
				t.Fatalf("lookup failure advanced delivery: lookups=%d posts=%d approvals=%d", lookups, posts, len(ap.asked))
			}
		})
	}
}

func TestBitbucketCloudPaginationAndScope(t *testing.T) {
	for _, mode := range []string{"second-page", "foreign-repo", "incomplete", "malformed", "missing-values", "null-values"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unexpected write")
				}
				calls++
				switch mode {
				case "second-page":
					if r.URL.Query().Get("page") == "2" {
						fmt.Fprint(w, `{"values":[{"id":5,"state":"OPEN","source":{"branch":{"name":"weft/x"},"repository":{"full_name":"acme/app"}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"}},"links":{"html":{"href":"https://bitbucket.org/acme/app/pull-requests/5"}}}]}`)
					} else {
						fmt.Fprintf(w, `{"values":[],"next":"http://%s%s?page=2"}`, r.Host, r.URL.Path)
					}
				case "foreign-repo":
					fmt.Fprintf(w, `{"values":[],"next":"http://%s/repositories/other/app/pullrequests?page=2"}`, r.Host)
				case "missing-values":
					fmt.Fprint(w, `{}`)
				case "null-values":
					fmt.Fprint(w, `{"values":null}`)
				case "incomplete":
					fmt.Fprintf(w, `{"values":[],"next":"http://%s%s?page=2"}`, r.Host, r.URL.Path)
				default:
					fmt.Fprint(w, "broken")
				}
			}))
			defer srv.Close()
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := m.Get(ctx, "bb")
			p, _, err := m.Open(c)
			if err != nil {
				t.Fatal(err)
			}
			pr, err := p.FindPullRequest(ctx, "weft/x", "main")
			if mode == "second-page" {
				if err != nil || pr == nil || pr.Number != 5 || calls != 2 {
					t.Fatalf("pagination: %+v %v calls=%d", pr, err, calls)
				}
			} else if err == nil || pr != nil || calls > 10 {
				t.Fatalf("unsafe/incomplete search: %+v %v calls=%d", pr, err, calls)
			}
		})
	}
}

func TestBitbucketCloudRejectsInvalidCreateAndReconcilesConflict(t *testing.T) {
	for _, mode := range []string{"closed", "fork", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			pull := `{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"repository":{"full_name":"acme/app"}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"}},"links":{"html":{"href":"https://bitbucket.org/acme/app/pull-requests/7"}}}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					switch mode {
					case "closed":
						fmt.Fprint(w, strings.Replace(pull, `"OPEN"`, `"MERGED"`, 1))
					case "fork":
						fmt.Fprint(w, strings.Replace(pull, `"acme/app"`, `"other/app"`, 1))
					default:
						w.WriteHeader(409)
					}
				} else {
					fmt.Fprintf(w, `{"values":[%s]}`, pull)
				}
			}))
			defer srv.Close()
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := m.Get(ctx, "bb")
			p, _, err := m.Open(c)
			if err != nil {
				t.Fatal(err)
			}
			pr, err := p.CreatePullRequest(ctx, scm.PullRequestInput{Title: "Fix", Head: "weft/fix", Base: "main"})
			if mode == "conflict" {
				if err != nil || pr == nil || pr.Created {
					t.Fatalf("reconciliation: %+v %v", pr, err)
				}
			} else if err == nil || pr != nil {
				t.Fatalf("invalid create accepted: %+v %v", pr, err)
			}
			if posts != 1 {
				t.Fatal("create was replayed")
			}
		})
	}
}

func TestBitbucketConnectionRejectsUnsafeRepositoryAndGitURL(t *testing.T) {
	for _, tc := range []struct{ repo, gitURL string }{{"acme/.", ""}, {"./app", ""}, {"acme/..", ""}, {"acme/app", "http://git.example/acme/app.git"}, {"acme/app", "https://user:secret@git.example/acme/app.git"}} {
		t.Run(tc.repo+tc.gitURL, func(t *testing.T) {
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: tc.repo, GitURL: tc.gitURL, Token: ptr(token)}, "admin", false)
			if err == nil {
				t.Fatal("unsafe connection saved")
			}
			rows, err := m.List(ctx)
			if err != nil || len(rows) != 0 {
				t.Fatalf("failed save changed storage: %v %v", rows, err)
			}
		})
	}
}
