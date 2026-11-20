package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/storage"
)

type configMappingFixture struct {
	remote         *httptest.Server
	calls          atomic.Int32
	mode           atomic.Int32
	beforeMetadata func()
}

func newConfigMappingFixture(t *testing.T) *configMappingFixture {
	t.Helper()
	f := &configMappingFixture{}
	f.remote = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Method != "GET" {
			t.Error("declarative mapping wrote to Jira")
			w.WriteHeader(405)
			return
		}
		if f.mode.Load() == 1 {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/jira/rest/api/2/issue/APP-7":
			updated := "2026-11-20T12:00:00.000+0000"
			if f.mode.Load() == 2 {
				updated = "2026-11-20T12:01:00.000+0000"
			}
			fmt.Fprintf(w, `{"id":"10007","key":"APP-7","fields":{"summary":"Sample","updated":%q,"issuelinks":[],"project":{"id":"10001"},"issuetype":{"id":"10002"},"status":{"id":"10","name":"Ready","statusCategory":{"key":"new"}}}}`, updated)
		case "/jira/rest/api/2/issue/10007/transitions":
			if f.beforeMetadata != nil {
				f.beforeMetadata()
			}
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Revisão","to":{"id":"20","name":"Review"},"fields":{}}]}`)
		default:
			t.Error("unexpected scope", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.remote.Close)
	old := http.DefaultTransport
	http.DefaultTransport = f.remote.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })
	return f
}
func configMappingYAML(name, connection string, revision int64) string {
	return yamlObject("IssueWorkflowMapping", name, fmt.Sprintf("  connection: %s\n  connectionRevision: %d\n  sampleIssueKey: APP-7\n  sampleIssueId: '10007'\n  issueTypeId: '10002'\n  fromStatusId: '10'\n  transitionId: '31'\n  targetStatusId: '20'\n  issueUpdated: '2026-11-20T12:00:00.000+0000'\n  enabled: true", connection, revision))
}
func configMappingConnectionYAML(f *configMappingFixture, name string) string {
	return yamlObject("IssueConnection", name, "  kind: jira-dc\n  siteUrl: "+f.remote.URL+"/jira\n  projectId: '10001'\n  enabled: true\n  projectRestricted: true\n  secretRef: {env: WEFT_SECRET_MAPPING_CONFIG_FIXTURE}")
}
func TestDeclarativeWorkflowMappingOrdersDependenciesAndNoopRoundTrips(t *testing.T) {
	f := newConfigMappingFixture(t)
	srv, a := setupApp(t, nil)
	t.Setenv("WEFT_SECRET_MAPPING_CONFIG_FIXTURE", "mapping-config-fixture-token")
	raw := configMappingYAML("review-step", "native", 1)
	connection := configMappingConnectionYAML(f, "native")
	req := installation.Request{Files: []string{raw, connection}}
	plan := configRequest(t, srv, "plan", req)
	if !plan.OK || plan.Objects[0].Key != "IssueConnection/native" || plan.Objects[1].Key != "IssueWorkflowMapping/review-step" || f.calls.Load() != 0 {
		t.Fatal(plan)
	}
	if _, err := (jiradc.WorkflowMappings{Connections: a.IssueConnections}).Get(t.Context(), "review-step"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("plan saved mapping", err)
	}
	report := configApplyRequest(t, srv, req)
	if !report.OK || report.Objects[1].Result != "applied" || f.calls.Load() == 0 {
		t.Fatal(report)
	}
	var saved jiradc.WorkflowMapping
	if code := call(t, srv, "GET", "/api/v1/issue-workflow-mappings/review-step", viewerTok, "", "", &saved); code != 200 || saved.Revision != 1 || saved.ConnectionRevision != 1 || saved.TransitionID != "31" || saved.TransitionName != "Revisão" {
		t.Fatal(code, saved)
	}
	exported := exportConfig(t, srv)
	if exported != exportConfig(t, srv) || !strings.Contains(exported, "kind: IssueWorkflowMapping") || strings.Contains(exported, "mapping-config-fixture-token") || strings.Contains(exported, "transitionName") {
		t.Fatal("unsafe or incomplete export")
	}
	before := f.calls.Load()
	events, err := a.Store.ListEvents(t.Context(), storage.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	again := configApplyRequest(t, srv, installation.Request{Files: []string{exported}})
	if !again.OK {
		t.Fatal(again)
	}
	for _, o := range again.Objects {
		if o.Result != "unchanged" {
			t.Fatal(o)
		}
	}
	afterEvents, err := a.Store.ListEvents(t.Context(), storage.EventFilter{})
	if err != nil || len(afterEvents) != len(events) || f.calls.Load() != before {
		t.Fatal("no-op dispatched discovery or audit", err)
	}
	// A changed definition requires explicit adoption of this mapping only.
	disable := strings.Replace(raw, "enabled: true", "enabled: false", 1)
	plan = configRequest(t, srv, "plan", installation.Request{Files: []string{disable}})
	if plan.OK || plan.Objects[0].Action != "conflict" {
		t.Fatal(plan)
	}
	f.mode.Store(1)
	report = configApplyRequest(t, srv, installation.Request{Files: []string{disable}, Force: []string{"IssueWorkflowMapping/review-step"}})
	if !report.OK || report.Objects[0].Result != "applied" || f.calls.Load() != before {
		t.Fatal("exact disable required provider access", report)
	}
	// The exported baseline cannot overwrite the later console/YAML revision.
	plan = configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	if plan.OK {
		t.Fatal("stale baseline accepted", plan)
	}
}

func TestDeclarativeWorkflowMappingRejectsInvalidShapeAndDependenciesWithoutDiscovery(t *testing.T) {
	f := newConfigMappingFixture(t)
	srv, _ := setupApp(t, nil)
	t.Setenv("WEFT_SECRET_MAPPING_CONFIG_FIXTURE", "mapping-config-fixture-token")
	raw := configMappingYAML("review-step", "native", 1)
	connection := configMappingConnectionYAML(f, "native")
	for _, input := range []string{
		strings.Replace(raw, "transitionId: '31'", "transitionId: Review", 1),
		strings.Replace(raw, "targetStatusId: '20'", "targetStatusId: '10'", 1),
		strings.Replace(raw, "connectionRevision: 1", "connectionRevision: 0", 1),
		strings.Replace(raw, "connectionRevision: 1", "connectionRevision: 2", 1),
		raw + "  transitionName: Review\n",
	} {
		plan := configRequest(t, srv, "plan", installation.Request{Files: []string{input, connection}, Force: []string{"IssueWorkflowMapping/review-step"}})
		if plan.OK || f.calls.Load() != 0 {
			t.Fatal("invalid dry-run accepted or dispatched", plan)
		}
	}
	for _, files := range [][]string{{raw}, {raw, strings.Replace(connection, "projectRestricted: true", "projectRestricted: false", 1)}, {raw, strings.Replace(connection, "enabled: true", "enabled: false", 1)}} {
		if plan := configRequest(t, srv, "plan", installation.Request{Files: files}); plan.OK || f.calls.Load() != 0 {
			t.Fatal(plan)
		}
	}
}

func TestDeclarativeWorkflowMappingFailurePreservesPartialApplyAndRetries(t *testing.T) {
	for _, mode := range []int32{1, 2} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := newConfigMappingFixture(t)
			srv, a := setupApp(t, nil)
			t.Setenv("WEFT_SECRET_MAPPING_CONFIG_FIXTURE", "mapping-config-fixture-token")
			req := installation.Request{Files: []string{configMappingYAML("review-step", "native", 1), configMappingConnectionYAML(f, "native"), yamlObject("Graph", "later", "  defaults: {runtime: {kind: echo}}\n  nodes: [{id: worker}]")}}
			f.mode.Store(mode)
			report := configApplyRequest(t, srv, req)
			if report.OK || report.Objects[0].Result != "applied" || report.Objects[1].Result != "failed" || report.Objects[2].Result != "not-applied" {
				t.Fatal(report)
			}
			if _, err := (jiradc.WorkflowMappings{Connections: a.IssueConnections}).Get(t.Context(), "review-step"); !errors.Is(err, storage.ErrNotFound) {
				t.Fatal("failed sample saved mapping", err)
			}
			f.mode.Store(0)
			report = configApplyRequest(t, srv, req)
			if !report.OK || report.Objects[0].Result != "unchanged" || report.Objects[1].Result != "applied" || report.Objects[2].Result != "applied" {
				t.Fatal(report)
			}
		})
	}
}

