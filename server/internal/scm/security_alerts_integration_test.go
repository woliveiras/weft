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

const dependabotFixture = `{"number":2,"state":"open","dependency":{"package":{"name":"undici","ecosystem":"npm"},"manifest_path":"web/pnpm-lock.yaml","scope":"development","relationship":"transitive"},"security_advisory":{"ghsa_id":"GHSA-fixture","cve_id":"CVE-fixture","severity":"high","summary":"fixture"},"security_vulnerability":{"vulnerable_version_range":"< 8.12.2","first_patched_version":{"identifier":"8.12.2"}}}`

func TestSecurityAlertsGatewayHTTP(t *testing.T) {
	for _, mode := range []string{"ok", "empty", "unpatched", "denied", "unauthorized", "not-found", "rate-limit", "invalid-json", "null", "wrong-state", "wrong-pr", "oversize", "redirect", "reflected-token", "later-page-failure", "page-limit", "pr-failure", "pr-null", "pr-later-failure", "pr-page-limit"} {
		t.Run(mode, func(t *testing.T) {
			calls, mutations, redirected := 0, 0, 0
			var branch string
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++; w.WriteHeader(500) }))
			defer target.Close()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" {
					mutations++
					w.WriteHeader(500)
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("missing scoped server credential")
				}
				if strings.HasSuffix(r.URL.Path, "/dependabot/alerts") {
					if r.URL.Path != "/repos/acme/app/dependabot/alerts" || r.URL.Query().Get("state") != "open" || r.URL.Query().Get("per_page") != "100" {
						t.Error("unbounded alert query", r.URL)
					}
					switch mode {
					case "denied":
						w.WriteHeader(403)
						fmt.Fprint(w, token)
						return
					case "unauthorized":
						w.WriteHeader(401)
						return
					case "not-found":
						w.WriteHeader(404)
						return
					case "rate-limit":
						w.Header().Set("Retry-After", "60")
						w.WriteHeader(429)
						return
					case "invalid-json":
						fmt.Fprint(w, "{")
						return
					case "null":
						fmt.Fprint(w, "null")
						return
					case "oversize":
						fmt.Fprint(w, strings.Repeat("x", (2<<20)+1))
						return
					case "redirect":
						http.Redirect(w, r, target.URL, 302)
						return
					case "empty":
						fmt.Fprint(w, "[]")
						return
					case "later-page-failure", "page-limit":
						if r.URL.Query().Get("page") != "1" && mode == "later-page-failure" {
							w.WriteHeader(503)
							return
						}
						items := make([]json.RawMessage, 100)
						for i := range items {
							items[i] = json.RawMessage(strings.Replace(dependabotFixture, `"number":2`, fmt.Sprintf(`"number":%d`, calls*100+i), 1))
						}
						_ = json.NewEncoder(w).Encode(items)
						return
					}
					item := dependabotFixture
					if mode == "unpatched" {
						item = strings.Replace(item, `{"identifier":"8.12.2"}`, "null", 1)
					}
					if mode == "wrong-state" {
						item = strings.Replace(item, `"state":"open"`, `"state":"fixed"`, 1)
					}
					if mode == "reflected-token" {
						item = strings.Replace(item, `"vulnerable_version_range":"< 8.12.2"`, `"vulnerable_version_range":"< \u0073cm-secret-token"`, 1)
					}
					fmt.Fprint(w, "["+item+"]")
					return
				}
				if strings.HasSuffix(r.URL.Path, "/pulls") {
					if r.URL.Query().Get("base") != "main" || r.URL.Query().Get("state") != "open" {
						t.Error("unscoped PR query")
					}
					switch mode {
					case "pr-failure":
						w.WriteHeader(503)
						return
					case "pr-null":
						fmt.Fprint(w, "null")
						return
					case "pr-later-failure", "pr-page-limit":
						if r.URL.Query().Get("page") != "1" && mode == "pr-later-failure" {
							w.WriteHeader(503)
							return
						}
						items := make([]map[string]any, 100)
						for i := range items {
							items[i] = map[string]any{"number": calls*100 + i, "state": "open", "head": map[string]any{"ref": "weft/other", "repo": map[string]any{"full_name": "acme/app"}}, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "acme/app"}}, "html_url": "https://github.com/acme/app/pull/1"}
						}
						_ = json.NewEncoder(w).Encode(items)
						return
					}
					if branch == "" {
						fmt.Fprint(w, "[]")
						return
					}
					base := "main"
					if mode == "wrong-pr" {
						base = "release"
					}
					fmt.Fprintf(w, `[{"number":9,"state":"open","html_url":"https://github.com/acme/app/pull/9","head":{"ref":%q,"repo":{"full_name":"acme/app"}},"base":{"ref":%q,"repo":{"full_name":"acme/app"}}}]`, branch, base)
					return
				}
				t.Errorf("unexpected HTTP path %s", r.URL.Path)
				w.WriteHeader(404)
			}))
			defer srv.Close()
			m, _ := manager(t)
			if _, err := m.Save(ctx, scm.Input{Name: "security", Kind: "github", Repository: "acme/app", APIURL: srv.URL, Token: ptr(token)}, "admin", false); err != nil {
				t.Fatal(err)
			}
			reg := tools.NewRegistry()
			reg.MustRegister(scm.Tools(m)...)
			gw := tools.NewGateway(reg, nil, nil)
			grant := domain.ToolGrant{Name: "scm.security.alerts", Repositories: []string{"security"}}
			args := json.RawMessage(`{"connection":"security","baseBranch":"main"}`)
			if _, err := gw.Invoke(ctx, principal("", domain.ToolGrant{Name: grant.Name, Repositories: []string{"other"}}), grant.Name, args); err == nil || calls != 0 {
				t.Fatal("wrong grant dispatched", calls, err)
			}
			invoke := func() *tools.Result {
				t.Helper()
				res, err := gw.Invoke(ctx, principal("", grant), grant.Name, args)
				if err != nil || res == nil {
					t.Fatal(res, err)
				}
				return res
			}
			for _, bad := range []string{"", "../bad", "-main", "refs/heads/../main"} {
				raw, _ := json.Marshal(map[string]string{"connection": "security", "baseBranch": bad})
				invalid, err := gw.Invoke(ctx, principal("", grant), grant.Name, raw)
				if (err == nil && (invalid == nil || !invalid.IsError)) || calls != 0 {
					t.Fatal("invalid base dispatched", bad, invalid, err, calls)
				}
			}
			res := invoke()
			success := mode == "ok" || mode == "empty" || mode == "unpatched" || mode == "reflected-token" || mode == "wrong-pr"
			if res.IsError == success || strings.Contains(res.Content, token) || strings.Contains(string(res.Data), token) || mutations != 0 || redirected != 0 {
				t.Fatalf("unsafe outcome: %+v calls=%d writes=%d redirected=%d", res, calls, mutations, redirected)
			}
			if success {
				var decoded any
				if json.Unmarshal(res.Data, &decoded) != nil {
					t.Fatal("invalid output JSON")
				}
				normalized, _ := json.Marshal(decoded)
				if strings.Contains(string(normalized), token) {
					t.Fatal("decoded credential reflection")
				}
				if mode == "reflected-token" && !strings.Contains(string(normalized), "[redacted]") {
					t.Fatal("reflection fixture did not reach output", string(normalized))
				}
				var report struct {
					Groups []struct {
						Branch               string
						Alerts               []struct{ FirstPatchedVersion string }
						ExistingPullRequests []struct{ Number int }
					}
				}
				if err := json.Unmarshal(res.Data, &report); err != nil {
					t.Fatal(err)
				}
				if mode == "empty" {
					if report.Groups == nil || len(report.Groups) != 0 {
						t.Fatal(report)
					}
					return
				}
				if len(report.Groups) != 1 || len(report.Groups[0].Alerts) != 1 {
					t.Fatal(report)
				}
				branch = report.Groups[0].Branch
				if mode == "unpatched" && report.Groups[0].Alerts[0].FirstPatchedVersion != "" {
					t.Fatal("invented patch")
				}
				res = invoke()
				if res.IsError || json.Unmarshal(res.Data, &report) != nil {
					t.Fatal(res)
				}
				want := 1
				if mode == "wrong-pr" {
					want = 0
				}
				if len(report.Groups[0].ExistingPullRequests) != want {
					t.Fatal("duplicate PR detection", report)
				}
			} else if len(res.Data) != 0 {
				t.Fatal("partial successful data returned", res)
			}
			if mode == "later-page-failure" && calls != 2 {
				t.Fatal("later page not checked", calls)
			}
			if mode == "pr-later-failure" && calls != 3 {
				t.Fatal("PR later page not checked", calls)
			}
			if mode == "pr-page-limit" && calls != 11 {
				t.Fatal("PR pagination not bounded", calls)
			}
			if mode == "page-limit" && calls != 10 {
				t.Fatal("pagination not bounded", calls)
			}
		})
	}
}

