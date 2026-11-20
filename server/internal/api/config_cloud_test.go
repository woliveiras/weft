package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
)

type cloudConfigFixture struct {
	input jiracloud.Input
	calls atomic.Int32
	mode  atomic.Int32
}

func cloudConfigScope(t *testing.T, a *app.App) *cloudConfigFixture {
	t.Helper()
	f := &cloudConfigFixture{}
	const cloud = "11111111-2222-3333-4444-555555555555"
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/ex/jira/"+cloud+"/rest/api/3/issue/") {
			t.Error("declarative Cloud configuration escaped discovery scope")
			w.WriteHeader(405)
			return
		}
		if f.mode.Load() == 1 {
			w.WriteHeader(403)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/transitions") {
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Review","to":{"id":"20","name":"Review"},"fields":{}}]}`)
			return
		}
		updated := "2026-11-20T12:00:00.000+0000"
		if f.mode.Load() == 2 {
			updated = "2026-11-20T12:01:00.000+0000"
		}
		fmt.Fprintf(w, `{"id":"10007","key":"APP-7","fields":{"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready"},"updated":%q}}`, updated)
	}))
	t.Cleanup(remote.Close)
	a.TrackerAPI.Client = &http.Client{Transport: cloudAPITransport{remote.URL, remote.Client().Transport}}
	v, err := a.MCP.Put(t.Context(), mcpconfig.Input{ID: "cloud-config", Name: "Cloud config fixture", Service: "jira", Endpoint: "https://mcp.atlassian.com/v2/mcp", Enabled: true,
		Credential: &mcpconfig.Credential{Value: "cloud-config-synthetic-credential"}}, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	f.input = jiracloud.Input{Name: "cloud-profile", MCPConnection: v.ID, MCPRevision: v.Revision, CloudID: cloud, SiteURL: "https://api.atlassian.com/ex/jira/" + cloud, ProjectID: "10001", Enabled: true, ProjectRestricted: true}
	return f
}

func cloudProfileYAML(t *testing.T, in jiracloud.Input) string {
	t.Helper()
	spec := installation.Map(in)
	delete(spec, "name")
	raw, err := installation.Export([]installation.Document{installation.New("JiraCloudProfile", in.Name, spec)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func cloudMappingYAML(name, profile string, revision int64) string {
	return strings.Replace(configMappingYAML(name, profile, revision), "kind: IssueWorkflowMapping", "kind: JiraCloudWorkflowMapping", 1)
}

func TestDeclarativeCloudScopesSeparateProfileMappingAndOfflineRoundTrip(t *testing.T) {
	srv, a := setupApp(t, nil)
	f := cloudConfigScope(t, a)
	profileDoc := cloudProfileYAML(t, f.input)
	profilePlan := configRequest(t, srv, "plan", installation.Request{Files: []string{profileDoc}})
	if !profilePlan.OK || f.calls.Load() != 0 {
		t.Fatal("profile plan contacted Jira", profilePlan)
	}
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{profileDoc}}); !report.OK || f.calls.Load() != 0 {
		t.Fatal("credential-free profile apply contacted Jira", report)
	}
	mappingDoc := cloudMappingYAML("review-step", f.input.Name, 1)
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{mappingDoc}})
	if !plan.OK || f.calls.Load() != 0 {
		t.Fatal("mapping plan performed discovery", plan)
	}
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{mappingDoc}}); !report.OK || f.calls.Load() == 0 {
		t.Fatal("effective mapping apply did not validate sample", report)
	}
	if _, err := (jiradc.WorkflowMappings{Connections: a.IssueConnections}).Get(t.Context(), "review-step"); err == nil {
		t.Fatal("Cloud YAML mapping collided with DC namespace")
	}
	exported := exportConfig(t, srv)
	if !strings.Contains(exported, "kind: JiraCloudProfile") || !strings.Contains(exported, "kind: JiraCloudWorkflowMapping") || strings.Contains(exported, "cloud-config-synthetic-credential") {
		t.Fatal("Cloud export incomplete or unsafe")
	}
	// Existing definitions remain inspectable and disableable after authority
	// revocation. No-op does not demand live/current MCP compatibility.
	view, err := a.MCP.Get(t.Context(), f.input.MCPConnection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.MCP.Put(t.Context(), mcpconfig.Input{ID: view.ID, Name: view.Name, Service: view.Service, Endpoint: view.Endpoint, Enabled: false}, view.Revision, "revoker"); err != nil {
		t.Fatal(err)
	}
	f.mode.Store(1)
	before := f.calls.Load()
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{exportConfig(t, srv)}}); !report.OK || f.calls.Load() != before {
		t.Fatal("unchanged Cloud export/apply required provider access", report)
	}
	disabledMapping := strings.Replace(mappingDoc, "enabled: true", "enabled: false", 1)
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{disabledMapping}, Force: []string{"JiraCloudWorkflowMapping/review-step"}}); !report.OK || f.calls.Load() != before {
		t.Fatal("Cloud mapping exact disable required access", report)
	}
	f.input.Enabled = false
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{cloudProfileYAML(t, f.input)}, Force: []string{"JiraCloudProfile/" + f.input.Name}}); !report.OK || f.calls.Load() != before {
		t.Fatal("Cloud profile exact disable required access", report)
	}
}

