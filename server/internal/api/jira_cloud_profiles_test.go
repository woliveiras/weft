package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
)

type cloudAPITransport struct {
	target string
	base   http.RoundTripper
}

func (v cloudAPITransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c := r.Clone(r.Context())
	u := *r.URL
	c.URL = &u
	remote := strings.TrimPrefix(v.target, "https://")
	c.URL.Scheme = "https"
	c.URL.Host = remote
	return v.base.RoundTrip(c)
}
func TestCloudProfilesAndMappingsUseExplicitRolesScopeAndCAS(t *testing.T) {
	var calls atomic.Int32
	var denied atomic.Bool
	const cloudID = "11111111-2222-3333-4444-555555555555"
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" {
			t.Error("configuration dispatched a remote mutation")
			w.WriteHeader(405)
			return
		}
		if denied.Load() {
			w.WriteHeader(403)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/ex/jira/"+cloudID+"/rest/api/3/issue/") {
			t.Errorf("unscoped request %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/transitions") {
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Review","to":{"id":"20","name":"Review"},"fields":{}}]}`)
			return
		}
		fmt.Fprint(w, `{"id":"10007","key":"APP-7","fields":{"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready"},"updated":"2026-11-20T12:00:00.000+0000"}}`)
	}))
	defer remote.Close()
	srv, a := setupApp(t, nil)
	a.TrackerAPI.Client = &http.Client{Transport: cloudAPITransport{remote.URL, remote.Client().Transport}}
	credential := mcpconfig.Credential{Value: "synthetic-cloud-workflow-token"}
	connection, err := a.MCP.Put(t.Context(), mcpconfig.Input{ID: "cloud-api", Name: "Cloud REST fixture", Service: "jira", Endpoint: "https://mcp.atlassian.com/v2/mcp", Enabled: true, Credential: &credential}, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	input := jiracloud.Input{Name: "cloud-profile", MCPConnection: connection.ID, MCPRevision: connection.Revision, CloudID: cloudID, SiteURL: "https://api.atlassian.com/ex/jira/" + cloudID, ProjectID: "10001", Enabled: true, ProjectRestricted: true}
	raw, _ := json.Marshal(input)
	for _, token := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "POST", "/api/v1/jira-cloud-profiles", token, "application/json", string(raw), nil); code != 403 {
			t.Fatal("profile role", code)
		}
	}
	if code := call(t, srv, "POST", "/api/v1/jira-cloud-profiles/preview", adminTok, "application/json", string(raw), nil); code != 200 {
		t.Fatal("preview", code)
	}
	if calls.Load() != 0 {
		t.Fatal("preview contacted Jira")
	}
	var profile jiracloud.Profile
	if code := call(t, srv, "POST", "/api/v1/jira-cloud-profiles", adminTok, "application/json", string(raw), &profile); code != 201 || profile.Revision != 1 {
		t.Fatal("create profile", code, profile)
	}
	code, headers, body := rawCall(t, "GET", srv.URL+"/api/v1/jira-cloud-profiles/cloud-profile", viewerTok, "", nil)
	if code != 200 || headers.Get("ETag") != `"1"` || strings.Contains(body, credential.Value) {
		t.Fatal("profile read", code)
	}
	if code := call(t, srv, "POST", "/api/v1/jira-cloud-profiles/cloud-profile/transitions", viewerTok, "application/json", `{"issueKey":"APP-7"}`, nil); code != 403 {
		t.Fatal(code)
	}
	code, _, _ = rawCall(t, "POST", srv.URL+"/api/v1/jira-cloud-profiles/cloud-profile/transitions", operatorTok, `{"issueKey":"APP-7"}`, map[string]string{"If-Match": "1"})
	if code != 200 {
		t.Fatal("cloud discovery", code)
	}
	mapping := jiradc.MappingInput{Name: "reviewing", Connection: profile.Name, ConnectionRevision: profile.Revision, SampleIssueKey: "APP-7", SampleIssueID: "10007", IssueTypeID: "10002", FromStatusID: "10", TransitionID: "31", TargetStatusID: "20", IssueUpdated: "2026-11-20T12:00:00.000+0000", Enabled: true}
	mr, _ := json.Marshal(mapping)
	var saved jiradc.WorkflowMapping
	if code := call(t, srv, "POST", "/api/v1/jira-cloud-workflow-mappings", adminTok, "application/json", string(mr), &saved); code != 201 || saved.ProjectID != "10001" {
		t.Fatal("cloud mapping", code, saved)
	}
	if code := call(t, srv, "GET", "/api/v1/issue-workflow-mappings/reviewing", viewerTok, "", "", nil); code != 404 {
		t.Fatal("edition namespace collision", code)
	}
	for _, rev := range []string{"", "0", "2"} {
		code, _, _ = rawCall(t, "PUT", srv.URL+"/api/v1/jira-cloud-profiles/cloud-profile", adminTok, string(raw), map[string]string{"If-Match": rev})
		if (rev == "2" && code != 409) || (rev != "2" && code != 400) {
			t.Fatal("profile stale CAS", rev, code)
		}
	}
	denied.Store(true)
	code, _, _ = rawCall(t, "POST", srv.URL+"/api/v1/jira-cloud-profiles/cloud-profile/transitions", operatorTok, `{"issueKey":"APP-7"}`, map[string]string{"If-Match": "1"})
	if code != 502 {
		t.Fatal("hidden discovery failure", code)
	}
	mapping.Enabled = false
	mr, _ = json.Marshal(mapping)
	before := calls.Load()
	code, _, _ = rawCall(t, "PUT", srv.URL+"/api/v1/jira-cloud-workflow-mappings/reviewing", adminTok, string(mr), map[string]string{"If-Match": "1"})
	if code != 200 || calls.Load() != before {
		t.Fatal("offline mapping disable", code)
	}
	input.Enabled = false
	raw, _ = json.Marshal(input)
	code, _, _ = rawCall(t, "PUT", srv.URL+"/api/v1/jira-cloud-profiles/cloud-profile", adminTok, string(raw), map[string]string{"If-Match": "1"})
	if code != 200 || calls.Load() != before {
		t.Fatal("offline profile disable", code)
	}
}
