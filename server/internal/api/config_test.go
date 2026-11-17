package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
	"github.com/woliveiras/weft/server/internal/repositoryimport/importtest"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/storage"
)

func configRequest(t *testing.T, srv *httptest.Server, path string, in installation.Request) installation.Plan {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var plan installation.Plan
	if code := call(t, srv, "POST", "/api/v1/config/"+path, adminTok, "application/json", string(b), &plan); code != 200 {
		t.Fatalf("%s: %d %+v", path, code, plan)
	}
	return plan
}
func configApplyRequest(t *testing.T, srv *httptest.Server, in installation.Request) installation.Plan {
	t.Helper()
	plan := configRequest(t, srv, "plan", in)
	if !plan.OK {
		t.Fatalf("invalid plan: %+v", plan.Objects)
	}
	in.Expected = map[string]string{}
	for _, o := range plan.Objects {
		in.Expected[o.Key] = o.Revision
	}
	in.Confirmed = true
	return configRequest(t, srv, "apply", in)
}
func yamlObject(kind, name, spec string) string {
	return "apiVersion: weft.io/v1\nkind: " + kind + "\nmetadata: {name: " + name + "}\nspec:\n" + spec + "\n"
}
func exportConfig(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/config/export", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export: %d %v %s", resp.StatusCode, err, raw)
	}
	return string(raw)
}

