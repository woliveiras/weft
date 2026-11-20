package scm_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestBitbucketAPITokenEncryptedScopeAndPreview(t *testing.T) {
	m, st := manager(t)
	in := scm.Input{Name: "bb-api", Kind: "bitbucket-cloud", Repository: "acme/app", AuthType: "api-token", Username: "operator@example.invalid", Token: ptr(token)}
	view, err := m.Save(ctx, in, "admin", false)
	if err != nil || view.Token != "" || !view.TokenConfigured || view.AuthType != "api-token" || view.Username != in.Username {
		t.Fatalf("save: %+v %v", view, err)
	}
	saved, err := st.GetSetting(ctx, scm.SettingKey)
	if err != nil || strings.Contains(string(saved.Value), token) || strings.Contains(string(saved.Value), scm.BasicAuth(in.Username, token)) {
		t.Fatal("credential persisted in plaintext")
	}
	in.Token = nil
	if _, err = m.Preview(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Save(ctx, in, "admin", true); err != nil {
		t.Fatal(err)
	}
	old, _ := m.Get(ctx, in.Name)
	for _, change := range []func(*scm.Input){
		func(x *scm.Input) { x.Repository = "acme/other" }, func(x *scm.Input) { x.APIURL = "https://another.invalid" },
		func(x *scm.Input) { x.GitURL = "https://another.invalid/repo.git" }, func(x *scm.Input) { x.Username = "other@example.invalid" },
		func(x *scm.Input) { x.AuthType = "bearer"; x.Username = "" }, func(x *scm.Input) { x.Kind = "github"; x.AuthType = "bearer"; x.Username = "" },
	} {
		changed := in
		change(&changed)
		if _, err = m.Preview(ctx, changed); err == nil {
			t.Fatal("preview accepted credential reuse")
		}
		if _, err = m.Save(ctx, changed, "admin", true); err == nil {
			t.Fatal("save accepted credential reuse")
		}
		after, _ := m.Get(ctx, in.Name)
		if after.Token != old.Token || !after.UpdatedAt.Equal(old.UpdatedAt) || after.Repository != old.Repository || after.Username != old.Username {
			t.Fatal("rejected update changed saved connection")
		}
		changed.Token = ptr("replacement-synthetic-token")
		if _, err = m.Preview(ctx, changed); err != nil {
			t.Fatal(err)
		}
	}
	corrupt := old
	corrupt.Username = ""
	if _, _, err = m.Open(corrupt); err == nil {
		t.Fatal("opened malformed stored authentication")
	}
	corrupt = old
	corrupt.Token = "corrupt-ciphertext"
	if _, _, err = m.Open(corrupt); err == nil {
		t.Fatal("opened corrupt encrypted credential")
	}
	for _, username := range []string{"", "not-email", "operator@example.invalid\r\nX: injected"} {
		bad := in
		bad.Name = "bad"
		bad.Username = username
		bad.Token = ptr(token)
		if _, err = m.Save(ctx, bad, "admin", false); err == nil {
			t.Fatal("accepted invalid API-token account")
		}
	}
	boundary := in
	boundary.Name = "boundary"
	boundary.Token = ptr(strings.Repeat("x", 4096))
	if _, err = m.Save(ctx, boundary, "admin", false); err != nil {
		t.Fatalf("maximum token length refused: %v", err)
	}
	boundary.Name = "oversized"
	boundary.Token = ptr(strings.Repeat("x", 4097))
	if _, err = m.Save(ctx, boundary, "admin", false); err == nil {
		t.Fatal("oversized token accepted")
	}
}

func TestBitbucketAPITokenHTTPGitDeliveryAndReview(t *testing.T) {
	m, st := manager(t)
	m.Bus = events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	work, remote := repo(t)
	git(t, remote, "config", "http.receivepack", "true")
	git(t, work, "checkout", "-qb", "weft/fix")
	execPath := git(t, work, "--exec-path")
	backend := &cgi.Handler{Path: filepath.Join(execPath, "git-http-backend"), Env: []string{"GIT_PROJECT_ROOT=" + filepath.Dir(remote), "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}}
	email := "operator@example.invalid"
	restHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
	gitHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-bitbucket-api-token-auth:"+token))
	restPayload, gitPayload := strings.TrimPrefix(restHeader, "Basic "), strings.TrimPrefix(gitHeader, "Basic ")
	reflectedURL := "https://bitbucket.org/acme/app/pull-requests/7?rest=" + url.QueryEscape(restPayload) + "&git=" + strings.ReplaceAll(url.QueryEscape(gitPayload), "%3D", "%3d")
	head, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var gitRequests, creates atomic.Int32
	var comment string
	pull := func() string {
		raw := fmt.Sprintf(`{"id":7,"state":"OPEN","title":%q,"description":%q,"source":{"branch":{"name":"weft/fix"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"links":{"html":{"href":%q}}}`, "Reflected "+restHeader, "Reflected "+gitPayload+" "+token, head, base, reflectedURL)
		// A valid alternate JSON Unicode spelling of each payload's first byte
		// must not restore a reusable secret after the response boundary.
		raw = strings.ReplaceAll(raw, restPayload, fmt.Sprintf(`\u%04x`, restPayload[0])+restPayload[1:])
		return strings.ReplaceAll(raw, gitPayload, fmt.Sprintf(`\u%04x`, gitPayload[0])+gitPayload[1:])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/remote.git/") {
			if r.Header.Get("Authorization") != gitHeader {
				http.Error(w, "bad Git credential", 401)
				return
			}
			gitRequests.Add(1)
			backend.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != restHeader {
			t.Error("wrong REST auth contract")
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/repositories/acme/app":
			fmt.Fprint(w, `{"full_name":"acme/app","mainbranch":{"name":"main"}}`)
		case "/2.0/user/workspaces/acme/permissions/repositories":
			fmt.Fprint(w, `{"values":[{"repository":{"full_name":"acme/app"},"user":{"nickname":"bot"},"permission":"write"}]}`)
		case "/2.0/repositories/acme/app/pullrequests":
			if r.Method == "POST" {
				creates.Add(1)
				w.WriteHeader(201)
				fmt.Fprint(w, pull())
			} else if creates.Load() > 0 {
				fmt.Fprintf(w, `{"values":[%s]}`, pull())
			} else {
				fmt.Fprint(w, `{"values":[]}`)
			}
		case "/2.0/repositories/acme/app/pullrequests/7":
			fmt.Fprint(w, pull())
		case "/2.0/repositories/acme/app/diff/" + head + ".." + base:
			fmt.Fprint(w, "diff --git a/a.go b/a.go\n+"+restHeader+"\n+"+strings.TrimPrefix(gitHeader, "Basic ")+"\n+"+token+"\n")
		case "/2.0/repositories/acme/app/pullrequests/7/comments":
			if r.Method == "POST" {
				var in struct {
					Content struct {
						Raw string `json:"raw"`
					} `json:"content"`
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				comment = in.Content.Raw
				fmt.Fprint(w, `{"id":1}`)
			} else {
				b, _ := json.Marshal(map[string]any{"values": []any{map[string]any{"pending": false, "content": map[string]string{"raw": comment}}}})
				w.Write(b)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", AuthType: "api-token", Username: email, APIURL: srv.URL + "/2.0", GitURL: srv.URL + "/remote.git", Token: ptr(token)}, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	// Reload through the persisted encrypted document before invoking the gateway.
	list, err := m.List(ctx)
	if err != nil || len(list) != 1 || list[0].Token != "" {
		t.Fatal("list credential boundary", err)
	}
	id, err := m.Test(ctx, "bb")
	if err != nil || !id.CanPush {
		t.Fatalf("identity: %+v %v", id, err)
	}
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	gw := tools.NewGateway(reg, &approver{approve: true}, nil)
	principal := principal(work, domain.ToolGrant{Name: "git.push", Repositories: []string{"bb"}}, domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"bb"}}, domain.ToolGrant{Name: "scm.pull_request.read", Repositories: []string{"bb"}}, domain.ToolGrant{Name: "scm.pull_request.review", Repositories: []string{"bb"}})
	for _, step := range []struct{ name, args string }{
		{"git.push", `{"connection":"bb"}`},
		{"scm.pull_request.create", `{"connection":"bb","title":"Fix"}`},
		{"scm.pull_request.create", `{"connection":"bb","title":"Fix"}`},
		{"scm.pull_request.read", `{"connection":"bb","number":7,"diff":true}`},
		{"scm.pull_request.review", fmt.Sprintf(`{"connection":"bb","number":7,"headSha":%q,"baseSha":%q,"verdict":"approved","summary":"No findings. Tests passed."}`, head, base)},
	} {
		out, err := gw.Invoke(ctx, principal, step.name, json.RawMessage(step.args))
		if err != nil || out == nil || out.IsError {
			t.Fatalf("%s: %+v %v", step.name, out, err)
		}
		for _, secret := range []string{token, restHeader, restPayload, gitHeader, gitPayload, url.QueryEscape(restPayload), url.QueryEscape(gitPayload), strings.ReplaceAll(url.QueryEscape(gitPayload), "%3D", "%3d")} {
			if strings.Contains(out.Content+string(out.Data), secret) {
				t.Fatalf("%s leaked credential", step.name)
			}
		}
	}
	if gitRequests.Load() < 2 || creates.Load() != 1 || comment == "" {
		t.Fatalf("collaboration: git=%d PR=%d comment=%t", gitRequests.Load(), creates.Load(), comment != "")
	}
	if git(t, remote, "rev-parse", "refs/heads/weft/fix") != git(t, work, "rev-parse", "HEAD") {
		t.Fatal("push did not reach remote")
	}
	// A remote hook may echo either API or Git reusable credential form. These
	// must be removed from Git's real stderr as well as ordinary REST evidence.
	reflected := token + " " + restHeader + " " + strings.TrimPrefix(restHeader, "Basic ") + " " + gitHeader + " " + strings.TrimPrefix(gitHeader, "Basic ")
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\necho '"+reflected+"' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("second commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "a.txt")
	git(t, work, "commit", "-qm", "second fixture commit")
	out, err := gw.Invoke(ctx, principal, "git.push", json.RawMessage(`{"connection":"bb"}`))
	if err != nil || out == nil || !out.IsError || !strings.Contains(out.Content, "[redacted]") {
		t.Fatalf("refused push: %+v %v", out, err)
	}
	for _, secret := range []string{token, strings.TrimPrefix(restHeader, "Basic "), strings.TrimPrefix(gitHeader, "Basic ")} {
		if strings.Contains(out.Content, secret) {
			t.Fatal("remote Git output leaked credential")
		}
	}
	settings, err := st.ListSettings(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, saved := range settings {
		if strings.Contains(string(saved.Value), token) || strings.Contains(string(saved.Value), restPayload) || strings.Contains(string(saved.Value), gitPayload) || strings.Contains(string(saved.Value), url.QueryEscape(restPayload)) || strings.Contains(string(saved.Value), strings.ReplaceAll(url.QueryEscape(gitPayload), "%3D", "%3d")) {
			t.Fatal("persisted plaintext credential")
		}
	}
	evs, err := st.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvPullRequestOpened, domain.EvReviewSummaryPublished}})
	if err != nil || len(evs) == 0 {
		t.Fatal("missing delivery/review evidence", err)
	}
	for _, ev := range evs {
		for _, secret := range []string{token, restPayload, gitPayload, url.QueryEscape(restPayload), strings.ReplaceAll(url.QueryEscape(gitPayload), "%3D", "%3d")} {
			if strings.Contains(string(ev.Data), secret) {
				t.Fatal("event leaked encoded credential")
			}
		}
	}
}

func TestBitbucketAPITokenNoAuthFallbackOnDependencyFailures(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if user, pw, ok := r.BasicAuth(); !ok || user != "operator@example.invalid" || pw != token {
					t.Error("wrong auth")
				}
				w.WriteHeader(status)
				fmt.Fprint(w, scm.BasicAuth("operator@example.invalid", token))
			}))
			defer srv.Close()
			m, _ := manager(t)
			_, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", AuthType: "api-token", Username: "operator@example.invalid", APIURL: srv.URL, Token: ptr(token)}, "admin", false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Test(ctx, "bb")
			if err == nil || calls.Load() != 1 || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), strings.TrimPrefix(scm.BasicAuth("operator@example.invalid", token), "Basic ")) {
				t.Fatal("failure hidden, leaked or retried", err, calls.Load())
			}
		})
	}
	m, _ := manager(t)
	_, err := m.Test(context.Background(), "missing")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("missing connection", err)
	}
}
