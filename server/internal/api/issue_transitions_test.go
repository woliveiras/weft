package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeTransitionDiscoveryThroughAPIUsesScopedReadOnlyConnection(t *testing.T) {
	const issue = `{"id":"10007","key":"APP-7","fields":{"summary":"Review","issuelinks":[],"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready","statusCategory":{"key":"new"}},"updated":"2026-11-20T10:00:00.000+0000"}}`
	var calls atomic.Int32
	var mode atomic.Int32
	var mutate func()
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer transition-fixture-token" {
			t.Error("unexpected remote mutation or credential")
		}
		switch r.URL.Path {
		case "/jira/rest/api/2/issue/APP-7":
			if mode.Load() == 1 {
				fmt.Fprint(w, strings.Replace(issue, `"project":{"id":"10001"}`, `"project":{"id":"10003"}`, 1))
				return
			}
			fmt.Fprint(w, issue)
		case "/jira/rest/api/2/issue/10007/transitions":
			if mode.Load() == 2 {
				w.WriteHeader(403)
				return
			}
			if mode.Load() == 3 {
				mutate()
			}
			if mode.Load() == 4 {
				fmt.Fprint(w, `{"transitions":[{"id":"31","name":"transition-fixture-token","to":{"id":"20","name":"Review"},"fields":{}}]}`)
				return
			}
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Revisão","to":{"id":"20","name":"Review"},"fields":{}},{"id":"32","name":"Review","to":{"id":"21","name":"Review"}}]}`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	old := http.DefaultTransport
	http.DefaultTransport = remote.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })
	srv, _ := setupApp(t, nil)
	connection := fmt.Sprintf(`{"name":"native","kind":"jira-dc","siteUrl":%q,"projectId":"10001","enabled":true,"projectRestricted":false,"token":"transition-fixture-token"}`, remote.URL+"/jira")
	if code := call(t, srv, "POST", "/api/v1/issue-connections", adminTok, "application/json", connection, nil); code != 201 {
		t.Fatal(code)
	}
	invoke := func(token, rev, body string) (int, string) {
		code, _, raw := rawCall(t, "POST", srv.URL+"/api/v1/issue-connections/native/transitions", token, body, map[string]string{"If-Match": rev})
		return code, raw
	}
	for _, tc := range []struct {
		token, rev, body string
		code             int
	}{{viewerTok, "1", `{"issueKey":"APP-7"}`, 403}, {operatorTok, "", `{"issueKey":"APP-7"}`, 400}, {adminTok, "0", `{"issueKey":"APP-7"}`, 400}, {operatorTok, "2", `{"issueKey":"APP-7"}`, 409}, {operatorTok, "1", `{"issueKey":"APP-7","transition":"31"}`, 400}} {
		if code, _ := invoke(tc.token, tc.rev, tc.body); code != tc.code {
			t.Fatal(code, tc.code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request contacted Jira")
	}
	code, raw := invoke(operatorTok, "1", `{"issueKey":"APP-7"}`)
	var out struct {
		Connection string `json:"connection"`
		Revision   int64  `json:"revision"`
		Discovery  struct {
			IssueID     string `json:"issueId"`
			ProjectID   string `json:"projectId"`
			Transitions []struct {
				ID     string         `json:"id"`
				Fields map[string]any `json:"fields"`
			} `json:"transitions"`
		} `json:"discovery"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil || code != 200 || out.Connection != "native" || out.Revision != 1 || out.Discovery.IssueID != "10007" || out.Discovery.ProjectID != "10001" || len(out.Discovery.Transitions) != 2 || out.Discovery.Transitions[0].Fields == nil || out.Discovery.Transitions[1].Fields != nil || strings.Contains(raw, "transition-fixture-token") {
		t.Fatal("invalid discovery", code)
	}
	for _, failure := range []int32{1, 2, 4} {
		mode.Store(failure)
		before := calls.Load()
		code, raw := invoke(operatorTok, "1", `{"issueKey":"APP-7"}`)
		if code != 502 || strings.Contains(raw, "transition-fixture-token") || strings.Contains(raw, `"discovery"`) {
			t.Fatal("unsafe failure", failure, code)
		}
		if failure == 1 && calls.Load()-before != 1 {
			t.Fatal("foreign issue metadata fetched")
		}
	}
	mutate = func() {
		code, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/issue-connections/native", adminTok, strings.Replace(connection, `"enabled":true`, `"enabled":false`, 1), map[string]string{"If-Match": "1"})
		if code != 200 {
			t.Error(code)
		}
	}
	mode.Store(3)
	if code, raw := invoke(operatorTok, "1", `{"issueKey":"APP-7"}`); code != 409 || strings.Contains(raw, `"discovery"`) {
		t.Fatal("stale connection discovery returned", code)
	}
	before := calls.Load()
	if code, _ := invoke(operatorTok, "2", `{"issueKey":"APP-7"}`); code != 409 || calls.Load() != before {
		t.Fatal("disabled connection dispatched", code)
	}
}
