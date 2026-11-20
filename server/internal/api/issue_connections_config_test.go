package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestDeclarativeNativeIssueConnectionRoundTripAndScopeFence(t *testing.T) {
	srv, a := setupApp(t, nil)
	var calls atomic.Int32
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer remote.Close()
	t.Setenv("WEFT_SECRET_JIRA_YAML_FIXTURE", "jira-yaml-synthetic-token")
	raw := yamlObject("IssueConnection", "jira-native", "  kind: jira-dc\n  siteUrl: "+remote.URL+"/jira/\n  projectId: '10001'\n  enabled: true\n  projectRestricted: true\n  secretRef: {env: WEFT_SECRET_JIRA_YAML_FIXTURE}")
	req := installation.Request{Files: []string{raw}}
	plan := configRequest(t, srv, "plan", req)
	if !plan.OK || len(plan.Objects) != 1 || plan.Objects[0].Action != "create" {
		t.Fatal(plan)
	}
	rows, err := a.Store.ListSettings(t.Context(), jiradc.ConnectionPrefix)
	if err != nil || len(rows) != 0 || calls.Load() != 0 {
		t.Fatal("dry run mutated or called provider", err)
	}
	report := configApplyRequest(t, srv, req)
	if !report.OK || report.Objects[0].Result != "applied" || calls.Load() != 0 {
		t.Fatal(report)
	}
	var current jiradc.Connection
	if code := call(t, srv, "GET", "/api/v1/issue-connections/jira-native", viewerTok, "", "", &current); code != 200 || current.Revision != 1 || !current.ProjectRestricted {
		t.Fatal(code, current)
	}
	exported := exportConfig(t, srv)
	if !strings.Contains(exported, "kind: IssueConnection") || !strings.Contains(exported, "preserve: true") || strings.Contains(exported, "jira-yaml-synthetic-token") || strings.Contains(exported, "sealedToken") {
		t.Fatal("unsafe or missing native export")
	}
	again := configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	if !again.OK {
		t.Fatal(again)
	}
	for _, o := range again.Objects {
		if o.Action != "unchanged" {
			t.Fatal(o)
		}
	}
	// The same referenced value also compares unchanged without resealing it.
	again = configRequest(t, srv, "plan", installation.Request{Files: []string{raw}})
	if !again.OK || again.Objects[0].Action != "unchanged" {
		t.Fatal(again)
	}
	bad := strings.Replace(raw, "'10001'", "'10002'", 1)
	bad = strings.Replace(bad, "secretRef: {env: WEFT_SECRET_JIRA_YAML_FIXTURE}", "secretRef: {preserve: true}", 1)
	if plan = configRequest(t, srv, "plan", installation.Request{Files: []string{bad}}); plan.OK {
		t.Fatal("scope edit preserved another audience", plan)
	}
	update := strings.Replace(raw, "enabled: true", "enabled: false", 1)
	plan = configRequest(t, srv, "plan", installation.Request{Files: []string{update}})
	if plan.OK || plan.Objects[0].Action != "conflict" {
		t.Fatal("unreviewed update", plan)
	}
	report = configApplyRequest(t, srv, installation.Request{Files: []string{update}, Revisions: map[string]string{"IssueConnection/jira-native": report.Objects[0].Revision}})
	if !report.OK {
		t.Fatal(report)
	}
	// A previously exported baseline cannot overwrite a later direct API edit.
	stale := configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	if stale.OK {
		t.Fatal("stale native revision accepted", stale)
	}
	stored, err := a.Store.GetSetting(t.Context(), jiradc.ConnectionPrefix+"jira-native")
	if err != nil || stored.Revision != 2 || strings.Contains(string(stored.Value), "jira-yaml-synthetic-token") {
		t.Fatal("native storage", err)
	}
	events, err := a.Store.ListEvents(t.Context(), storage.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	eventJSON, err := json.Marshal(events)
	if err != nil || strings.Contains(string(eventJSON), "jira-yaml-synthetic-token") {
		t.Fatal("unsafe declarative audit", err)
	}
	if calls.Load() != 0 {
		t.Fatal("configuration dispatched a provider call")
	}
}