func TestDeclarativeCreateRoundTripSecretsAndRevision(t *testing.T) {
	srv, a := setupApp(t, nil)
	upstream := mcptest.New()
	defer upstream.Close()
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, token, upstream.Client())
	}
	t.Setenv("WEFT_SECRET_CONFIG_FIXTURE", "declarative-synthetic-secret")
	files := []string{
		yamlObject("Graph", "configured", "  defaults: {runtime: {kind: echo}}\n  nodes: [{id: worker}]"),
		yamlObject("MCPConnection", "github", "  name: GitHub\n  service: github\n  endpoint: "+upstream.URL+"\n  enabled: true\n  callsPerMinute: 60\n  secretRef: {env: WEFT_SECRET_CONFIG_FIXTURE}"),
		yamlObject("ModelProvider", "configured", "  type: gemini\n  defaultModel: gemini-3.6-flash\n  concurrency: 2\n  contextWindow: 1048576\n  secretRef: {env: WEFT_SECRET_CONFIG_FIXTURE}"),
		yamlObject("InferencePlugin", "gemini", "  enabled: true"),
		yamlObject("PriceCatalog", "installation", "  currency: USD\n  unpriced: allow\n  prices: [{provider: configured, model: '*', inputMicrosPerMTok: 1000000, outputMicrosPerMTok: 2000000}]"),
		yamlObject("CostPolicy", "installation", "  taskRunBudgetMicros: 2000000\n  warningPercent: 80"),
		yamlObject("Budget", "installation/*", "  scope: installation\n  id: '*'\n  limitMicros: 10000000\n  interruptPercent: 110"),
		yamlObject("NetworkPolicy", "installation", "  setupNetworks: [none, bridge]\n  runNetworks: [none, egress]\n  egressHosts: [proxy.golang.org, sum.golang.org, storage.googleapis.com]"),
	}
	in := installation.Request{Files: files}
	report := configApplyRequest(t, srv, in)
	if !report.OK {
		t.Fatalf("apply: %+v", report.Objects)
	}
	if report.Objects[0].Key != "InferencePlugin/gemini" {
		t.Fatalf("dependency order: %+v", report.Objects)
	}
	for _, o := range report.Objects {
		if o.Result != "applied" {
			t.Fatalf("result: %+v", o)
		}
	}
	exported := exportConfig(t, srv)
	if exported != exportConfig(t, srv) {
		t.Fatal("export not deterministic")
	}
	if !strings.Contains(exported, "preserve: true") {
		t.Fatal("missing placeholder")
	}
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	if !plan.OK {
		t.Fatalf("round-trip plan: %+v", plan.Objects)
	}
	for _, o := range plan.Objects {
		if o.Action != "unchanged" {
			t.Fatalf("round-trip change: %+v", o)
		}
	}
	roundtrip := configApplyRequest(t, srv, installation.Request{Files: []string{exported}})
	if !roundtrip.OK {
		t.Fatal(roundtrip)
	}
	for _, o := range roundtrip.Objects {
		if o.Result != "unchanged" {
			t.Fatal(o)
		}
	}
	// Console editing changes this object's revision, while the original export retains its baseline.
	code := call(t, srv, "PUT", "/api/v1/providers/configured", adminTok, "application/json", `{"name":"configured","type":"gemini","defaultModel":"console-model","concurrency":2,"contextWindow":1048576}`, nil)
	if code != 200 {
		t.Fatal(code)
	}
	plan = configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	conflicts := 0
	for _, o := range plan.Objects {
		if o.Action == "conflict" {
			conflicts++
			if o.Key != "ModelProvider/configured" {
				t.Fatal(o)
			}
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflicts: %+v", plan.Objects)
	}
	report = configApplyRequest(t, srv, installation.Request{Files: []string{exported}, Force: []string{"ModelProvider/configured"}})
	if !report.OK {
		t.Fatal(report)
	}
	b, _ := json.Marshal([]any{report, plan})
	events, err := a.Store.ListEvents(context.Background(), storage.EventFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	eb, _ := json.Marshal(events)
	for _, raw := range []string{string(b), exported, string(eb)} {
		if strings.Contains(raw, "declarative-synthetic-secret") {
			t.Fatal("secret disclosed")
		}
	}
	v, err := a.Providers.Get("configured")
	if err != nil || !v.Credential.Configured {
		t.Fatalf("credential was not retained: %+v %v", v, err)
	}
}
func TestDeclarativeAuthorizationAndStalePlan(t *testing.T) {
	srv := setup(t)
	for _, path := range []string{"plan", "apply", "export"} {
		method := "POST"
		if path == "export" {
			method = "GET"
		}
		for _, token := range []string{"", viewerTok, operatorTok} {
			want := 403
			if token == "" {
				want = 401
			}
			if code := call(t, srv, method, "/api/v1/config/"+path, token, "application/json", `{}`, nil); code != want {
				t.Fatalf("%s: %d", path, code)
			}
		}
	}
	raw := yamlObject("CostPolicy", "installation", "  taskRunBudgetMicros: 2000000\n  warningPercent: 80")
	in := installation.Request{Files: []string{raw}}
	plan := configRequest(t, srv, "plan", in)
	in.Expected = map[string]string{plan.Objects[0].Key: plan.Objects[0].Revision}
	in.Confirmed = true
	if code := call(t, srv, "PUT", "/api/v1/cost/policy", adminTok, "application/json", `{"taskRunBudgetMicros":3000000,"warningPercent":80}`, nil); code != 200 {
		t.Fatal(code)
	}
	in.Force = []string{"CostPolicy/installation"}
	report := configRequest(t, srv, "apply", in)
	if report.OK || report.Objects[0].Result != "not-applied" || !strings.Contains(report.Objects[0].Error, "stale") {
		t.Fatalf("stale plan accepted: %+v", report)
	}
}
func TestDeclarativeRejectsInlineAndMissingSecrets(t *testing.T) {
	srv := setup(t)
	for _, secret := range []string{"  secretRef: {env: WEFT_ADMIN_TOKEN}", "  secretRef: {env: WEFT_SECRET_UNSET_CONFIG}", "  secretRef: {file: ../secret.key}"} {
		raw := yamlObject("ModelProvider", "unsafe", "  type: gemini\n"+secret)
		plan := configRequest(t, srv, "plan", installation.Request{Files: []string{raw}})
		b, _ := json.Marshal(plan)
		if plan.OK || len(plan.Objects) != 1 || plan.Objects[0].Error == "" || strings.Contains(string(b), "do-not-echo") {
			t.Fatalf("unsafe plan: %s", b)
		}
	}
}
func TestDeclarativeNoImplicitRemoval(t *testing.T) {
	srv, a := setupApp(t, nil)
	raw := yamlObject("Graph", "kept", "  defaults: {runtime: {kind: echo}}\n  nodes: [{id: worker}]")
	report := configApplyRequest(t, srv, installation.Request{Files: []string{raw}})
	if !report.OK {
		t.Fatal(report)
	}
	report = configApplyRequest(t, srv, installation.Request{Files: []string{yamlObject("Budget", "installation/*", "  scope: installation\n  id: '*'\n  limitMicros: 10000000")}})
	if !report.OK {
		t.Fatal(report)
	}
	g, err := a.Store.GetGraph(t.Context(), "kept", 0)
	if err != nil || g.Metadata.ID != "kept" {
		t.Fatal("unmanaged graph removed")
	}
}

func TestDeclarativeTriggerConsentAndExport(t *testing.T) {
	srv, a := setupTriggerApp(t, nil)
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); c != 201 {
		t.Fatal(c)
	}
	raw := yamlObject("IssueTrigger", "weft-issues", "  workflow: task-fixture\n  nodeId: reader\n  source: {kind: github, connectionId: gh, repository: acme/app}\n  label: 'weft:todo'\n  intervalMinutes: 5\n  authorizationMode: full")
	report := configApplyRequest(t, srv, installation.Request{Files: []string{raw}})
	if !report.OK {
		t.Fatal(report)
	}
	ts, err := a.API.TriggerManager.List(t.Context(), "")
	if err != nil || len(ts) != 1 || ts[0].State != "paused" || ts[0].PausedReason != "consent required" || ts[0].Consent.Actor != "" {
		t.Fatalf("consent: %+v %v", ts, err)
	}
	in := installation.Request{Files: []string{raw}, Revisions: map[string]string{report.Objects[0].Key: report.Objects[0].Revision}, RecordConsent: true}
	report = configApplyRequest(t, srv, in)
	if !report.OK {
		t.Fatal(report)
	}
	ts, err = a.API.TriggerManager.List(t.Context(), "")
	if err != nil || ts[0].State != "active" || ts[0].Consent.Actor != "admin" || ts[0].Consent.Authorization != "full" {
		t.Fatalf("recorded consent: %+v %v", ts, err)
	}
	exported := exportConfig(t, srv)
	report = configApplyRequest(t, srv, installation.Request{Files: []string{exported}})
	if !report.OK {
		t.Fatal(report)
	}
	for _, o := range report.Objects {
		if o.Result != "unchanged" {
			t.Fatal(o)
		}
	}
}

