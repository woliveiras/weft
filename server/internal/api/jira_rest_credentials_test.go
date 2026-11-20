package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
)

const restFixtureCloud = "11111111-2222-3333-4444-555555555555"

func TestJiraRESTCredentialAPIAcceptsIndependentMaximumTokens(t *testing.T) {
	srv, _ := setupApp(t, nil)
	body, err := json.Marshal(map[string]any{
		"id": "jira-max", "name": "Maximum tokens", "service": "atlassian",
		"endpoint": "https://mcp.atlassian.com/v2/mcp", "enabled": true,
		"credential":         map[string]any{"value": strings.Repeat("m", 16384)},
		"jiraRest":           map[string]any{"cloudId": restFixtureCloud},
		"jiraRestCredential": map[string]any{"username": "fixture@example.invalid", "value": strings.Repeat("r", 16384)},
	})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := rawCall(t, "POST", srv.URL+"/api/v1/mcp/connections", adminTok, string(body), nil)
	if code != 201 {
		t.Fatalf("independent maximum credentials rejected: HTTP %d", code)
	}
}

func TestJiraRESTCredentialAPISeparatesSecretsAndFencesProfiles(t *testing.T) {
	srv, a := setupApp(t, nil)
	const email = "rest-fixture@example.invalid"
	const restToken = "synthetic-jira-rest-only"
	const mcpToken = "synthetic-jira-mcp-only"
	body := fmt.Sprintf(`{"id":"jira-rest","name":"Jira REST fixture","service":"atlassian","endpoint":"https://mcp.atlassian.com/v2/mcp","enabled":true,"credential":{"value":%q},"jiraRest":{"cloudId":%q},"jiraRestCredential":{"username":%q,"value":%q}}`, mcpToken, restFixtureCloud, email, restToken)
	for _, token := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "POST", "/api/v1/mcp/connections", token, "application/json", body, nil); code != 403 {
			t.Fatal("non-admin credential mutation", code)
		}
	}
	code, _, saved := rawCall(t, "POST", srv.URL+"/api/v1/mcp/connections", adminTok, body, nil)
	if code != 201 {
		t.Fatalf("separate REST configuration not created: HTTP %d", code)
	}
	assertJiraRESTCredentialFlags(t, saved, true, true)
	assertJiraRESTSecretsAbsent(t, saved, email, restToken, mcpToken)
	row, err := a.Store.GetSetting(t.Context(), "mcp/connections/jira-rest")
	if err != nil {
		t.Fatal(err)
	}
	assertJiraRESTSecretsAbsent(t, string(row.Value), email, restToken, mcpToken)
	var calls atomic.Int32
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		user, pass, ok := r.BasicAuth()
		if !ok || user != email || pass != restToken {
			t.Error("REST received an incorrect credential")
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/ex/jira/"+restFixtureCloud+"/rest/api/3/issue/10007/transitions" {
			fmt.Fprint(w, `{"transitions":[]}`)
			return
		}
		fmt.Fprint(w, `{"id":"10007","key":"APP-7","fields":{"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready"},"updated":"2026-11-20T12:00:00.000+0000"}}`)
	}))
	defer remote.Close()
	a.TrackerAPI.Client = &http.Client{Transport: cloudAPITransport{remote.URL, remote.Client().Transport}}
	profile := fmt.Sprintf(`{"name":"rest-scope","mcpConnection":"jira-rest","mcpRevision":1,"cloudId":%q,"siteUrl":"https://api.atlassian.com/ex/jira/%s","projectId":"10001","enabled":true}`, restFixtureCloud, restFixtureCloud)
	if code := call(t, srv, "POST", "/api/v1/jira-cloud-profiles", adminTok, "application/json", profile, nil); code != 201 || calls.Load() != 0 {
		t.Fatal("profile creation failed or contacted upstream", code)
	}
	code, _, _ = rawCall(t, "POST", srv.URL+"/api/v1/jira-cloud-profiles/rest-scope/transitions", operatorTok, `{"issueKey":"APP-7"}`, map[string]string{"If-Match": "1"})
	if code != 200 || calls.Load() == 0 {
		t.Fatal("API/profile/REST collaboration failed", code)
	}
	var view mcpconfig.View
	if err := json.Unmarshal([]byte(saved), &view); err != nil {
		t.Fatal(err)
	}
	a.MCP.Discover = func(_ context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		if endpoint != "https://mcp.atlassian.com/v2/mcp" || token != mcpToken {
			t.Error("MCP discovery received the REST credential")
		}
		return []mcpconfig.Tool{}, nil
	}
	if code := call(t, srv, "POST", "/api/v1/mcp/connections/jira-rest/test", operatorTok, "", "", &view); code != 200 || view.Status != "ready" {
		t.Fatal("MCP discovery failed", code)
	}
	code, _, _ = rawCall(t, "POST", srv.URL+"/api/v1/jira-cloud-profiles/rest-scope/transitions", operatorTok, `{"issueKey":"APP-7"}`, map[string]string{"If-Match": "1"})
	if code != 409 {
		t.Fatal("old profile pin survived a connection revision change", code)
	}
	profile = strings.Replace(profile, `"mcpRevision":1`, fmt.Sprintf(`"mcpRevision":%d`, view.Revision), 1)
	if code, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/jira-cloud-profiles/rest-scope", adminTok, profile, map[string]string{"If-Match": "1"}); code != 200 {
		t.Fatal("could not adopt current connection pin", code)
	}
	count := calls.Load()
	clear := `{"id":"jira-rest","name":"Jira REST fixture","service":"atlassian","endpoint":"https://mcp.atlassian.com/v2/mcp","enabled":true,"jiraRestCredential":{"clear":true}}`
	code, _, out := rawCall(t, "PUT", srv.URL+"/api/v1/mcp/connections/jira-rest", adminTok, clear, map[string]string{"If-Match": fmt.Sprint(view.Revision)})
	if code != 200 {
		t.Fatal("REST clear lost explicit mode or kept credential", code)
	}
	assertJiraRESTCredentialFlags(t, out, true, false)
	var cleared mcpconfig.View
	if err := json.Unmarshal([]byte(out), &cleared); err != nil || cleared.Revision <= view.Revision {
		t.Fatal("REST-only update did not advance connection revision")
	}
	code, _, _ = rawCall(t, "POST", srv.URL+"/api/v1/jira-cloud-profiles/rest-scope/transitions", operatorTok, `{"issueKey":"APP-7"}`, map[string]string{"If-Match": "2"})
	if code != 409 || calls.Load() != count {
		t.Fatal("REST-only update did not invalidate adopted profile", code)
	}
	assertJiraRESTSecretsAbsent(t, out, email, restToken, mcpToken)
	if _, err := a.TrackerAPI.Revision(t.Context(), "jira-rest", "jira"); err == nil || calls.Load() != count {
		t.Fatal("cleared REST credential fell back to MCP")
	}
	if code, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/mcp/connections/jira-rest", adminTok, clear, map[string]string{"If-Match": fmt.Sprint(view.Revision)}); code != 409 {
		t.Fatal("stale credential update accepted", code)
	}
}

