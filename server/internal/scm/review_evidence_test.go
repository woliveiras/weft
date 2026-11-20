package scm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestPortableReviewEvidence(t *testing.T) {
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		for _, mode := range []string{"ok", "empty", "unknown", "denied", "incomplete", "changed", "escape", "filter", "topic", "limit"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
				reads := 0
				requests := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.Method != "GET" {
						t.Error("review evidence sent a mutation")
					}
					if r.Header.Get("Authorization") != "Bearer "+token {
						t.Error("credential scope")
					}
					path := r.URL.Path
					if strings.HasSuffix(path, "/7") {
						reads++
						head := sha
						if reads > 1 && mode == "changed" {
							head = strings.Repeat("c", 40)
						}
						switch kind {
						case "github":
							fmt.Fprintf(w, `{"number":7,"html_url":"https://scm.example/pr/7","state":"open","head":{"ref":"weft/fix","sha":%q,"repo":{"full_name":"acme/app"}},"base":{"ref":"main","sha":%q,"repo":{"full_name":"acme/app"}}}`, head, base)
						case "bitbucket-cloud":
							fmt.Fprintf(w, `{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"links":{"html":{"href":"https://scm.example/pr/7"}}}`, head, base)
						case "bitbucket-dc":
							fmt.Fprintf(w, `{"id":7,"state":"OPEN","fromRef":{"id":"refs/heads/weft/fix","latestCommit":%q,"repository":{"slug":"app","project":{"key":"acme"}}},"toRef":{"id":"refs/heads/main","latestCommit":%q,"repository":{"slug":"app","project":{"key":"acme"}}},"links":{"self":[{"href":"https://scm.example/pr/7"}]}}`, head, base)
						}
						return
					}
					if mode == "denied" {
						w.WriteHeader(403)
						return
					}
					if mode == "incomplete" {
						fmt.Fprint(w, `{}`)
						return
					}
					state := "SUCCESSFUL"
					if mode == "unknown" {
						state = "NEW_VENDOR_STATE"
					}
					switch {
					case strings.Contains(path, "/compare/"):
						if mode == "empty" {
							fmt.Fprintf(w, `{"base_commit":{"sha":%q},"files":[]}`, base)
						} else if mode == "limit" {
							rows := make([]map[string]any, 300)
							for i := range rows {
								rows[i] = map[string]any{"filename": fmt.Sprint(i), "status": "modified"}
							}
							json.NewEncoder(w).Encode(map[string]any{"base_commit": map[string]string{"sha": base}, "files": rows})
						} else {
							fmt.Fprintf(w, `{"base_commit":{"sha":%q},"files":[{"filename":%q,"previous_filename":"old.go","status":"renamed"}]}`, base, "new-"+token+".go")
						}
					case strings.Contains(path, "/diffstat/"):
						if !strings.HasSuffix(path, sha+".."+base) || r.URL.Query().Get("topic") != "true" {
							t.Error("file inventory revisions not pinned")
						}
						next := ""
						if mode == "filter" {
							next = "/repositories/acme/app/diffstat/" + sha + ".." + base + "?topic=true&path=only.go"
						}
						if mode == "topic" {
							next = "/repositories/acme/app/diffstat/" + sha + ".." + base + "?topic=false"
						}
						if mode == "escape" {
							next = "https://foreign.example/leak"
						}
						if mode == "limit" {
							next = "?page=2"
							next = "/repositories/acme/app/diffstat/" + sha + ".." + base + "?topic=true&page=2"
						}
						rows := []any{}
						if mode != "empty" {
							rows = append(rows, map[string]any{"status": "renamed", "old": map[string]string{"path": "old.go"}, "new": map[string]string{"path": "new-" + token + ".go"}})
						}
						json.NewEncoder(w).Encode(map[string]any{"values": rows, "next": next})
					case strings.HasSuffix(path, "/check-runs"):
						rows := []any{}
						if mode != "empty" {
							conclusion := "success"
							if mode == "unknown" {
								conclusion = "skipped"
							}
							rows = append(rows, map[string]any{"name": "lint", "head_sha": sha, "status": "completed", "conclusion": conclusion})
						}
						json.NewEncoder(w).Encode(map[string]any{"check_runs": rows, "total_count": len(rows)})
					case strings.HasSuffix(path, "/status"):
						rows := []any{}
						if mode != "empty" {
							s := "success"
							if mode == "unknown" {
								s = "unrecognized"
							}
							rows = append(rows, map[string]any{"context": "build-" + token, "state": s})
						}
						json.NewEncoder(w).Encode(map[string]any{"sha": sha, "total_count": len(rows), "statuses": rows})
					case strings.Contains(path, "/statuses/build"):
						if !strings.Contains(path, sha) {
							t.Error("build request not pinned")
						}
						rows := []any{}
						if mode != "empty" {
							rows = append(rows, map[string]any{"key": "build-" + token, "state": state})
						}
						json.NewEncoder(w).Encode(map[string]any{"values": rows})
					case strings.Contains(path, "/rest/build-status/1.0/commits/"):
						if !strings.HasSuffix(path, sha) {
							t.Error("DC build request not pinned")
						}
						rows := []any{}
						if mode != "empty" {
							rows = append(rows, map[string]any{"key": "build-" + token, "state": state})
						}
						last := true
						next := 100
						if mode == "limit" {
							last = false
						}
						json.NewEncoder(w).Encode(map[string]any{"values": rows, "isLastPage": last, "nextPageStart": next})
					default:
						t.Error("unexpected path", path)
						w.WriteHeader(404)
					}
				}))
				defer srv.Close()
				m, _ := manager(t)
				if _, err := m.Save(ctx, scm.Input{Name: "review", Kind: kind, Repository: "acme/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false); err != nil {
					t.Fatal(err)
				}
				reg := tools.NewRegistry()
				reg.MustRegister(scm.Tools(m)...)
				gw := tools.NewGateway(reg, nil, nil)
				grant := domain.ToolGrant{Name: "scm.pull_request.read", Repositories: []string{"review"}}
				out, err := gw.Invoke(ctx, principal("", grant), grant.Name, json.RawMessage(`{"connection":"review","number":7,"files":true,"checks":true}`))
				if err != nil || out == nil {
					t.Fatal(err, out)
				}
				success := mode == "ok" || mode == "empty" || mode == "unknown" || ((mode == "escape" || mode == "filter" || mode == "topic") && kind != "bitbucket-cloud")
				if out.IsError == success {
					t.Fatalf("unexpected outcome %s %+v", mode, out)
				}
				if success {
					var got struct {
						Files  scm.ReviewFiles
						Checks scm.ReviewChecks
					}
					if json.Unmarshal(out.Data, &got) != nil {
						t.Fatal("invalid evidence")
					}
					if !got.Checks.Available || (kind != "bitbucket-dc" && !got.Checks.Complete) || got.Checks.RequiredRules != "unavailable" {
						t.Fatal(got)
					}
					if kind == "bitbucket-dc" {
						if got.Files.Available || got.Files.Complete || got.Files.Reason == "" {
							t.Fatal("false DC inventory coverage")
						}
					} else if !got.Files.Available || !got.Files.Complete {
						t.Fatal(got.Files)
					}
					if mode == "empty" && len(got.Checks.Items) != 0 {
						t.Fatal(got.Checks)
					}
					if mode == "unknown" {
						for _, c := range got.Checks.Items {
							if c.State != "unknown" {
								t.Fatal("unknown became pass", c)
							}
						}
					}
					if strings.Contains(string(out.Data), token) || reads != 2 {
						t.Fatal("redaction/revision fence", string(out.Data), reads)
					}
				}
				before := requests
				_, err = gw.Invoke(ctx, principal("", domain.ToolGrant{Name: grant.Name, Repositories: []string{"foreign"}}), grant.Name, json.RawMessage(`{"connection":"review","number":7,"checks":true}`))
				if err == nil || before != requests {
					t.Fatal("scope escape")
				}
			})
		}
	}
}
