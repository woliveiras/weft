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
					case kind == "bitbucket-cloud" && path == "/repositories/acme/app/commit/"+sha+"/statuses":
						if r.URL.Query().Get("pagelen") != "100" {
							t.Error("unexpected status collection page size")
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

func TestBitbucketCloudCheckCollectionThroughGateway(t *testing.T) {
	for _, mode := range []string{"ok", "empty", "second-page", "denied", "malformed", "foreign-page", "filtered-page", "page-limit", "changed"} {
		t.Run(mode, func(t *testing.T) {
			sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
			reads, pages, writes := 0, 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					http.Error(w, "unexpected write", 405)
					return
				}
				if r.Header.Get("Authorization") != scm.BasicAuth("operator@example.invalid", token) {
					t.Error("wrong API-token credential")
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/2.0/repositories/acme/app/pullrequests/7" {
					reads++
					head := sha
					if mode == "changed" && reads > 1 {
						head = strings.Repeat("c", 40)
					}
					fmt.Fprintf(w, `{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"links":{"html":{"href":"https://bitbucket.org/acme/app/pull-requests/7"}}}`, head, base)
					return
				}
				if r.URL.Path != "/2.0/repositories/acme/app/commit/"+sha+"/statuses" {
					t.Error("requested something other than the pinned status collection", r.URL.Path)
					http.Error(w, "unsupported collection", 403)
					return
				}
				pages++
				q := r.URL.Query()
				if q.Get("pagelen") != "100" || q.Get("q") != "" || q.Get("refname") != "" {
					t.Error("status collection was filtered or unbounded")
				}
				if mode == "denied" {
					w.WriteHeader(403)
					fmt.Fprint(w, token)
					return
				}
				if mode == "malformed" {
					fmt.Fprint(w, `{"values":null}`)
					return
				}
				next := ""
				switch mode {
				case "second-page":
					if pages == 1 {
						next = fmt.Sprintf("http://%s%s?pagelen=100&page=2", r.Host, r.URL.Path)
					}
				case "foreign-page":
					next = "https://foreign.example/credentials"
				case "filtered-page":
					next = fmt.Sprintf("http://%s%s?pagelen=100&q=state=SUCCESSFUL", r.Host, r.URL.Path)
				case "page-limit":
					next = fmt.Sprintf("http://%s%s?pagelen=100&page=%d", r.Host, r.URL.Path, pages+1)
				}
				rows := []map[string]string{}
				if mode != "empty" {
					rows = append(rows, map[string]string{"key": "build-" + token, "state": "SUCCESSFUL"})
				}
				if mode == "second-page" && pages == 2 {
					rows = []map[string]string{{"key": "test", "state": "FAILED"}}
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"values": rows, "next": next}); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			m, _ := manager(t)
			if _, err := m.Save(ctx, scm.Input{Name: "bb", Kind: "bitbucket-cloud", Repository: "acme/app", APIURL: srv.URL + "/2.0", AuthType: "api-token", Username: "operator@example.invalid", Token: ptr(token)}, "admin", false); err != nil {
				t.Fatal(err)
			}
			out, err := gateway(t, m).Invoke(ctx, principal("", domain.ToolGrant{Name: "scm.pull_request.read", Repositories: []string{"bb"}}), "scm.pull_request.read", json.RawMessage(`{"connection":"bb","number":7,"checks":true}`))
			valid := mode == "ok" || mode == "empty" || mode == "second-page"
			if err != nil || out == nil || out.IsError == valid || writes != 0 {
				t.Fatalf("checks: %+v error=%v writes=%d", out, err, writes)
			}
			if strings.Contains(out.Content+string(out.Data), token) {
				t.Fatal("credential escaped checks")
			}
			wantPages := 1
			if mode == "second-page" {
				wantPages = 2
			}
			if mode == "page-limit" {
				wantPages = 10
			}
			if pages != wantPages {
				t.Fatalf("page count=%d want=%d", pages, wantPages)
			}
			if valid {
				var result struct {
					Checks scm.ReviewChecks `json:"checks"`
				}
				if json.Unmarshal(out.Data, &result) != nil || !result.Checks.Available || !result.Checks.Complete || result.Checks.RequiredRules != "unavailable" || reads != 2 {
					t.Fatalf("incomplete or unsafe evidence: %+v", out)
				}
				wantItems := 1
				if mode == "empty" {
					wantItems = 0
				}
				if mode == "second-page" {
					wantItems = 2
				}
				if len(result.Checks.Items) != wantItems || mode == "second-page" && result.Checks.Items[1].State != "failed" {
					t.Fatalf("check results lost: %+v", result.Checks)
				}
			} else if len(out.Data) != 0 {
				t.Fatal("failed evidence exposed a partial success")
			}
		})
	}
}