func TestDeclarativeAllKindsImportAndRoundTrip(t *testing.T) {
	srv, a := setupTriggerApp(t, nil)
	fixture, err := importtest.New(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Server.Close()
	if err = a.RepositoryImports.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	manager, err := repositoryimport.New(repositoryimport.Options{Store: a.Store, Root: filepath.Join(t.TempDir(), "imports"), Client: fixture.Client})
	if err != nil {
		t.Fatal(err)
	}
	a.RepositoryImports = manager
	a.API.RepositoryImports = manager
	t.Setenv("WEFT_SECRET_ALL_KINDS", "all-kinds-synthetic-token")
	scmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer all-kinds-synthetic-token" {
			t.Error("SCM did not receive server-resolved token")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/user" {
			io.WriteString(w, `{"login":"fixture"}`)
		} else {
			io.WriteString(w, `{"full_name":"acme/app","default_branch":"main","permissions":{"push":true}}`)
		}
	}))
	defer scmServer.Close()
	files := []string{
		yamlObject("InferencePlugin", "gemini", "  enabled: true"),
		yamlObject("ModelProvider", "gemini", "  type: gemini\n  defaultModel: fixture\n  secretRef: {env: WEFT_SECRET_ALL_KINDS}"),
		yamlObject("SCMConnection", "weft-github", "  kind: github\n  repository: acme/app\n  apiUrl: "+scmServer.URL+"\n  requirePullRequestApproval: true\n  secretRef: {env: WEFT_SECRET_ALL_KINDS}"),
		yamlObject("Project", "fixture-project", "  url: https://repository.fixture.example/source.git\n  ref: HEAD"),
		yamlObject("Graph", "import-workflow", "  defaults: {runtime: {kind: echo}}\n  nodes: [{id: worker}]"),
		yamlObject("IssueTrigger", "import-issues", "  workflow: import-workflow\n  nodeId: worker\n  source: {kind: github, connectionId: gh, repository: acme/app}\n  projectId: fixture-project\n  scmConnection: weft-github\n  authorizationMode: full\n  intervalMinutes: 5"),
		yamlObject("Budget", "installation/*", "  scope: installation\n  id: '*'\n  limitMicros: 10000000"),
	}
	report := configApplyRequest(t, srv, installation.Request{Files: files})
	if !report.OK {
		t.Fatalf("all kinds apply: %+v", report.Objects)
	}
	revisions := map[string]string{}
	for _, o := range report.Objects {
		revisions[o.Key] = o.Revision
	}
	again := configRequest(t, srv, "plan", installation.Request{Files: files, Revisions: revisions})
	if !again.OK {
		t.Fatalf("idempotent plan: %+v", again.Objects)
	}
	for _, o := range again.Objects {
		if o.Action != "unchanged" {
			t.Fatalf("not idempotent: %+v", o)
		}
	}
	exported := exportConfig(t, srv)
	parsed, err := installation.Parse([]string{exported})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, d := range parsed {
		kinds[d.Kind] = true
	}
	for _, kind := range installation.Kinds {
		if !kinds[kind] {
			t.Fatalf("missing exported kind %s", kind)
		}
	}
	roundtrip := configApplyRequest(t, srv, installation.Request{Files: []string{exported}})
	if !roundtrip.OK {
		t.Fatal(roundtrip)
	}
	for _, o := range roundtrip.Objects {
		if o.Result != "unchanged" {
			t.Fatal(o)
		}
	}
	projects, err := (projectconfig.Manager{Store: a.Store}).List(t.Context())
	if err != nil || len(projects) != 1 || projects[0].Head != fixture.Commit {
		t.Fatalf("project import not idempotent: %+v %v", projects, err)
	}
}

