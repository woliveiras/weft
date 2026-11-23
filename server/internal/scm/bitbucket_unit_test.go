package scm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type bitbucketRoundTripFunc func(*http.Request) (*http.Response, error)

func (f bitbucketRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBitbucketCloudPullRequestLookupRequest(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			h, err := newProviderHTTP("https://api.bitbucket.org/2.0", "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			h.client.Transport = bitbucketRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/2.0/repositories/acme/app/pullrequests" {
					t.Fatalf("unexpected lookup: %s %s", r.Method, r.URL.Path)
				}
				q := r.URL.Query()
				// This endpoint has a lower limit than Bitbucket's global maximum of 100.
				if q.Get("pagelen") != "50" || q.Get("state") != "OPEN" || len(q) != 3 {
					t.Fatalf("invalid collection constraints: %v", q)
				}
				if q.Get("q") != `source.branch.name="weft/topic\"quoted" AND destination.branch.name="release/next"` {
					t.Fatalf("branch filter was not quoted exactly: %s", q.Get("q"))
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"values":[]}`))}, nil
			})
			b := &bitbucketCloud{http: h, c: Connection{Repository: "acme/app"}}
			pr, err := b.FindPullRequest(context.Background(), `weft/topic"quoted`, "release/next")
			if pr != nil || calls != 1 || (status == http.StatusOK) != (err == nil) {
				t.Fatalf("lookup: pr=%+v error=%v calls=%d", pr, err, calls)
			}
		})
	}
}

func TestBitbucketCloudDefaultsAndInput(t *testing.T) {
	f, _ := factory("bitbucket-cloud")
	p, err := f(Connection{Repository: "team/repo"}, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if p.RemoteURL() != "https://bitbucket.org/team/repo.git" || p.GitAuthHeader() != BasicAuth("x-token-auth", "synthetic") {
		t.Fatal("incorrect Git contract")
	}
	for _, u := range []string{"http://example.com/api", "https://user:pass@example.com/api", "https://example.com/api?x=1", "https://example.com/api#x", "file:///tmp/api"} {
		if _, err := f(Connection{APIURL: u}, "synthetic"); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	if err = p.Comment(context.Background(), 0, "x"); err == nil {
		t.Fatal("accepted invalid PR")
	}
	if err = p.Comment(context.Background(), 1, ""); err == nil {
		t.Fatal("accepted empty comment")
	}
}
func TestBitbucketHTTPBoundary(t *testing.T) {
	var leaked bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer sink.Close()
	for _, mode := range []string{"redirect", "foreign-page", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, sink.URL, 302)
				case "malformed":
					fmt.Fprint(w, "{")
				case "oversized":
					fmt.Fprint(w, strings.Repeat("x", adapterBodyLimit+1))
				default:
					fmt.Fprint(w, `{}`)
				}
			}))
			defer srv.Close()
			h, err := newProviderHTTP(srv.URL, "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			path := "/test"
			if mode == "foreign-page" {
				path = sink.URL + "/test"
			}
			var out any
			if err = h.do(context.Background(), "GET", path, nil, &out); err == nil {
				t.Fatal("accepted unsafe/broken response")
			}
			if leaked {
				t.Fatal("credential sent outside reviewed API")
			}
		})
	}
}

func TestBitbucketPaginationConfinesCollection(t *testing.T) {
	h, err := newProviderHTTP("https://api.example/2.0", "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"https://other.example/2.0/repositories/acme/app/pullrequests?page=2", "https://api.example/2.0/repositories/other/app/pullrequests?page=2", "https://user:pass@api.example/2.0/repositories/acme/app/pullrequests?page=2"} {
		if _, err := h.nextPage("/repositories/acme/app/pullrequests", next); err == nil {
			t.Fatalf("accepted %s", next)
		}
	}
	if _, err = h.nextPage("/repositories/acme/app/pullrequests", "https://api.example/2.0/repositories/acme/app/pullrequests?page=2"); err != nil {
		t.Fatal(err)
	}
}
