package scm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

// githubFixture is the slice of the GitHub REST API the adapter uses.
type githubFixture struct {
	*httptest.Server
	mu        sync.Mutex
	pulls     []map[string]any
	comments  []string
	push      bool
	duplicate bool // the next create answers 422 "already exists"
}

func newGitHub(t *testing.T) *githubFixture {
	f := &githubFixture{push: true}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"message":"Bad credentials: ` + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") + `"}`))
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"login":"weft-bot"}`))
		case r.Method == "GET" && r.URL.Path == "/repos/acme/app":
			_ = json.NewEncoder(w).Encode(map[string]any{"full_name": "acme/app", "default_branch": "main", "permissions": map[string]bool{"push": f.push}})
		case r.Method == "GET" && r.URL.Path == "/repos/missing/app":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == "GET" && r.URL.Path == "/repos/acme/app/pulls":
			out := []map[string]any{}
			for _, p := range f.pulls {
				if "acme:"+p["head"].(map[string]any)["ref"].(string) == r.URL.Query().Get("head") && r.URL.Query().Get("state") == "open" {
					out = append(out, p)
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "POST" && r.URL.Path == "/repos/acme/app/pulls":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			n := len(f.pulls) + 1
			p := map[string]any{"number": n, "html_url": "https://github.example/acme/app/pull/" + string(rune('0'+n)), "state": "open",
				"head": map[string]any{"ref": in["head"]}, "base": map[string]any{"ref": in["base"]}, "title": in["title"], "body": in["body"]}
			f.pulls = append(f.pulls, p)
			if f.duplicate {
				f.duplicate = false
				w.WriteHeader(422)
				_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[{"message":"A pull request already exists for acme:` + in["head"] + `."}]}`))
				return
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(p)
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/repos/acme/app/issues/"):
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.comments = append(f.comments, in["body"])
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func TestGitHubAdapter(t *testing.T) {
	gh := newGitHub(t)
	m, _ := manager(t)
	if _, err := m.Save(ctx, scm.Input{Name: "gh", Kind: "github", Repository: "acme/app", APIURL: gh.URL, Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	c, _ := m.Get(ctx, "gh")
	p, _, err := m.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	if p.RemoteURL() != "https://github.com/acme/app.git" || p.GitAuthHeader() != scm.BasicAuth("x-access-token", token) {
		t.Fatalf("remote %q header %q", p.RemoteURL(), p.GitAuthHeader())
	}
	id, err := m.Test(ctx, "gh")
	if err != nil || id != (scm.Identity{User: "weft-bot", Repository: "acme/app", DefaultBranch: "main", CanPush: true}) {
		t.Fatalf("identity: %+v %v", id, err)
	}
	if pr, err := p.FindPullRequest(ctx, "weft/x", "main"); err != nil || pr != nil {
		t.Fatalf("no pr yet: %+v %v", pr, err)
	}
	pr, err := p.CreatePullRequest(ctx, scm.PullRequestInput{Title: "T", Body: "B", Head: "weft/x", Base: "main"})
	if err != nil || pr.Number != 1 || !pr.Created || pr.Head != "weft/x" || pr.Base != "main" || !strings.HasPrefix(pr.URL, "https://github.example/") {
		t.Fatalf("create: %+v %v", pr, err)
	}
	if found, err := p.FindPullRequest(ctx, "weft/x", "main"); err != nil || found == nil || found.Number != 1 || found.Created {
		t.Fatalf("find: %+v %v", found, err)
	}
	// Opened concurrently by someone else: the existing one is returned.
	gh.duplicate = true
	if got, err := p.CreatePullRequest(ctx, scm.PullRequestInput{Title: "T", Head: "weft/y", Base: "main"}); err != nil || got.Head != "weft/y" || got.Created {
		t.Fatalf("duplicate: %+v %v", got, err)
	}
	if err := p.Comment(ctx, 1, "Weft finished"); err != nil || len(gh.comments) != 1 {
		t.Fatalf("comment: %v %v", err, gh.comments)
	}

	// Failures are actionable and never echo the token.
	if _, err := m.Save(ctx, scm.Input{Name: "bad", Kind: "github", Repository: "acme/app", APIURL: gh.URL, Token: ptr("wrong-" + token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Test(ctx, "bad"); err == nil || !strings.Contains(err.Error(), "invalid or expired") || strings.Contains(err.Error(), "wrong-"+token) {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := m.Save(ctx, scm.Input{Name: "missing", Kind: "github", Repository: "missing/app", APIURL: gh.URL, Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Test(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "cannot see it") {
		t.Fatalf("missing repository: %v", err)
	}
}

// A task branch is pushed and its pull request opened through the gateway,
// against the GitHub fixture with a local remote standing in for github.com.
func TestGitHubPushAndPullRequestThroughTheGateway(t *testing.T) {
	gh := newGitHub(t)
	m, _ := manager(t)
	work, remote := repo(t)
	if _, err := m.Save(ctx, scm.Input{Name: "gh", Kind: "github", Repository: "acme/app", APIURL: gh.URL, GitURL: "file://" + remote, Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	gw := tools.NewGateway(reg, &approver{approve: true}, nil)
	grants := []domain.ToolGrant{{Name: "scm.*", Repositories: []string{"gh"}}, {Name: "git.push", Repositories: []string{"gh"}}}
	git(t, work, "checkout", "-q", "-b", "weft/task-9")
	if err := os.WriteFile(filepath.Join(work, "fix.txt"), []byte("fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "fix.txt")
	git(t, work, "commit", "-q", "-m", "fix")
	res, err := gw.Invoke(ctx, principal(work, grants...), "git.push", json.RawMessage(`{"connection":"gh"}`))
	if err != nil || res.IsError {
		t.Fatalf("push: %+v %v", res, err)
	}
	res, err = gw.Invoke(ctx, principal(work, grants...), "scm.pull_request.create", json.RawMessage(`{"connection":"gh","title":"Fix","body":"Done by Weft"}`))
	if err != nil || res.IsError || !strings.Contains(res.Content, "Opened pull request #1") {
		t.Fatalf("pull request: %+v %v", res, err)
	}
	if git(t, remote, "rev-parse", "refs/heads/weft/task-9") != git(t, work, "rev-parse", "HEAD") || gh.pulls[0]["title"] != "Fix" {
		t.Fatalf("remote or pull request missing: %+v", gh.pulls)
	}
	// A read-only token cannot push.
	gh.push = false
	res, _ = gw.Invoke(ctx, principal(work, grants...), "git.push", json.RawMessage(`{"connection":"gh"}`))
	if !res.IsError || !strings.Contains(res.Content, "cannot push") {
		t.Fatalf("read-only: %+v", res)
	}
}

func TestTriggeredPullRequestBodyUsesAdmittedIssueIdentity(t *testing.T) {
	for _, tc := range []struct{ issue, closing string }{
		{"acme/app#7", "Closes #7"},
		{"acme/issues#7", "Closes acme/issues#7"},
		{"", ""},
	} {
		t.Run(tc.closing, func(t *testing.T) {
			gh := newGitHub(t)
			m, st := manager(t)
			if _, err := m.Save(ctx, scm.Input{Name: "gh", Kind: "github", Repository: "acme/app", APIURL: gh.URL, Token: ptr(token)}, "admin", false); err != nil {
				t.Fatal(err)
			}
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "workflow", Name: "Workflow"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker"}}}}
			if _, err := st.SaveGraph(ctx, g, 0); err != nil {
				t.Fatal(err)
			}
			tasks := taskconfig.Manager{Store: st}
			draft, err := tasks.SaveTriggered(ctx, taskconfig.Input{Title: "Fix", Request: "User text mentions #999", Outcome: "implement", GraphID: "workflow", GraphVersion: 1, NodeID: "worker"}, "admin", tc.issue)
			if err != nil {
				t.Fatal(err)
			}
			draft.ExecutionGraphID = domain.TaskRunGraphPrefix + strings.TrimPrefix(draft.ID, "tsk_")
			if _, err := tasks.Transition(ctx, draft, "running", "admin"); err != nil {
				t.Fatal(err)
			}
			reg := tools.NewRegistry()
			reg.MustRegister(scm.Tools(m)...)
			gw := tools.NewGateway(reg, &approver{approve: true}, nil)
			p := principal("", domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"gh"}})
			p.GraphID = draft.ExecutionGraphID
			out, err := gw.Invoke(ctx, p, "scm.pull_request.create", json.RawMessage(`{"connection":"gh","branch":"weft/fix","title":"Fix","body":"Verified change."}`))
			if err != nil || out.IsError || len(gh.pulls) != 1 {
				t.Fatalf("open triggered PR: %+v %v", out, err)
			}
			want := "Verified change."
			if tc.closing != "" {
				want += "\n\n" + tc.closing
			}
			if got := gh.pulls[0]["body"]; got != want {
				t.Fatalf("body %q, want %q", got, want)
			}
		})
	}
}

// TestGitHubLive runs against a personal test repository only when
// WEFT_TEST_GITHUB_TOKEN and WEFT_TEST_GITHUB_REPO are set.
func TestGitHubLive(t *testing.T) {
	tok, repo := os.Getenv("WEFT_TEST_GITHUB_TOKEN"), os.Getenv("WEFT_TEST_GITHUB_REPO")
	if tok == "" || repo == "" {
		t.Skip("set WEFT_TEST_GITHUB_TOKEN and WEFT_TEST_GITHUB_REPO to run against GitHub")
	}
	m, _ := manager(t)
	if _, err := m.Save(ctx, scm.Input{Name: "live", Kind: "github", Repository: repo, Token: &tok}, "test", false); err != nil {
		t.Fatal(err)
	}
	id, err := m.Test(ctx, "live")
	if err != nil || id.DefaultBranch == "" {
		t.Fatalf("live identity: %+v %v", id, err)
	}
}
