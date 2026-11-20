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
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestPortableDeliveryEventAndConnectionRevision(t *testing.T) {
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		for _, mode := range []string{"normal", "reflected", "legacy"} {
			for _, sourceKind := range []string{"github", "jira"} {
				t.Run(fmt.Sprintf("%s/%s/%s", kind, mode, sourceKind), func(t *testing.T) {
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
					receivedBody := ""
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
							t.Error("credential scope")
						}
						path := r.URL.Path
						if r.Method == "POST" {
							var in map[string]any
							if json.NewDecoder(r.Body).Decode(&in) != nil {
								t.Error("invalid create body")
							}
							field := "description"
							if kind == "github" {
								field = "body"
							}
							receivedBody, _ = in[field].(string)
						}
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
					g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "flow"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker"}}}}
					if _, err = st.SaveGraph(ctx, g, 0); err != nil {
						t.Fatal(err)
					}
					origin := taskconfig.IssueOrigin{Kind: sourceKind, ConnectionID: "tracker-team", ConnectionRevision: 9}
					if sourceKind == "github" {
						origin.Instance = "https://api.github.com"
						origin.Key = "acme/issues#7"
						origin.URL = "https://github.com/acme/issues/issues/7"
					} else {
						origin.Instance = "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555"
						origin.Key = "APP-7"
						origin.URL = "https://team.atlassian.net/browse/APP-7"
					}
					tasks := taskconfig.Manager{Store: st}
					draft, err := tasks.SaveTriggeredIssue(ctx, taskconfig.Input{Title: "Fix", Request: "Work", Outcome: "implement", GraphID: "flow", GraphVersion: 1, NodeID: "worker", SCMConnection: "team"}, "admin", origin)
					if err != nil {
						t.Fatal(err)
					}
					draft.ExecutionGraphID = domain.TaskRunGraphPrefix + strings.TrimPrefix(draft.ID, "tsk_")
					if _, err = tasks.Transition(ctx, draft, "running", "admin"); err != nil {
						t.Fatal(err)
					}
					requestPrincipal := principal("", domain.ToolGrant{Name: "scm.pull_request.create", Repositories: []string{"team"}})
					requestPrincipal.GraphID = draft.ExecutionGraphID
					gw := gateway(t, m)
					out, err := gw.Invoke(ctx, requestPrincipal, "scm.pull_request.create", json.RawMessage(`{"connection":"team","branch":"weft/fix","title":"Fix"}`))
					if err != nil || out.IsError {
						t.Fatalf("create %+v %v", out, err)
					}
					if receivedBody != "Source issue: "+origin.URL || strings.Contains(receivedBody, "Closes") {
						t.Fatalf("provider pair body %q", receivedBody)
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
}