func TestDeclarativeWorkflowMappingRejectsConcurrentConnectionChangeAndStalePlanEvenForced(t *testing.T) {
	f := newConfigMappingFixture(t)
	srv, a := setupApp(t, nil)
	t.Setenv("WEFT_SECRET_MAPPING_CONFIG_FIXTURE", "mapping-config-fixture-token")
	configApplyRequest(t, srv, installation.Request{Files: []string{configMappingConnectionYAML(f, "native")}})
	req := installation.Request{Files: []string{configMappingYAML("review-step", "native", 1)}, Force: []string{"IssueWorkflowMapping/review-step"}}
	f.beforeMetadata = func() {
		c, err := a.IssueConnections.Get(t.Context(), "native")
		if err != nil {
			t.Error(err)
			return
		}
		_, err = a.IssueConnections.Put(t.Context(), jiradc.ConnectionInput{Name: c.Name, Kind: c.Kind, SiteURL: c.SiteURL, ProjectID: c.ProjectID, Enabled: false, ProjectRestricted: true}, c.Revision, "other-instance")
		if err != nil {
			t.Error(err)
		}
	}
	report := configApplyRequest(t, srv, req)
	if report.OK || report.Objects[0].Result != "failed" {
		t.Fatal("concurrent scope accepted", report)
	}
	if _, err := (jiradc.WorkflowMappings{Connections: a.IssueConnections}).Get(t.Context(), "review-step"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	f.beforeMetadata = nil
	c, err := a.IssueConnections.Get(t.Context(), "native")
	if err != nil {
		t.Fatal(err)
	}
	c, err = a.IssueConnections.Put(t.Context(), jiradc.ConnectionInput{Name: c.Name, Kind: c.Kind, SiteURL: c.SiteURL, ProjectID: c.ProjectID, Enabled: true, ProjectRestricted: true}, c.Revision, "other-instance")
	if err != nil {
		t.Fatal(err)
	}
	req.Files[0] = configMappingYAML("review-step", "native", c.Revision)
	report = configApplyRequest(t, srv, req)
	if !report.OK {
		t.Fatal(report)
	}
	req.Files[0] = strings.Replace(req.Files[0], "enabled: true", "enabled: false", 1)
	plan := configRequest(t, srv, "plan", req)
	if !plan.OK {
		t.Fatal(plan)
	}
	mapping, err := (jiradc.WorkflowMappings{Connections: a.IssueConnections}).Get(t.Context(), "review-step")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (jiradc.WorkflowMappings{Connections: a.IssueConnections}).Put(t.Context(), mapping.MappingInput, mapping.Revision, "other-instance"); err != nil {
		t.Fatal(err)
	}
	before := f.calls.Load()
	req.Confirmed = true
	req.Expected = map[string]string{plan.Objects[0].Key: plan.Objects[0].Revision}
	report = configRequest(t, srv, "apply", req)
	if report.OK || report.Objects[0].Result != "not-applied" || f.calls.Load() != before {
		t.Fatal("force bypassed stale reviewed plan", report)
	}
}

func TestDeclarativeWorkflowMappingRequiresSavedDefinitionBeforeTriggerConsent(t *testing.T) {
	f := newConfigMappingFixture(t)
	srv, a := setupApp(t, nil)
	t.Setenv("WEFT_SECRET_MAPPING_CONFIG_FIXTURE", "mapping-config-fixture-token")
	triggerDoc := yamlObject("IssueTrigger", "issues", "  workflow: later\n  nodeId: worker\n  source: {kind: jira-dc, connectionId: native, siteUrl: "+f.remote.URL+"/jira, projectId: '10001'}\n  authorizationMode: ask\n  readinessDisabled: true\n  lifecycleMappings: {prDelivered: {name: review-step, revision: 1}}")
	req := installation.Request{Files: []string{configMappingYAML("review-step", "native", 1), configMappingConnectionYAML(f, "native"), triggerDoc}}
	plan := configRequest(t, srv, "plan", req)
	if plan.OK || len(plan.Objects) != 3 || plan.Objects[2].Action != "error" || !strings.Contains(plan.Objects[2].Error, "apply mapping definitions first") || f.calls.Load() != 0 {
		t.Fatal(plan)
	}
	req.Confirmed = true
	req.Expected = map[string]string{}
	for _, o := range plan.Objects {
		req.Expected[o.Key] = o.Revision
	}
	report := configRequest(t, srv, "apply", req)
	if report.OK || f.calls.Load() != 0 {
		t.Fatal(report)
	}
	for _, o := range report.Objects {
		if o.Result != "not-applied" {
			t.Fatal(o)
		}
	}
	rows, err := a.Store.ListSettings(t.Context(), jiradc.ConnectionPrefix)
	if err != nil || len(rows) != 0 {
		t.Fatal("invalid adoption mutated configuration", err)
	}
}