func TestDeclarativeCloudRejectsImplicitRevisionsAndMixedAdoptionWithoutDiscovery(t *testing.T) {
	srv, a := setupApp(t, nil)
	f := cloudConfigScope(t, a)
	for _, edit := range []func(*jiracloud.Input){func(v *jiracloud.Input) { v.MCPRevision = 0 }, func(v *jiracloud.Input) { v.MCPRevision++ }, func(v *jiracloud.Input) { v.MCPConnection = "missing" }, func(v *jiracloud.Input) { v.SiteURL = "https://jira.example" }} {
		in := f.input
		edit(&in)
		if plan := configRequest(t, srv, "plan", installation.Request{Files: []string{cloudProfileYAML(t, in)}}); plan.OK || f.calls.Load() != 0 {
			t.Fatal("invalid Cloud profile dispatched discovery", plan)
		}
	}
	mcpDoc := yamlObject("MCPConnection", f.input.MCPConnection, "  name: changed\n  service: jira\n  endpoint: https://mcp.atlassian.com/v2/mcp\n  enabled: true\n  secretRef: {preserve: true}")
	if plan := configRequest(t, srv, "plan", installation.Request{Files: []string{mcpDoc, cloudProfileYAML(t, f.input)}, Force: []string{"MCPConnection/" + f.input.MCPConnection}}); plan.OK || f.calls.Load() != 0 {
		t.Fatal("planned MCP change was implicitly adopted", plan)
	}
	if plan := configRequest(t, srv, "plan", installation.Request{Files: []string{cloudProfileYAML(t, f.input), cloudMappingYAML("review-step", f.input.Name, 1)}}); plan.OK || f.calls.Load() != 0 {
		t.Fatal("unsaved profile was adopted by a mapping", plan)
	}
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{cloudProfileYAML(t, f.input)}}); !report.OK {
		t.Fatal(report)
	}
	for _, mode := range []int32{1, 2} {
		f.mode.Store(mode)
		report := configApplyRequest(t, srv, installation.Request{Files: []string{cloudMappingYAML("review-step", f.input.Name, 1)}})
		if report.OK || report.Objects[0].Result != "failed" {
			t.Fatal("denied/stale discovery became saved mapping", report)
		}
	}
}

func TestDeclarativeCloudMCPProfileAndOpaqueRevisionsRemainDistinct(t *testing.T) {
	srv, a := setupApp(t, nil)
	f := cloudConfigScope(t, a)
	view, err := a.MCP.Get(t.Context(), f.input.MCPConnection)
	if err != nil {
		t.Fatal(err)
	}
	view, err = a.MCP.Put(t.Context(), mcpconfig.Input{ID: view.ID, Name: "A separately reviewed MCP revision", Service: view.Service, Endpoint: view.Endpoint, Enabled: true}, view.Revision, "operator")
	if err != nil {
		t.Fatal(err)
	}
	f.input.MCPRevision = view.Revision
	profile := configApplyRequest(t, srv, installation.Request{Files: []string{cloudProfileYAML(t, f.input)}})
	if !profile.OK || f.input.MCPRevision != 2 || profile.Objects[0].Revision == "1" || f.calls.Load() != 0 {
		t.Fatal(profile)
	}
	for _, revision := range []int64{2, 1} {
		plan := configRequest(t, srv, "plan", installation.Request{Files: []string{cloudMappingYAML("review", f.input.Name, revision)}})
		if plan.OK != (revision == 1) || f.calls.Load() != 0 {
			t.Fatal("mapping confused MCP/profile revisions or contacted provider", plan)
		}
	}
}