func TestDeclarativePartialFailureAndRetry(t *testing.T) {
	srv, a := setupApp(t, nil)
	wrapped := &configFailStore{Store: a.Store}
	a.API.Store = wrapped
	in := installation.Request{Files: []string{yamlObject("CostPolicy", "installation", "  taskRunBudgetMicros: 2000000\n  warningPercent: 80"), yamlObject("Budget", "installation/*", "  scope: installation\n  id: '*'\n  limitMicros: 10000000"), yamlObject("NetworkPolicy", "installation", "  setupNetworks: [none]\n  runNetworks: [none]\n  egressHosts: []")}}
	plan := configRequest(t, srv, "plan", in)
	if !plan.OK {
		t.Fatal(plan)
	}
	in.Expected = map[string]string{}
	for _, o := range plan.Objects {
		in.Expected[o.Key] = o.Revision
	}
	in.Confirmed = true
	wrapped.fail = true
	report := configRequest(t, srv, "apply", in)
	if report.OK || report.Objects[0].Result != "applied" || report.Objects[1].Result != "failed" || report.Objects[2].Result != "not-applied" {
		t.Fatalf("partial report: %+v", report)
	}
	wrapped.fail = false
	in.Revisions = map[string]string{report.Objects[0].Key: report.Objects[0].Revision}
	report = configApplyRequest(t, srv, in)
	if !report.OK || report.Objects[0].Result != "unchanged" {
		t.Fatalf("retry: %+v", report)
	}
}

type configFailStore struct {
	storage.Store
	fail bool
}

func (s *configFailStore) PutSetting(ctx context.Context, key string, value []byte, rev int64, actor string) (int64, error) {
	if s.fail && key == "cost/definitions" {
		return 0, fmt.Errorf("fixture storage unavailable")
	}
	return s.Store.PutSetting(ctx, key, value, rev, actor)
}