func TestSecurityAlertsUnsupportedAndInvalidArguments(t *testing.T) {
	m, _ := manager(t)
	if _, err := m.Save(ctx, scm.Input{Name: "bitbucket", Kind: "bitbucket-cloud", Repository: "acme/app", Token: ptr(token)}, "admin", false); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	gw := tools.NewGateway(reg, nil, nil)
	p := principal("", domain.ToolGrant{Name: "scm.security.alerts", Repositories: []string{"bitbucket"}})
	for _, args := range []string{`{"connection":"bitbucket","baseBranch":"main"}`, `{"connection":"bitbucket","baseBranch":"../bad"}`, `{"connection":"bitbucket","baseBranch":""}`} {
		res, err := gw.Invoke(ctx, p, "scm.security.alerts", json.RawMessage(args))
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("unsupported/invalid request accepted", res)
		}
	}
}

func TestSecurityAlertsFinalRedactedOutputBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pulls") || r.URL.Query().Get("page") != "1" {
			fmt.Fprint(w, "[]")
			return
		}
		items := make([]json.RawMessage, 5)
		for i := range items {
			s := strings.Replace(dependabotFixture, `"number":2`, fmt.Sprintf(`"number":%d`, i+1), 1)
			s = strings.Replace(s, `"summary":"fixture"`, `"summary":"`+strings.Repeat("z", 2048)+`"`, 1)
			s = strings.Replace(s, `"vulnerable_version_range":"< 8.12.2"`, `"vulnerable_version_range":"`+strings.Repeat("z", 2048)+`"`, 1)
			items[i] = json.RawMessage(s)
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()
	m, _ := manager(t)
	if _, err := m.Save(ctx, scm.Input{Name: "security", Kind: "github", Repository: "acme/app", APIURL: srv.URL, Token: ptr("z")}, "admin", false); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	reg.MustRegister(scm.Tools(m)...)
	gw := tools.NewGateway(reg, nil, nil)
	res, err := gw.Invoke(ctx, principal("", domain.ToolGrant{Name: "scm.security.alerts", Repositories: []string{"security"}}), "scm.security.alerts", json.RawMessage(`{"connection":"security","baseBranch":"main"}`))
	if err != nil || res == nil || !res.IsError || res.Truncated || len(res.Data) != 0 || !strings.Contains(res.Content, "model result limit") {
		t.Fatal("oversized redacted inventory accepted", res, err)
	}
}
