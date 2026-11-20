package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/jiradc"
)

func TestNativeIssueConnectionsThroughRoleGuardedAPI(t *testing.T) {
	srv, _ := setupApp(t, nil)
	body := `{"name":"jira-team","kind":"jira-dc","siteUrl":"https://jira.fixture.invalid/context","projectId":"10001","enabled":true,"token":"native-fixture-only"}`
	if code := call(t, srv, "POST", "/api/v1/issue-connections", viewerTok, "application/json", body, nil); code != 403 {
		t.Fatal(code)
	}
	var v jiradc.Connection
	if code := call(t, srv, "POST", "/api/v1/issue-connections", adminTok, "application/json", body, &v); code != 201 || v.Revision != 1 || !v.CredentialConfigured {
		t.Fatal(code, v)
	}
	code, headers, raw := rawCall(t, "GET", srv.URL+"/api/v1/issue-connections/jira-team", viewerTok, "", nil)
	if code != 200 || headers.Get("ETag") != `"1"` || strings.Contains(raw, "native-fixture-only") {
		t.Fatal(code, headers.Get("ETag"))
	}
	for _, token := range []string{viewerTok, operatorTok} {
		code, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/issue-connections/jira-team", token, body, map[string]string{"If-Match": "1"})
		if code != 403 {
			t.Fatal("non-admin update", code)
		}
	}
	code, _, _ = rawCall(t, "POST", srv.URL+"/api/v1/issue-connections/jira-team/test", viewerTok, "", map[string]string{"If-Match": "1"})
	if code != 403 {
		t.Fatal("viewer test", code)
	}
	for _, rev := range []string{"", "0"} {
		code, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/issue-connections/missing", adminTok, body, map[string]string{"If-Match": rev})
		if code != 400 {
			t.Fatal("update created without reviewed revision", code)
		}
	}
	var list []map[string]any
	if code := call(t, srv, "GET", "/api/v1/issue-connections", viewerTok, "", "", &list); code != 200 || len(list) != 1 {
		t.Fatal(code, list)
	}
	for _, field := range []string{"token", "sealedToken"} {
		if _, ok := list[0][field]; ok {
			t.Fatal("secret field in API", field)
		}
	}
	for _, invalid := range []string{strings.Replace(body, `"jira-dc"`, `"jira"`, 1), strings.Replace(body, `"10001"`, `"APP"`, 1), strings.Replace(body, `"https://jira.fixture.invalid/context"`, `"http://jira.fixture.invalid"`, 1)} {
		if code := call(t, srv, "POST", "/api/v1/issue-connections", adminTok, "application/json", strings.Replace(invalid, "jira-team", "invalid", 1), nil); code != 422 {
			t.Fatal(code)
		}
	}
	invoke := func(method, path, body, rev string, out any) int {
		code, _, raw := rawCall(t, method, srv.URL+path, adminTok, body, map[string]string{"If-Match": rev})
		if out != nil && code < 300 {
			if err := json.Unmarshal([]byte(raw), out); err != nil {
				t.Fatal(err)
			}
		}
		return code
	}
	update := strings.Replace(body, `"enabled":true`, `"enabled":false`, 1)
	if code := invoke("PUT", "/api/v1/issue-connections/jira-team", update, "1", &v); code != 200 || v.Revision != 2 || v.Enabled {
		t.Fatal(code, v)
	}
	if code := invoke("PUT", "/api/v1/issue-connections/jira-team", update, "1", nil); code != 409 {
		t.Fatal(code)
	}
	// Both fail locally; testing never contacts the synthetic remote when disabled
	// or when the operator reviewed a stale connection revision.
	for _, rev := range []string{"1", "2"} {
		if code := invoke("POST", "/api/v1/issue-connections/jira-team/test", "", rev, nil); code != 409 {
			t.Fatal(code)
		}
	}
}

func TestNativeIssueConnectionTestReportsTLSFailure(t *testing.T) {
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("untrusted TLS must not dispatch an authenticated request")
	}))
	defer remote.Close()
	srv, _ := setupApp(t, nil)
	body, err := json.Marshal(map[string]any{"name": "jira-tls", "kind": "jira-dc", "siteUrl": remote.URL, "projectId": "10001", "enabled": true, "token": "native-fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if code := call(t, srv, "POST", "/api/v1/issue-connections", adminTok, "application/json", string(body), nil); code != 201 {
		t.Fatal(code)
	}
	code, _, raw := rawCall(t, "POST", srv.URL+"/api/v1/issue-connections/jira-tls/test", operatorTok, "", map[string]string{"If-Match": "1"})
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if code != 200 || out.OK || out.Error == "" || strings.Contains(raw, "native-fixture-secret") || strings.Contains(raw, remote.URL) {
		t.Fatal(code, out.OK, out.Error)
	}
}