func TestDeclarativeSecretNeverReachesLogsOrEvents(t *testing.T) {
	// Capture the logger's actual stdout sink from application construction onward.
	captured, err := os.CreateTemp(t.TempDir(), "logs")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = captured
	t.Cleanup(func() { os.Stdout = original; captured.Close() })
	srv, a := setupApp(t, func(c *config.Config) { c.Observability.LogLevel = "debug" })
	t.Setenv("WEFT_SECRET_LOG_FIXTURE", "unique-secret-log-fixture")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"message":"unique-secret-log-fixture"}`)
	}))
	defer upstream.Close()
	raw := yamlObject("SCMConnection", "logs", "  kind: github\n  repository: acme/app\n  apiUrl: "+upstream.URL+"\n  secretRef: {env: WEFT_SECRET_LOG_FIXTURE}")
	report := configApplyRequest(t, srv, installation.Request{Files: []string{raw}})
	if !report.OK || !strings.Contains(report.Objects[0].Status, "unavailable") {
		t.Fatal(report)
	}
	exported := exportConfig(t, srv)
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	events, err := a.Store.ListEvents(t.Context(), storage.EventFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal([]any{plan, report, events})
	logBytes, err := os.ReadFile(captured.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(logBytes) == 0 {
		t.Fatal("log capture was empty")
	}
	for _, text := range []string{exported, string(payload), string(logBytes)} {
		if strings.Contains(text, "unique-secret-log-fixture") {
			t.Fatal("credential leaked in observable output")
		}
	}
}
func TestDeclarativeKindValidationPaths(t *testing.T) {
	srv := setup(t)
	cases := map[string]string{
		"InferencePlugin": "  enabled: nope", "ModelProvider": "  type: missing", "PriceCatalog": "  currency: INVALID\n  prices: []", "CostPolicy": "  warningPercent: 101", "Budget": "  scope: invalid\n  limitMicros: -1", "NetworkPolicy": "  setupNetworks: []\n  runNetworks: []", "MCPConnection": "  endpoint: invalid", "SCMConnection": "  repository: invalid", "Project": "  url: file:///etc/passwd", "Graph": "  nodes: [{id: duplicate}, {id: duplicate}]", "IssueTrigger": "  authorizationMode: root"}
	for kind, spec := range cases {
		t.Run(kind, func(t *testing.T) {
			name := "installation"
			if kind == "InferencePlugin" {
				name = "gemini"
			}
			plan := configRequest(t, srv, "plan", installation.Request{Files: []string{yamlObject(kind, name, spec)}})
			if plan.OK || plan.Objects[0].Action != "error" || !strings.Contains(plan.Objects[0].Error, ":") {
				t.Fatalf("missing field error: %+v", plan)
			}
		})
	}
}
func TestDeclarativeDiscoveryDoesNotInvalidateRevision(t *testing.T) {
	srv, a := setupApp(t, nil)
	upstream := mcptest.New()
	defer upstream.Close()
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, token, upstream.Client())
	}
	t.Setenv("WEFT_SECRET_MCP_FIXTURE", "fixture-mcp-token")
	raw := yamlObject("MCPConnection", "fixture", "  name: Fixture\n  service: generic\n  endpoint: "+upstream.URL+"\n  enabled: true\n  secretRef: {env: WEFT_SECRET_MCP_FIXTURE}")
	report := configApplyRequest(t, srv, installation.Request{Files: []string{raw}})
	if !report.OK || report.Objects[0].Status != "ready" {
		t.Fatal(report)
	}
	exported := exportConfig(t, srv)
	if _, err := a.MCP.Refresh(t.Context(), "fixture", "operator"); err != nil {
		t.Fatal(err)
	}
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	if !plan.OK {
		t.Fatal(plan)
	}
	for _, o := range plan.Objects {
		if o.Action != "unchanged" {
			t.Fatal(o)
		}
	}
}

func TestDeclarativeDisabledProviderRoundTrip(t *testing.T) {
	srv := setup(t)
	t.Setenv("WEFT_SECRET_DISABLED_FIXTURE", "disabled-provider-fixture")
	in := installation.Request{Files: []string{yamlObject("InferencePlugin", "gemini", "  enabled: true"), yamlObject("ModelProvider", "disabled", "  type: gemini\n  secretRef: {env: WEFT_SECRET_DISABLED_FIXTURE}")}}
	report := configApplyRequest(t, srv, in)
	if !report.OK {
		t.Fatal(report)
	}
	if code := call(t, srv, "PUT", "/api/v1/provider-plugins/gemini", adminTok, "application/json", `{"enabled":false}`, nil); code != 200 {
		t.Fatal(code)
	}
	exported := exportConfig(t, srv)
	report = configApplyRequest(t, srv, installation.Request{Files: []string{exported}})
	if !report.OK {
		t.Fatal(report)
	}
	for _, o := range report.Objects {
		if o.Result != "unchanged" {
			t.Fatal(o)
		}
	}
}

func TestDeclarativeFileCredentialAndRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provider.key")
	if err := os.WriteFile(path, []byte("file-secret-one"), 0600); err != nil {
		t.Fatal(err)
	}
	srv, a := setupApp(t, func(c *config.Config) { c.Secrets.Dir = dir })
	raw := yamlObject("ModelProvider", "file-provider", "  type: gemini\n  secretRef: {file: provider.key}")
	in := installation.Request{Files: []string{yamlObject("InferencePlugin", "gemini", "  enabled: true"), raw}}
	report := configApplyRequest(t, srv, in)
	if !report.OK {
		t.Fatal(report)
	}
	in.Revisions = map[string]string{}
	for _, o := range report.Objects {
		in.Revisions[o.Key] = o.Revision
	}
	if err := os.WriteFile(path, []byte("file-secret-two"), 0600); err != nil {
		t.Fatal(err)
	}
	plan := configRequest(t, srv, "plan", in)
	if !plan.OK || plan.Objects[1].Secret != "changed" {
		t.Fatal(plan)
	}
	report = configApplyRequest(t, srv, in)
	if !report.OK {
		t.Fatal(report)
	}
	same, err := a.Providers.CredentialMatches(t.Context(), "file-provider", "file-secret-two")
	if err != nil || !same {
		t.Fatal("rotation did not store the resolved value", err)
	}
	row, err := a.Store.GetSetting(t.Context(), "llm/providers")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(row.Value), "file-secret-") {
		t.Fatal("credential stored without encryption")
	}
}

func TestDeclarativeExportHandlesDuplicateTriggerDisplayNames(t *testing.T) {
	srv, a := setupTriggerApp(t, nil)
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", taskGraph, nil); code != 201 {
		t.Fatal(code)
	}
	for i := 0; i < 2; i++ {
		if code := call(t, srv, "POST", "/api/v1/triggers", adminTok, "application/json", triggerBody, nil); code != 201 {
			t.Fatal(code)
		}
	}
	exported := exportConfig(t, srv)
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	if !plan.OK {
		t.Fatal(plan)
	}
	for _, o := range plan.Objects {
		if o.Action != "unchanged" {
			t.Fatal(o)
		}
	}
	triggers, err := a.API.TriggerManager.List(t.Context(), "")
	if err != nil || len(triggers) != 2 {
		t.Fatal(err)
	}
	raw := yamlObject("IssueTrigger", "Ready", "  workflow: task-fixture\n  nodeId: reader\n  source: {kind: github, connectionId: gh, repository: acme/app}\n  authorizationMode: full")
	plan = configRequest(t, srv, "plan", installation.Request{Files: []string{raw}})
	if plan.OK || !strings.Contains(plan.Objects[0].Error, "ambiguous") {
		t.Fatal(plan)
	}
}

func TestDeclarativeBudgetRevisionDetectsRestoredConsoleEdit(t *testing.T) {
	srv := setup(t)
	const initial = `{"budgets":[{"scope":"installation","id":"*","limitMicros":10000000},{"scope":"user","id":"alice","limitMicros":2000000}]}`
	put := func(body string) {
		t.Helper()
		if code := call(t, srv, "PUT", "/api/v1/cost/budgets", adminTok, "application/json", body, nil); code != 200 {
			t.Fatalf("console budget update: %d", code)
		}
	}
	put(initial)
	exported := exportConfig(t, srv)
	put(strings.Replace(initial, "10000000", "11000000", 1))
	put(initial)
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{exported}})
	if plan.OK {
		t.Fatal("restoring an edited budget must not restore its prior revision")
	}
	for _, o := range plan.Objects {
		want := "unchanged"
		if o.Key == "Budget/installation/*" {
			want = "conflict"
		}
		if o.Action != want {
			t.Fatalf("%s: want %s, got %s", o.Key, want, o.Action)
		}
	}
}
