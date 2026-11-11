package repositoryimport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/repositoryimport/importtest"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func TestAuthenticationValidation(t *testing.T) {
	for _, a := range []Authentication{{Mode: "token"}, {Mode: "token", Username: "a:b", Token: "s"}, {Mode: "token", Username: "a", Token: "s\n"}, {Mode: "anonymous", Token: "s"}, {Mode: "local_git"}, {Mode: "local_gh"}, {Mode: "mcp"}} {
		if a.validate(false) == nil {
			t.Fatal("invalid selection accepted", a.Mode)
		}
	}
	for _, a := range []Authentication{{}, {Mode: "token", Username: "user", Token: "synthetic"}, {Mode: "local_git"}, {Mode: "local_gh"}} {
		if a.validate(true) != nil {
			t.Fatal(a.Mode)
		}
	}
}
func TestAuthenticationOnlyReachesFixedUpstream(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "fixture" || p != "synthetic-token" || r.URL.String() != importtestURL+"/info/refs?service=git-upload-pack" {
			t.Error("unexpected credential routing")
		}
		calls.Add(1)
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example/repo"}}, Body: io.NopCloser(strings.NewReader("synthetic-token"))}, nil
	})}
	var received atomic.Int64
	endpoint, close, _, err := bridge(t.Context(), importtestURL, client, 1024, &received, Authentication{Mode: "token", Username: "fixture", Token: "synthetic-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	resp, err := http.Get(endpoint + "/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if calls.Load() != 1 || strings.Contains(string(b), "synthetic-token") {
		t.Fatal("credential leaked or redirected")
	}
}
func TestNativeGitHelperExplicitAndReadOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	// A synthetic helper supports get only. No actual user credentials are accessed.
	config := "[credential]\n helper =\n helper = \"!f() { test \\\"$1\\\" = get || exit 1; printf 'username=fixture-user\\\\npassword=synthetic-read-token\\\\n'; }; f\"\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := (Authentication{Mode: "local_git"}).resolve(t.Context(), importtestURL, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a.Username != "fixture-user" || a.Token != "synthetic-read-token" {
		t.Fatal("helper result missing")
	}
}
func TestPrivateImportRetryAndNoPersistedCredentials(t *testing.T) {
	store, err := sqlstore.OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f, err := importtest.New(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Server.Close()
	root := t.TempDir()
	m, err := New(Options{Store: store, Root: root, Client: f.Client})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	in := Input{URL: "https://repository.fixture.example/private.git", Ref: "HEAD", Authentication: Authentication{Mode: "token", Username: "fixture-user", Token: "wrong-synthetic-token"}}
	r, err := m.Start(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	r = wait(t, m, r.ID)
	if r.State != "failed" || !strings.Contains(r.Message, "MCP OAuth is separate") {
		t.Fatal(r.State, r.Message)
	}
	old := r.Attempt
	// Wait for worker teardown before an explicit retry.
	m.Close(context.Background())
	m, err = New(Options{Store: store, Root: root, Client: f.Client})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	in.Retry = true
	in.Revision = r.Revision
	in.Authentication.Token = "synthetic-read-token"
	r, err = m.Start(t.Context(), in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	r = wait(t, m, r.ID)
	if r.State != "ready" || r.Commit != f.Commit || r.Attempt == old {
		t.Fatal(r.State)
	}
	rows, err := store.ListSettings(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.Contains(string(row.Value), "synthetic-read-token") || strings.Contains(string(row.Value), "fixture-user") {
			t.Fatal("credentials persisted")
		}
	}
	events, err := store.ListEvents(t.Context(), storage.EventFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(events)
	if strings.Contains(string(b), "synthetic-read-token") || strings.Contains(string(b), "fixture-user") {
		t.Fatal("credential in audit event")
	}
	config, err := os.ReadFile(filepath.Join(root, r.Attempt, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "synthetic-read-token") || strings.Contains(string(config), "fixture-user") {
		t.Fatal("credential in Git config")
	}
	if _, err = os.Stat(filepath.Join(root, old, ".git")); err != nil {
		t.Fatal("previous attempt lost", err)
	}
	// Public requests never silently select the private-authentication snapshot.
	public, err := m.Start(t.Context(), Input{URL: in.URL, Ref: in.Ref}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if public.ID == r.ID {
		t.Fatal("auth selection lost")
	}
}
func TestNativeGitHubCLIUsesSelectedHostAndNoTokenEnvironment(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
[ "$1" = auth ] && [ "$2" = token ] && [ "$3" = --hostname ] && [ "$4" = repository.fixture.example ] || exit 1
[ -z "$GH_TOKEN" ] && [ -z "$GITHUB_TOKEN" ] || exit 2
printf 'synthetic-gh-token\n'
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "must-not-be-used")
	t.Setenv("GITHUB_TOKEN", "must-not-be-used")
	a, err := (Authentication{Mode: "local_gh"}).resolve(t.Context(), importtestURL, t.TempDir())
	if err != nil || a.Username != "x-access-token" || a.Token != "synthetic-gh-token" {
		t.Fatal("explicit CLI resolution failed", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = (Authentication{Mode: "local_gh"}).resolve(ctx, importtestURL, t.TempDir()); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestCredentialOutputBoundedAndMissingHelperDoesNotExposeOutput(t *testing.T) {
	var b strings.Builder
	w := authOutput{writer: &b, left: 2}
	if _, err := w.Write([]byte("too long")); err == nil || b.Len() != 0 {
		t.Fatal("unbounded credential output")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nprintf 'synthetic-sensitive-error' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	_, err := (Authentication{Mode: "local_gh"}).resolve(t.Context(), importtestURL, t.TempDir())
	if err == nil || strings.Contains(err.Error(), "synthetic-sensitive-error") {
		t.Fatal("unsafe helper error")
	}
}