func assertJiraRESTCredentialFlags(t *testing.T, raw string, mcp, rest bool) {
	t.Helper()
	var view struct {
		CredentialConfigured bool `json:"credentialConfigured"`
		JiraREST             *struct {
			CloudID              string `json:"cloudId"`
			CredentialConfigured bool   `json:"credentialConfigured"`
		} `json:"jiraRest"`
	}
	if err := json.Unmarshal([]byte(raw), &view); err != nil || view.CredentialConfigured != mcp || view.JiraREST == nil || view.JiraREST.CloudID != restFixtureCloud || view.JiraREST.CredentialConfigured != rest {
		t.Fatal("MCP/REST configured flags or Cloud ID are incorrect")
	}
}

func assertJiraRESTSecretsAbsent(t *testing.T, raw string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(raw, value) || strings.Contains(raw, base64.StdEncoding.EncodeToString([]byte(value))) {
			t.Fatal("credential material escaped its write-only boundary")
		}
	}
	if len(values) >= 2 && strings.Contains(raw, base64.StdEncoding.EncodeToString([]byte(values[0]+":"+values[1]))) {
		t.Fatal("derived Basic credential escaped")
	}
}

func TestDeclarativeJiraRESTCredentialRoundTripAndScopeChanges(t *testing.T) {
	srv, a := setupApp(t, nil)
	const email = "yaml-rest@example.invalid"
	const token = "synthetic-yaml-jira-rest-token"
	t.Setenv("WEFT_SECRET_JIRA_REST_FIXTURE", token)
	t.Setenv("WEFT_SECRET_JIRA_EMAIL_FIXTURE", email)
	spec := "  name: Cloud fixture\n  service: atlassian\n  endpoint: https://mcp.atlassian.com/v2/mcp\n  enabled: true\n  jiraRest: {cloudId: '" + restFixtureCloud + "'}\n  jiraRestSecretRef: {env: WEFT_SECRET_JIRA_REST_FIXTURE}\n  jiraRestUsernameRef: {env: WEFT_SECRET_JIRA_EMAIL_FIXTURE}"
	raw := yamlObject("MCPConnection", "yaml-rest", spec)
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{raw}}); !report.OK {
		t.Fatal("REST credential refs rejected")
	}
	exported := exportConfig(t, srv)
	assertJiraRESTSecretsAbsent(t, exported, email, token)
	if !strings.Contains(exported, "jiraRestSecretRef:") || strings.Contains(exported, "credentialConfigured:") || strings.Contains(exported, "jiraRestUsernameRef:") {
		t.Fatal("export must retain configuration and preserve ref, not secret metadata/email")
	}
	for _, report := range []installation.Plan{
		configApplyRequest(t, srv, installation.Request{Files: []string{exported}}),
		configApplyRequest(t, srv, installation.Request{Files: []string{raw}}),
	} {
		for _, object := range report.Objects {
			if object.Key == "MCPConnection/yaml-rest" && object.Result != "unchanged" {
				t.Fatalf("round trip caused credential churn: %+v", object)
			}
		}
	}
	before, _ := a.MCP.Get(t.Context(), "yaml-rest")
	changed := strings.Replace(exported, restFixtureCloud, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", 1)
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{changed}})
	if plan.OK {
		t.Fatal("preserved REST token accepted a new Cloud ID")
	}
	after, _ := a.MCP.Get(t.Context(), "yaml-rest")
	if before.Revision != after.Revision {
		t.Fatal("denied dry-run changed persisted credentials")
	}
	for _, bad := range []string{
		strings.Replace(raw, "  jiraRestUsernameRef: {env: WEFT_SECRET_JIRA_EMAIL_FIXTURE}\n", "", 1),
		strings.Replace(raw, "  jiraRestSecretRef: {env: WEFT_SECRET_JIRA_REST_FIXTURE}\n", "", 1),
		raw + "  jiraRestCredential: {username: inline@example.invalid, value: forbidden-inline-token}\n",
		strings.Replace(raw, "kind: MCPConnection", "kind: Project", 1),
	} {
		if configRequest(t, srv, "plan", installation.Request{Files: []string{bad}}).OK {
			t.Fatal("incomplete/plaintext/wrong-kind REST secret input accepted")
		}
	}
}
