package scm_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

var ctx = context.Background()

const token = "scm-secret-token"

// fake is a provider that records calls; registered as kind "fake".
type fake struct {
	token string
	prs   *[]scm.PullRequest // shared across Open calls, like a real server
}

var openPRs []scm.PullRequest

// remoteURL is the fake connection's Git URL (set from the connection).
var remoteURL string

func (f *fake) Identity(context.Context) (scm.Identity, error) {
	if f.token != token {
		return scm.Identity{}, errors.New("bad credentials " + f.token)
	}
	return scm.Identity{User: "bot", Repository: "acme/app", DefaultBranch: "main", CanPush: !readOnly}, nil
}
func (f *fake) FindPullRequest(_ context.Context, head, base string) (*scm.PullRequest, error) {
	for _, p := range *f.prs {
		if p.Head == head && p.Base == base {
			return &p, nil
		}
	}
	return nil, nil
}
func (f *fake) CreatePullRequest(_ context.Context, in scm.PullRequestInput) (*scm.PullRequest, error) {
	p := scm.PullRequest{Number: len(*f.prs) + 1, URL: fmt.Sprintf("https://scm.example/pr/%d", len(*f.prs)+1), Head: in.Head, Base: in.Base, State: "open", Created: true}
	*f.prs = append(*f.prs, p)
	return &p, nil
}
func (f *fake) Comment(context.Context, int, string) error { return nil }
func (f *fake) GitAuthHeader() string                      { return "Bearer " + f.token }
func (f *fake) RemoteURL() string                          { return remoteURL }

var last *fake

// readOnly makes the fake token unable to push.
var readOnly bool

func init() {
	scm.RegisterKind("fake", func(c scm.Connection, tok string) (scm.Provider, error) {
		remoteURL = c.GitURL
		last = &fake{token: tok, prs: &openPRs}
		return last, nil
	})
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo returns a work tree with one commit on main and a bare remote.
func repo(t *testing.T) (work, remote string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	work = filepath.Join(root, "work")
	git(t, root, "init", "-q", "--bare", remote)
	git(t, root, "init", "-q", "-b", "main", work)
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "a.txt")
	git(t, work, "commit", "-q", "-m", "base")
	return work, remote
}

func manager(t *testing.T) (scm.Manager, storage.Store) {
	t.Helper()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secrets.NewBox(bytes.Repeat([]byte{7}, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return scm.Manager{Store: st, Box: box}, st
}

func ptr[T any](v T) *T { return &v }

func TestConnectionsSealTokensAndValidate(t *testing.T) {
	m, st := manager(t)
	_, err := m.Save(ctx, scm.Input{Name: "Bad Name", Kind: "svn", Repository: "x", APIURL: "ftp://x", GitURL: "https://u:p@host/r.git", BranchPrefix: "nope", Token: ptr(" ")}, "admin", false)
	var ve *scm.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want validation error: %v", err)
	}
	paths := map[string]bool{}
	for _, i := range ve.Errors {
		paths[i.Path] = true
	}
	for _, p := range []string{"name", "kind", "repository", "apiUrl", "gitUrl", "branchPrefix", "token"} {
		if !paths[p] {
			t.Errorf("missing %s in %+v", p, ve.Errors)
		}
	}
	if _, err := m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app"}, "admin", false); !errors.As(err, &ve) {
		t.Fatalf("a new connection needs a token: %v", err)
	}
	v, err := m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app", GitURL: "file:///tmp/r.git", Token: ptr(token)}, "admin", false)
	if err != nil || !v.TokenConfigured || v.Token != "" || v.Prefix() != scm.DefaultBranchPrefix || !v.ApprovalRequired() {
		t.Fatalf("created: %+v %v", v, err)
	}
	raw, _ := st.GetSetting(ctx, scm.SettingKey)
	if bytes.Contains(raw.Value, []byte(token)) {
		t.Fatal("token stored in plain text")
	}
	if _, err := m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app", Token: ptr(token)}, "admin", false); !errors.As(err, &ve) {
		t.Fatalf("duplicate name: %v", err)
	}
	// Updating without a token keeps the stored one.
	v, err = m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app", BranchPrefix: "bots/", RequirePullRequestApproval: ptr(false)}, "admin", true)
	if err != nil || !v.TokenConfigured || v.Prefix() != "bots/" || v.ApprovalRequired() {
		t.Fatalf("updated: %+v %v", v, err)
	}
	id, err := m.Test(ctx, "app")
	if err != nil || id.User != "bot" || !id.CanPush {
		t.Fatalf("test: %+v %v", id, err)
	}
	list, _ := m.List(ctx)
	if len(list) != 1 || list[0].Token != "" {
		t.Fatalf("list: %+v", list)
	}
	if err := m.Delete(ctx, "app", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, "app"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
	// Without an installation key nothing can be stored.
	noKey := scm.Manager{Store: st}
	if _, err := noKey.Save(ctx, scm.Input{Name: "x", Kind: "fake", Repository: "a/b", Token: ptr(token)}, "admin", false); !errors.Is(err, secrets.ErrNoKey) {
		t.Fatalf("no key: %v", err)
	}
}

func TestPushOnlyPrefixedBranchesWithoutStoringCredentials(t *testing.T) {
	m, _ := manager(t)
	work, remote := repo(t)
	if _, err := m.Save(ctx, scm.Input{Name: "app", Kind: "fake", Repository: "acme/app", GitURL: "file://" + remote, Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	c, _ := m.Get(ctx, "app")
	p, tok, err := m.Open(c)
	if err != nil || tok != token {
		t.Fatalf("open: %v", err)
	}
	for _, branch := range []string{"main", "feature/x", "weft/../main", "-weft/x", "weft/x.lock", "weft/"} {
		if _, err := scm.Push(ctx, c, p, tok, work, branch, "main"); !errors.Is(err, scm.ErrPush) {
			t.Fatalf("%q must be refused: %v", branch, err)
		}
	}
	git(t, work, "checkout", "-q", "-b", "weft/task-1")
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "b.txt")
	git(t, work, "commit", "-q", "-m", "change")
	git(t, work, "tag", "v1")
	if _, err := scm.Push(ctx, c, p, tok, work, "weft/task-1", "main"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, remote, "rev-parse", "refs/heads/weft/task-1"); got != git(t, work, "rev-parse", "HEAD") {
		t.Fatalf("remote branch = %s", got)
	}
	// Nothing else reached the remote, and no credential was written to
	// the repository's configuration.
	if refs := git(t, remote, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/weft/task-1" {
		t.Fatalf("remote refs = %q", refs)
	}
	cfg, _ := os.ReadFile(filepath.Join(work, ".git", "config"))
	if bytes.Contains(cfg, []byte(token)) || bytes.Contains(cfg, []byte("extraHeader")) {
		t.Fatalf("credential in git config: %s", cfg)
	}
	// History rewrites are never forced.
	git(t, work, "reset", "-q", "--hard", "HEAD~1")
	if err := os.WriteFile(filepath.Join(work, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "c.txt")
	git(t, work, "commit", "-q", "-m", "rewrite")
	if _, err := scm.Push(ctx, c, p, tok, work, "weft/task-1", "main"); !errors.Is(err, scm.ErrPush) || strings.Contains(err.Error(), token) {
		t.Fatalf("non-fast-forward must be refused without leaking the token: %v", err)
	}
}
