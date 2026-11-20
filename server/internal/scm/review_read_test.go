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

func TestPortablePullRequestRead(t *testing.T) {
	for _, kind := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
		for _, mode := range []string{"ok", "changed-head", "changed-base", "wrong-repository", "invalid-json", "denied", "oversize", "reflected-token", "truncated"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				fixtureToken := `fixture-<&"credential`
				reads := 0
				sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
						t.Error("missing server credential")
					}
					if mode == "denied" {
						w.WriteHeader(403)
						fmt.Fprint(w, fixtureToken)
						return
					}
					if r.Header.Get("Accept") == "application/vnd.github.diff" || strings.Contains(r.URL.Path, "/diff/") || strings.HasSuffix(r.URL.Path, "/diff") {
						if kind == "github" && !strings.HasSuffix(r.URL.Path, "/compare/"+base+"..."+sha) {
							t.Error("GitHub diff is not pinned")
						}
						if kind == "bitbucket-cloud" && (!strings.HasSuffix(r.URL.Path, "/diff/"+sha+".."+base) || r.URL.Query().Get("topic") != "true") {
							t.Error("Cloud diff order/topic not pinned")
						}
						if kind == "bitbucket-dc" && (r.URL.Query().Get("sinceId") != base || r.URL.Query().Get("untilId") != sha) {
							t.Error("DC diff is not pinned")
						}
						if mode == "oversize" {
							fmt.Fprint(w, strings.Repeat("x", 2<<20+1))
							return
						}
						if mode == "truncated" {
							if kind == "bitbucket-dc" {
								fmt.Fprintf(w, `{"fromHash":%q,"toHash":%q,"truncated":false,"diffs":[{"hunks":[{"truncated":"true"}]}]}`, base, sha)
							} else {
								w.WriteHeader(500)
							}
							return
						}
						if kind == "bitbucket-dc" {
							_ = json.NewEncoder(w).Encode(map[string]any{"fromHash": base, "toHash": sha, "truncated": false, "diffs": []any{map[string]any{"line": "+fixed " + fixtureToken, "truncated": false}}})
						} else {
							fmt.Fprint(w, "diff --git a/a.go b/a.go\n+fixed "+fixtureToken+"\n")
						}
						return
					}
					reads++
					head, bs := sha, base
					if reads > 1 && mode == "changed-head" {
						head = strings.Repeat("c", 40)
					}
					if reads > 1 && mode == "changed-base" {
						bs = strings.Repeat("d", 40)
					}
					repo := "acme/app"
					if mode == "wrong-repository" {
						repo = "fork/app"
					}
					if mode == "invalid-json" {
						fmt.Fprint(w, "{")
						return
					}
					switch kind {
					case "github":
						fmt.Fprintf(w, `{"number":7,"html_url":"https://scm.example/pr/7","state":"open","head":{"ref":"weft/fix","sha":%q,"repo":{"full_name":%q}},"base":{"ref":"main","sha":%q,"repo":{"full_name":"acme/app"}}}`, head, repo, bs)
					case "bitbucket-cloud":
						fmt.Fprintf(w, `{"id":7,"state":"OPEN","source":{"branch":{"name":"weft/fix"},"repository":{"full_name":%q},"commit":{"hash":%q}},"destination":{"branch":{"name":"main"},"repository":{"full_name":"acme/app"},"commit":{"hash":%q}},"links":{"html":{"href":"https://scm.example/pr/7"}}}`, repo, head, bs)
					case "bitbucket-dc":
						project, slug, _ := strings.Cut(repo, "/")
						fmt.Fprintf(w, `{"id":7,"state":"OPEN","fromRef":{"id":"refs/heads/weft/fix","latestCommit":%q,"repository":{"slug":%q,"project":{"key":%q}}},"toRef":{"id":"refs/heads/main","latestCommit":%q,"repository":{"slug":"app","project":{"key":"acme"}}},"links":{"self":[{"href":"https://scm.example/pr/7"}]}}`, head, slug, project, bs)
					}
				}))
				defer srv.Close()
				m, _ := manager(t)
				_, err := m.Save(ctx, scm.Input{Name: "review", Kind: kind, Repository: "acme/app", APIURL: srv.URL, Token: ptr(fixtureToken)}, "admin", false)
				if err != nil {
					t.Fatal(err)
				}
				reg := tools.NewRegistry()
				reg.MustRegister(scm.Tools(m)...)
				gw := tools.NewGateway(reg, nil, nil)
				grant := domain.ToolGrant{Name: "scm.pull_request.read", Repositories: []string{"review"}}
				result, err := gw.Invoke(ctx, principal("", grant), "scm.pull_request.read", json.RawMessage(`{"connection":"review","number":7,"diff":true}`))
				if err != nil || result == nil {
					t.Fatalf("%+v %v", result, err)
				}
				if mode == "ok" || mode == "reflected-token" {
					var outer struct {
						Diff string `json:"diff"`
					}
					if json.Unmarshal(result.Data, &outer) != nil {
						t.Fatal("invalid result JSON")
					}
					if kind == "bitbucket-dc" {
						var nested map[string]any
						if json.Unmarshal([]byte(outer.Diff), &nested) != nil {
							t.Fatal("invalid nested DC diff")
						}
						line := nested["diffs"].([]any)[0].(map[string]any)["line"].(string)
						if strings.Contains(line, fixtureToken) {
							t.Fatal("credential reflected after decoding structured diff")
						}
					} else if strings.Contains(outer.Diff, fixtureToken) {
						t.Fatal("credential reflected after decoding raw diff")
					}
					if !json.Valid(result.Data) || strings.Contains(string(result.Data), fixtureToken) || !strings.Contains(string(result.Data), "[redacted]") {
						t.Fatalf("credential reflection: %+v", result)
					}
					if result.IsError || !strings.Contains(string(result.Data), "+fixed") || !strings.Contains(string(result.Data), sha) || reads != 2 {
						t.Fatalf("%+v reads=%d", result, reads)
					}
				} else if !result.IsError || strings.Contains(result.Content, fixtureToken) {
					t.Fatalf("unsafe failure: %+v", result)
				}
				before := reads
				_, err = gw.Invoke(ctx, principal("", domain.ToolGrant{Name: grant.Name, Repositories: []string{"other"}}), grant.Name, json.RawMessage(`{"connection":"review","number":7}`))
				if err == nil || reads != before {
					t.Fatal("scope escape")
				}
			})
		}
	}
}

func TestPortableReadUnavailableAdapterAndInvalidNumber(t *testing.T) {
	m, _ := manager(t)
	if _, err := m.Save(ctx, scm.Input{Name: "review", Kind: "fake", Repository: "acme/app", Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	gw := tools.NewGateway(reg, nil, nil)
	grant := domain.ToolGrant{Name: "scm.pull_request.read", Repositories: []string{"review"}}
	for _, body := range []string{`{"connection":"review","number":7}`, `{"connection":"review","number":0}`} {
		result, err := gw.Invoke(ctx, principal("", grant), grant.Name, json.RawMessage(body))
		if err == nil && (result == nil || !result.IsError) {
			t.Fatalf("unsupported/invalid succeeded: %+v", result)
		}
	}
}
