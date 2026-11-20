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
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestPortableDeliveryEventAndConnectionRevision(t *testing.T) {
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		for _, mode := range []string{"normal", "reflected", "legacy"} {
			t.Run(fmt.Sprintf("%s/%s", kind, mode), func(t *testing.T) {
				reflected := mode == "reflected"
				fixtureToken := token
				if reflected {
					fixtureToken = strings.Repeat("a", 40)
				}
				sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
				if mode == "legacy" {
					sha = ""
					base = ""
				}
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
						t.Error("credential scope")
					}
					path := r.URL.Path
					if r.Method == "POST" || strings.HasSuffix(r.URL.Path, "/7") {
						switch kind {
						case "github":
							fmt.Fprintf(w, `{"number":7,"html_url":"https://scm.example/pr/7","state":"open","head":{"ref":"weft/fix","sha":%q,"repo":{"full_name":"acme/app"}},"base":{"ref":"main","sha":%q,"repo":{"full_name":"acme/app"}}}`, sha, base)
						case "bitbucket-cloud":
							fmt.Fprintf(w, `{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"commit":{"hash":%q},"repository":{"full_name":"acme/app"}},"destination":{"branch":{"name":"main"},"commit":{"hash":%q},"repository":{"full_name":"acme/app"}},"links":{"html":{"href":"https://scm.example/pr/7"}}}`, sha, base)
						case "bitbucket-dc":
							fmt.Fprintf(w, `{"id":7,"state":"OPEN","fromRef":{"id":"refs/heads/weft/fix","latestCommit":%q,"repository":{"slug":"app","project":{"key":"acme"}}},"toRef":{"id":"refs/heads/main","latestCommit":%q,"repository":{"slug":"app","project":{"key":"acme"}}},"links":{"self":[{"href":"https://scm.example/pr/7"}]}}`, sha, base)
						}
						return
					}
					switch {
					case path == "/user":
						fmt.Fprint(w, `{"login":"bot"}`)
					case strings.Contains(path, "/permissions/"):
						fmt.Fprint(w, `{"values":[{"repository":{"full_name":"acme/app"},"user":{"nickname":"bot"},"permission":"write"}]}`)
					case strings.HasSuffix(path, "/branches/default"):
						fmt.Fprint(w, `{"displayId":"main","id":"refs/heads/main"}`)
					case path == "/rest/api/1.0/repos":
						fmt.Fprint(w, `{"isLastPage":true,"values":[{"id":42,"slug":"app","name":"App","project":{"key":"acme"}}]}`)
					case strings.HasSuffix(path, "/pulls"):
						fmt.Fprint(w, `[]`)
					case strings.HasSuffix(path, "/pullrequests"):
						fmt.Fprint(w, `{"values":[]}`)
					case strings.HasSuffix(path, "/pull-requests"):
						fmt.Fprint(w, `{"isLastPage":true,"values":[]}`)
					default:
						fmt.Fprint(w, `{"full_name":"acme/app","default_branch":"main","permissions":{"push":true},"mainbranch":{"name":"main"},"id":42,"slug":"app","name":"App","project":{"key":"acme"}}`)
					}
				}))
				defer srv.Close()
				m, st := manager(t)
				m.Bus = events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
				_, err := m.Save(ctx, scm.Input{Name: "team", Kind: kind, Repository: "acme/app", APIURL: srv.URL, Token: ptr(fixtureToken), RequirePullRequestApproval: ptr(false)}, "admin", false)
				if err != nil {
					t.Fatal(err)
				}
				gw := gateway(t, m)
				out, err := gw.Invoke(ctx, principal("", domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"team"}}), "scm.pull_request.create", json.RawMessage(`{"connection":"team","branch":"weft/fix","title":"Fix"}`))
				if err != nil || out.IsError {
					t.Fatalf("create %+v %v", out, err)
				}
				evs, err := st.ListEvents(ctx, storage.EventFilter{Types: []domain.EventType{domain.EvPullRequestOpened}})
				if err != nil || len(evs) != 1 {
					t.Fatalf("events %+v %v", evs, err)
				}
				var event map[string]any
				if json.Unmarshal(evs[0].Data, &event) != nil {
					t.Fatal("event JSON")
				}
				c, _ := m.Get(ctx, "team")
				rev := c.UpdatedAt.UTC().Format(time.RFC3339Nano)
				expectedHead := sha
				if reflected {
					expectedHead = ""
				}
				if event["headSha"] != expectedHead || event["baseSha"] != base || event["provider"] != kind || event["instance"] != srv.URL || event["connectionRevision"] != rev || event["reviewRevisionAvailable"] != (mode == "normal") {
					t.Fatalf("identity %+v", event)
				}
				if strings.Contains(string(evs[0].Data), fixtureToken) || strings.Contains(string(out.Data), fixtureToken) || strings.Contains(out.Content, fixtureToken) {
					t.Fatal("credential in event")
				}
				if mode == "normal" {
					raw := json.RawMessage(fmt.Sprintf(`{"connection":"team","connectionRevision":%q,"number":7}`, rev))
					out, err = gw.Invoke(ctx, principal("", domain.ToolGrant{Name: "scm.pull_request.read", Repositories: []string{"team"}}), "scm.pull_request.read", raw)
					if err != nil || out.IsError || !strings.Contains(string(out.Data), rev) {
						t.Fatalf("matching revision %+v %v", out, err)
					}
				}
				// A real configuration change must invalidate the event's prior identity.
				if _, err = m.Save(ctx, scm.Input{Name: "team", Kind: kind, Repository: "acme/app", APIURL: srv.URL + "/changed"}, "admin", true); err != nil {
					t.Fatal(err)
				}
				before := calls
				for _, tool := range []string{"scm.pull_request.read", "scm.pull_request.review"} {
					raw := json.RawMessage(fmt.Sprintf(`{"connection":"team","connectionRevision":%q,"number":7,"headSha":%q,"baseSha":%q,"verdict":"approved","summary":"No findings."}`, rev, strings.Repeat("a", 40), strings.Repeat("b", 40)))
					out, err = gw.Invoke(ctx, principal("", domain.ToolGrant{Name: tool, Repositories: []string{"team"}}), tool, raw)
					if err != nil || out == nil || !out.IsError || !strings.Contains(out.Content, "connection changed") {
						t.Fatalf("stale %s: %+v %v", tool, out, err)
					}
				}
				if calls != before {
					t.Fatal("stale connection contacted provider")
				}
			})
		}
	}
}