func TestDeclarativeCloudTriggerConsentRequiresAlreadySavedAuthority(t *testing.T) {
	srv, a := setupApp(t, nil)
	f := cloudConfigScope(t, a)
	view, err := a.MCP.Get(t.Context(), f.input.MCPConnection)
	if err != nil {
		t.Fatal(err)
	}
	view, err = a.MCP.Put(t.Context(), mcpconfig.Input{ID: view.ID, Name: "Separately reviewed MCP revision 2", Service: view.Service, Endpoint: view.Endpoint, Enabled: true}, view.Revision, "operator")
	if err != nil {
		t.Fatal(err)
	}
	f.input.MCPRevision = view.Revision
	profileDoc := cloudProfileYAML(t, f.input)
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{profileDoc}}); !report.OK {
		t.Fatal(report)
	}
	mappingDoc := cloudMappingYAML("review", f.input.Name, 1)
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{mappingDoc}}); !report.OK {
		t.Fatal(report)
	}
	if report := configApplyRequest(t, srv, installation.Request{Files: []string{yamlObject("Graph", "cloud-workflow", "  defaults: {runtime: {kind: echo}}\n  nodes: [{id: worker}]")}}); !report.OK {
		t.Fatal(report)
	}
	triggerDoc := yamlObject("IssueTrigger", "cloud-issues", fmt.Sprintf("  workflow: cloud-workflow\n  nodeId: worker\n  label: weft\n  authorizationMode: ask\n  readinessDisabled: true\n  source: {kind: jira, connectionId: %s, cloudId: %s, siteUrl: %s, projectId: '10001', profileId: %s, profileRevision: 1}\n  lifecycleMappings: {prDelivered: {name: review, revision: 1}}", f.input.MCPConnection, f.input.CloudID, f.input.SiteURL, f.input.Name))
	before := f.calls.Load()
	if plan := configRequest(t, srv, "plan", installation.Request{Files: []string{triggerDoc}, RecordConsent: true}); !plan.OK || f.calls.Load() != before {
		t.Fatal("saved Cloud consent was unavailable or contacted Jira", plan)
	}
	if plan := configRequest(t, srv, "plan", installation.Request{Files: []string{exportConfig(t, srv), triggerDoc}, RecordConsent: true}); !plan.OK || f.calls.Load() != before {
		t.Fatal("unchanged dependencies rewrote the saved native MCP revision", plan)
	}
	for _, dependency := range []struct{ key, document string }{
		{"JiraCloudProfile/" + f.input.Name, strings.Replace(profileDoc, "enabled: true", "enabled: false", 1)},
		{"JiraCloudWorkflowMapping/review", strings.Replace(mappingDoc, "enabled: true", "enabled: false", 1)},
		{"MCPConnection/" + f.input.MCPConnection, yamlObject("MCPConnection", f.input.MCPConnection, "  name: changed\n  service: jira\n  endpoint: https://mcp.atlassian.com/v2/mcp\n  enabled: true\n  secretRef: {preserve: true}")},
	} {
		req := installation.Request{Files: []string{triggerDoc, dependency.document}, RecordConsent: true, Force: []string{dependency.key}}
		plan := configRequest(t, srv, "plan", req)
		if plan.OK || f.calls.Load() != before {
			t.Fatal("Cloud consent adopted an unsaved replacement", plan)
		}
		req.Confirmed = true
		req.Expected = map[string]string{}
		for _, item := range plan.Objects {
			req.Expected[item.Key] = item.Revision
		}
		report := configRequest(t, srv, "apply", req)
		if report.OK || f.calls.Load() != before {
			t.Fatal(report)
		}
		for _, item := range report.Objects {
			if item.Result != "not-applied" {
				t.Fatal("invalid consent mutated configuration", item)
			}
		}
	}
}
