package scm_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestBitbucketCloudAbbreviatedRevisionsThroughGateway(t *testing.T) {
	for _, mode := range []string{"ok", "denied", "prefix-mismatch", "foreign-repository", "missing", "changed-head", "changed-base"} {
		t.Run(mode, func(t *testing.T) {
			head, base, changed := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
			posts, prReads, commits := 0, 0, 0
			created := false
			pull := func() string {
				h, b := head[:12], base[:12]
				if mode == "missing" {
					h = ""
				}
				if prReads > 1 && mode == "changed-head" {
					h = changed[:12]
				}
				if prReads > 1 && mode == "changed-base" {
					b = changed[:12]
				}
				return fmt.Sprintf(`{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"commit":{"hash":%q},"repository":{"full_name":"acme/app"}},"destination":{"branch":{"name":"main"},"commit":{"hash":%q},"repository":{"full_name":"acme/app"}},"links":{"html":{"href":"https://bitbucket.org/acme/app/pull-requests/7"}}}`, h, b)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != scm.BasicAuth("operator@example.invalid", token) {
					t.Error("wrong connection credential")
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/2.0/repositories/acme/app":
					fmt.Fprint(w, `{"full_name":"acme/app","mainbranch":{"name":"main"}}`)
				case strings.Contains(r.URL.Path, "/permissions/"):
					fmt.Fprint(w, `{"values":[{"repository":{"full_name":"acme/app"},"user":{"nickname":"bot"},"permission":"write"}]}`)
				case r.URL.Path == "/2.0/repositories/acme/app/pullrequests":
					if r.Method == "POST" {
						posts++
						created = true
						w.WriteHeader(http.StatusCreated)
						fmt.Fprint(w, pull())
					} else if created {
						fmt.Fprintf(w, `{"values":[%s]}`, pull())
					} else {
						fmt.Fprint(w, `{"values":[]}`)
					}
				case r.URL.Path == "/2.0/repositories/acme/app/pullrequests/7":
					prReads++
					fmt.Fprint(w, pull())
				case strings.HasPrefix(r.URL.Path, "/2.0/repositories/acme/app/commit/"):
					commits++
					if r.Method != "GET" {
						t.Error("resolution wrote to provider")
					}
					if mode == "denied" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprintf(w, `{"error":{"message":%q}}`, token)
						return
					}
					prefix := strings.TrimPrefix(r.URL.Path, "/2.0/repositories/acme/app/commit/")
					hash := ""
					for _, candidate := range []string{head, base, changed} {
						if prefix == candidate[:12] {
							hash = candidate
						}
					}
					if hash == "" {
						t.Error("resolution used a mutable or unknown revision")
					}
					repo := "acme/app"
					if mode == "prefix-mismatch" {
						hash = changed
					}
					if mode == "foreign-repository" {
						repo = "fork/app"
					}
					fmt.Fprintf(w, `{"hash":%q,"repository":{"full_name":%q}}`, hash, repo)
				case strings.HasPrefix(r.URL.Path, "/2.0/repositories/acme/app/diff/"):
					if r.URL.Path != "/2.0/repositories/acme/app/diff/"+head+".."+base {
						t.Error("diff was not pinned to full revisions")
					}
					fmt.Fprint(w, "diff --git a/a.go b/a.go\n+fixed\n")
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			m, st := manager(t)
			m.Bus = events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if _, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: srv.URL + "/2.0", AuthType: "api-token", Username: "operator@example.invalid", Token: ptr(token), RequirePullRequestApproval: ptr(false)}, "admin", false); err != nil {
				t.Fatal(err)
			}
			gw := gateway(t, m)
			createPrincipal := principal("", domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"bb"}})
			resolvable := mode == "ok" || strings.HasPrefix(mode, "changed-")
			for attempt := 0; attempt < 2; attempt++ {
				out, err := gw.Invoke(ctx, createPrincipal, "scm.pull_request.create", json.RawMessage(`{"connection":"bb","branch":"weft/fix","title":"Fix"}`))
				if err != nil || out == nil || out.IsError {
					t.Fatalf("delivery lost: %+v %v", out, err)
				}
				var pr scm.PullRequest
				if json.Unmarshal(out.Data, &pr) != nil || pr.Number != 7 || pr.Created != (attempt == 0) {
					t.Fatalf("delivery identity: %+v", pr)
				}
				if resolvable && (pr.HeadSHA != head || pr.BaseSHA != base) || !resolvable && (pr.HeadSHA != "" || pr.BaseSHA != "" || !strings.Contains(out.Content, "Automatic review is unavailable")) {
					t.Fatalf("unsafe revision result: %+v", out)
				}
				if strings.Contains(out.Content+string(out.Data), token) {
					t.Fatal("credential in delivery")
				}
			}
			evs, err := st.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvPullRequestOpened}})
			if err != nil || len(evs) != 2 {
				t.Fatalf("delivery events: %d %v", len(evs), err)
			}
			for _, ev := range evs {
				var data map[string]any
				if json.Unmarshal(ev.Data, &data) != nil || data["reviewRevisionAvailable"] != resolvable {
					t.Fatalf("unsafe event: %s", ev.Data)
				}
				if resolvable && (data["headSha"] != head || data["baseSha"] != base) || !resolvable && data["reviewRevisionError"] != "commit_resolution_failed" {
					t.Fatalf("revision evidence: %s", ev.Data)
				}
				if strings.Contains(string(ev.Data), token) {
					t.Fatal("credential in event")
				}
			}
			out, err := gw.Invoke(ctx, principal("", domain.ToolGrant{Name: "scm.pull_request.read", Repositories: []string{"bb"}}), "scm.pull_request.read", json.RawMessage(`{"connection":"bb","number":7,"diff":true}`))
			if err != nil || out == nil || out.IsError != (mode != "ok") {
				t.Fatalf("review read: %+v %v", out, err)
			}
			if strings.Contains(out.Content+string(out.Data), token) {
				t.Fatal("credential in read")
			}
			if posts != 1 || mode == "missing" && commits != 0 {
				t.Fatalf("unexpected writes/resolution: posts=%d commits=%d", posts, commits)
			}
		})
	}
}
