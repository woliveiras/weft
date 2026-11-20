package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/jiradc"
)

func TestJiraWorkflowMappingsUseRoleGuardedCASAndActualIssueDiscovery(t *testing.T) {
	var denied atomic.Bool
	var calls atomic.Int32
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" {
			t.Error("mapping save mutated Jira")
		}
		if r.Header.Get("Authorization") != "Bearer mapping-fixture-token" {
			t.Error("wrong credential")
		}
		if denied.Load() {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/jira/rest/api/2/issue/APP-7":
			fmt.Fprint(w, `{"id":"10007","key":"APP-7","fields":{"summary":"Sample","updated":"2026-11-20T12:00:00.000+0000","issuelinks":[],"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready","statusCategory":{"key":"new"}}}}`)
		case "/jira/rest/api/2/issue/10007/transitions":
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Revisão","to":{"id":"20","name":"Review"},"fields":{}}]}`)
		default:
			t.Error("unscoped discovery", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	old := http.DefaultTransport
	http.DefaultTransport = remote.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })
	srv, _ := setupApp(t, nil)
	connection := fmt.Sprintf(`{"name":"native","kind":"jira-dc","siteUrl":%q,"projectId":"10001","enabled":true,"projectRestricted":true,"token":"mapping-fixture-token"}`, remote.URL+"/jira")
	if code := call(t, srv, "POST", "/api/v1/issue-connections", adminTok, "application/json", connection, nil); code != 201 {
		t.Fatal(code)
	}
	input := `{"name":"review-step","connection":"native","connectionRevision":1,"sampleIssueKey":"APP-7","sampleIssueId":"10007","issueTypeId":"10002","fromStatusId":"10","transitionId":"31","targetStatusId":"20","issueUpdated":"2026-11-20T12:00:00.000+0000","enabled":true}`
	for _, token := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "POST", "/api/v1/issue-workflow-mappings", token, "application/json", input, nil); code != 403 {
			t.Fatal(code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("denied save dispatched discovery")
	}
	var v jiradc.WorkflowMapping
	if code := call(t, srv, "POST", "/api/v1/issue-workflow-mappings", adminTok, "application/json", input, &v); code != 201 || v.Revision != 1 || v.TransitionName != "Revisão" || v.ProjectID != "10001" {
		t.Fatal(code, v)
	}
	code, headers, raw := rawCall(t, "GET", srv.URL+"/api/v1/issue-workflow-mappings/review-step", viewerTok, "", nil)
	if code != 200 || headers.Get("ETag") != `"1"` || strings.Contains(raw, "mapping-fixture-token") {
		t.Fatal("mapping read failed", code)
	}
	for _, rev := range []string{"", "0", "2"} {
		code, _, _ = rawCall(t, "PUT", srv.URL+"/api/v1/issue-workflow-mappings/review-step", adminTok, input, map[string]string{"If-Match": rev})
		if (rev == "2" && code != 409) || (rev != "2" && code != 400) {
			t.Fatal("unreviewed revision accepted", rev, code)
		}
	}
	denied.Store(true)
	code, _, _ = rawCall(t, "PUT", srv.URL+"/api/v1/issue-workflow-mappings/review-step", adminTok, input, map[string]string{"If-Match": "1"})
	if code != 502 {
		t.Fatal("discovery failure hidden", code)
	}
	disable := strings.Replace(input, `"enabled":true`, `"enabled":false`, 1)
	before := calls.Load()
	code, _, raw = rawCall(t, "PUT", srv.URL+"/api/v1/issue-workflow-mappings/review-step", adminTok, disable, map[string]string{"If-Match": "1"})
	if json.Unmarshal([]byte(raw), &v) != nil || code != 200 || v.Enabled || v.Revision != 2 || calls.Load() != before {
		t.Fatal("disable required remote access", code, v)
	}
	var list []jiradc.WorkflowMapping
	if code := call(t, srv, "GET", "/api/v1/issue-workflow-mappings?connection=native", viewerTok, "", "", &list); code != 200 || len(list) != 1 {
		t.Fatal(code, list)
	}
	if code := call(t, srv, "GET", "/api/v1/issue-workflow-mappings?connection=other", viewerTok, "", "", &list); code != 200 || len(list) != 0 {
		t.Fatal(code, list)
	}
}
