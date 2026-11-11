// Package importtest serves real Git smart HTTP from disposable repositories.
// The injected transport maps only a reserved fixture hostname; production never
// enables this mapping or allows private network imports.
package importtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
)

const URL = "https://repository.fixture.example/source.git"

type Fixture struct {
	Server *httptest.Server
	Client *http.Client
	Commit string
	Root   string
}
type transport struct {
	target *url.URL
	next   http.RoundTripper
}

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "repository.fixture.example" {
		return nil, fmt.Errorf("unknown fixture host")
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = t.target.Scheme
	u.Host = t.target.Host
	copy.URL = &u
	copy.Host = t.target.Host
	return t.next.RoundTrip(copy)
}
func New(ctx context.Context, root string) (*Fixture, error) {
	git := func(dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		b, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("fixture Git: %w: %s", err, b)
		}
		return strings.TrimSpace(string(b)), nil
	}
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(src, 0700); err != nil {
		return nil, err
	}
	if _, err := git(src, "init", "-q", "-b", "main"); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("Public import fixture\n"), 0600); err != nil {
		return nil, err
	}
	if _, err := git(src, "add", "README.md"); err != nil {
		return nil, err
	}
	if _, err := git(src, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture baseline"); err != nil {
		return nil, err
	}
	head, err := git(src, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if _, err = git(root, "clone", "-q", "--bare", src, filepath.Join(root, "source.git")); err != nil {
		return nil, err
	}
	path, err := git(root, "--exec-path")
	if err != nil {
		return nil, err
	}
	handler := &cgi.Handler{Path: filepath.Join(path, "git-http-backend"), Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}}
	var retries atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/private.git/") {
			user, token, ok := r.BasicAuth()
			if !ok || user != "fixture-user" || token != "synthetic-read-token" {
				http.Error(w, "synthetic secret rejected", 401)
				return
			}
			copied := r.Clone(r.Context())
			u := *r.URL
			u.Path = strings.Replace(u.Path, "/private.git/", "/source.git/", 1)
			copied.URL = &u
			r = copied
		}
		if strings.HasPrefix(r.URL.Path, "/slow.git/") {
			<-r.Context().Done()
			return
		}
		if strings.HasPrefix(r.URL.Path, "/denied.git/") {
			http.Error(w, "synthetic private response must not leak", 401)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/retry.git/") {
			if retries.Add(1) == 1 {
				http.Error(w, "synthetic transient failure", 503)
				return
			}
			copied := r.Clone(r.Context())
			u := *r.URL
			u.Path = strings.Replace(u.Path, "/retry.git/", "/source.git/", 1)
			copied.URL = &u
			r = copied
		}
		handler.ServeHTTP(w, r)
	}))
	u, _ := url.Parse(srv.URL)
	return &Fixture{Server: srv, Client: &http.Client{Transport: transport{target: u, next: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Commit: head, Root: src}, nil
}
