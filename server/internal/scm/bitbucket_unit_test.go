package scm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
