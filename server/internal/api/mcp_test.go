package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/tools"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
)

func TestMCPRegistryDiscoveryAndPermissions(t *testing.T) {
	srv, a := setupApp(t, nil)
	upstream := mcptest.New()
	defer upstream.Close()
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, token, upstream.Client())
	}
	in := mcpconfig.Input{ID: "team", Name: "Team", Service: "github", Endpoint: upstream.URL, Enabled: true, Credential: &mcpconfig.Credential{Value: "fixture-mcp-token"}}
	body, _ := json.Marshal(in)
	path := "/api/v1/mcp/connections"
	var v mcpconfig.View
	if code := call(t, srv, "POST", path, viewerTok, "application/json", string(body), nil); code != 403 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", path, adminTok, "application/json", string(body), &v); code != 201 || !v.CredentialConfigured {
		t.Fatal(code, v)
	}
	if code := call(t, srv, "POST", path+"/team/tools/refresh", viewerTok, "", "", nil); code != 403 {
		t.Fatal(code)
	}
	if code := call(t, srv, "POST", path+"/team/tools/refresh", operatorTok, "", "", &v); code != 200 || v.Status != "ready" || len(v.Tools) != 2 {
		t.Fatal(code, v)
	}
	var safe any
	if code := call(t, srv, "GET", path, viewerTok, "", "", &safe); code != 200 {
		t.Fatal(code)
	}
	b, _ := json.Marshal(safe)
	if strings.Contains(string(b), "fixture-mcp-token") || strings.Contains(string(b), "sealed") {
		t.Fatal("secret disclosed")
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "mcp-graph", Name: "MCP"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "worker", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}, Tools: []domain.ToolGrant{{Name: "mcp.team.search"}}}}}}
	gb, _ := json.Marshal(g)
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(gb), nil); code != 201 {
		t.Fatal(code)
	}
	saved, _, e := a.Engine.GetGraphDocument(t.Context(), "mcp-graph", 0)
	if e != nil || saved.Spec.Nodes[0].Tools[0].Name != "mcp.team.search" {
		t.Fatal(saved, e)
	}
	if _, ok := a.Tools.Get("mcp.team.search"); ok {
		t.Fatal("dynamic MCP tools must not be cached in the static registry")
	}
	in.Enabled = false
	in.Credential = nil
	body, _ = json.Marshal(in)
	put := func(rev int64) int {
		req, _ := http.NewRequest("PUT", srv.URL+path+"/team", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+adminTok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", fmt.Sprint(rev))
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if code := put(1); code != 409 {
		t.Fatal("stale write", code)
	}
	if code := put(v.Revision); code != 200 {
		t.Fatal(code)
	}
	g.Metadata.ID = "blocked"
	gb, _ = json.Marshal(g)
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/json", string(gb), nil); code != 422 {
		t.Fatal("disabled grant", code)
	}
	var catalogue []any
	call(t, srv, "GET", "/api/v1/mcp/tools", viewerTok, "", "", &catalogue)
	if len(catalogue) != 0 {
		t.Fatal(catalogue)
	}
	var report struct{ Valid bool }
	if code := call(t, srv, "POST", "/api/v1/graphs/validate", adminTok, "application/json", string(gb), &report); code != 200 || report.Valid {
		t.Fatal(code, report)
	}
}
func TestMCPInputAndUpstreamFailures(t *testing.T) {
	srv, a := setupApp(t, nil)
	upstream := mcptest.New()
	defer upstream.Close()
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, token, upstream.Client())
	}
	for _, body := range []string{`{}`, `{"id":"bad","credential":{"unexpected":"secret"}}`, `{} {}`, `{"id":"team","name":"Team","service":"generic","endpoint":"http://localhost","enabled":true}`} {
		if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", body, nil); code != 400 && code != 422 {
			t.Fatal(code)
		}
	}
	body := fmt.Sprintf(`{"id":"noauth","name":"No auth","service":"generic","endpoint":%q,"enabled":true}`, upstream.URL)
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", body, nil); code != 201 {
		t.Fatal(code)
	}
	var v mcpconfig.View
	if code := call(t, srv, "POST", "/api/v1/mcp/connections/noauth/test", operatorTok, "", "", &v); code != 200 || v.Status != "unavailable" || len(v.Tools) != 0 {
		t.Fatal(code, v)
	}
	if code := call(t, srv, "PUT", "/api/v1/mcp/connections/noauth", adminTok, "application/json", body, nil); code != 400 {
		t.Fatal("missing revision", code)
	}
}

func TestMCPGatewayWithTLSAndPersistedRegistry(t *testing.T) {
	_, a := setupApp(t, nil)
	upstream := mcptest.NewRecovering()
	defer upstream.Close()
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, token, upstream.Client())
	}
	a.MCP.Call = func(ctx context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
		return mcpconfig.CallWithClient(ctx, endpoint, token, name, args, upstream.Client())
	}
	in := mcpconfig.Input{ID: "remote", Name: "Remote", Service: "generic", Endpoint: upstream.URL, Enabled: true, Credential: &mcpconfig.Credential{Value: "fixture-mcp-token"}}
	if _, err := a.MCP.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	v, err := a.MCP.Refresh(t.Context(), in.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	p := &tools.Principal{GraphID: "graph", NodeID: "allowed", Grants: []domain.ToolGrant{{Name: "mcp.remote.search"}}}
	result, err := a.Gateway.Invoke(t.Context(), p, "mcp.remote.search", json.RawMessage(`{"query":"issue"}`))
	if err != nil || result.IsError || !strings.Contains(result.Content, "MCP fixture search completed") {
		t.Fatal(result, err)
	}
	other := &tools.Principal{GraphID: "graph", NodeID: "other", Grants: []domain.ToolGrant{{Name: "*"}}}
	if _, err = a.Gateway.Invoke(t.Context(), other, "mcp.remote.search", nil); !errors.Is(err, tools.ErrDenied) {
		t.Fatal(err)
	}
	in.Enabled = false
	in.Credential = nil
	if _, err = a.MCP.Put(t.Context(), in, v.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Gateway.Invoke(t.Context(), p, "mcp.remote.search", nil); !errors.Is(err, tools.ErrDenied) {
		t.Fatal(err)
	}
}

func TestMCPVersionedTemplatesPersistence(t *testing.T) {
	srv, a := setupApp(t, nil)
	var catalogue []mcpconfig.Template
	if code := call(t, srv, "GET", "/api/v1/mcp/templates", viewerTok, "", "", &catalogue); code != 200 || len(catalogue) != 9 {
		t.Fatal(code, catalogue)
	}
	template := catalogue[0]
	in := mcpconfig.Input{ID: "from-template", Name: "From template", Service: template.Service, Endpoint: template.Endpoint, Enabled: true, Template: &template.TemplateRef}
	body, _ := json.Marshal(in)
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", operatorTok, "application/json", string(body), nil); code != 403 {
		t.Fatal(code)
	}
	var saved mcpconfig.View
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), &saved); code != 201 {
		t.Fatal(code)
	}
	loaded, err := a.MCP.Get(t.Context(), in.ID)
	if err != nil || loaded.Template == nil || *loaded.Template != template.TemplateRef || loaded.Status != "untested" || len(loaded.Tools) != 0 {
		t.Fatal(loaded, err)
	}
	var read mcpconfig.View
	if code := call(t, srv, "GET", "/api/v1/mcp/connections/"+in.ID, viewerTok, "", "", &read); code != 200 || read.Template == nil || *read.Template != template.TemplateRef {
		t.Fatal(code, read)
	}
	for _, bad := range []mcpconfig.Input{
		{ID: "bad-version", Name: "Bad", Service: template.Service, Endpoint: template.Endpoint, Template: &mcpconfig.TemplateRef{ID: template.ID, Version: 999}},
		{ID: "bad-endpoint", Name: "Bad", Service: template.Service, Endpoint: "https://other.example/mcp", Template: &template.TemplateRef},
	} {
		body, _ = json.Marshal(bad)
		if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), nil); code != 422 {
			t.Fatal(code)
		}
		if _, err := a.MCP.Get(t.Context(), bad.ID); err == nil {
			t.Fatal("invalid template persisted")
		}
	}
}

func TestMCPDeletionAPIAndGateway(t *testing.T) {
	srv, a := setupApp(t, nil)
	in := mcpconfig.Input{ID: "delete-me", Name: "Delete", Service: "generic", Endpoint: "https://example.com/mcp", Enabled: false, Credential: &mcpconfig.Credential{Value: "synthetic-secret"}}
	v, err := a.MCP.Put(t.Context(), in, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	other := in
	other.ID = "keep-me"
	other.Name = "Keep"
	if _, err = a.MCP.Put(t.Context(), other, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	remove := func(token, revision string) int {
		req, _ := http.NewRequest("DELETE", srv.URL+"/api/v1/mcp/connections/delete-me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("If-Match", revision)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	for _, tok := range []string{viewerTok, operatorTok} {
		if code := remove(tok, fmt.Sprint(v.Revision)); code != 403 {
			t.Fatal(code)
		}
	}
	if code := remove(adminTok, ""); code != 400 {
		t.Fatal(code)
	}
	if code := remove(adminTok, "999"); code != 409 {
		t.Fatal(code)
	}
	if code := remove(adminTok, fmt.Sprint(v.Revision)); code != 204 {
		t.Fatal(code)
	}
	if code := call(t, srv, "GET", "/api/v1/mcp/connections/delete-me", viewerTok, "", "", nil); code != 404 {
		t.Fatal(code)
	}
	if _, err = a.MCP.Get(t.Context(), other.ID); err != nil {
		t.Fatal("other connection affected", err)
	}
	principal := &tools.Principal{GraphID: "graph", NodeID: "worker", Grants: []domain.ToolGrant{{Name: "mcp.delete-me.search"}}}
	if _, err = a.Gateway.Invoke(t.Context(), principal, "mcp.delete-me.search", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal(err)
	}
	body, _ := json.Marshal(in)
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), nil); code != 409 {
		t.Fatal("deleted id reused", code)
	}
}

func TestMCPQuotaAndSafeHistoryAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	upstream := mcptest.New()
	defer upstream.Close()
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, endpoint, token, upstream.Client())
	}
	a.MCP.Call = func(ctx context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
		return mcpconfig.CallWithClient(ctx, endpoint, token, name, args, upstream.Client())
	}
	now := time.Now()
	a.MCP.QuotaNow = func() time.Time { return now }
	in := mcpconfig.Input{ID: "quota", Name: "Quota", Service: "generic", Endpoint: upstream.URL, Enabled: true, CallsPerMinute: 1, Credential: &mcpconfig.Credential{Value: "fixture-mcp-token"}}
	body, _ := json.Marshal(in)
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), nil); code != 201 {
		t.Fatal(code)
	}
	if _, err := a.MCP.Refresh(t.Context(), in.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	p := &tools.Principal{GraphID: "quota-graph", NodeID: "first", Grants: []domain.ToolGrant{{Name: "mcp.quota.search"}}}
	if result, err := a.Gateway.Invoke(t.Context(), p, "mcp.quota.search", json.RawMessage(`{}`)); err != nil || result.IsError {
		t.Fatal(result, err)
	}
	// A fresh manager over the same SQL store cannot reset the aggregate quota.
	restarted := mcpconfig.New(a.MCP.Store, a.MCP.Box, nil)
	restarted.Call = a.MCP.Call
	restarted.QuotaNow = a.MCP.QuotaNow
	a.Gateway.ResolveTool = restarted.Resolve
	p.NodeID = "second"
	if _, err := a.Gateway.Invoke(t.Context(), p, "mcp.quota.search", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrRateLimited) {
		t.Fatal(err)
	}
	approval := domain.NewEvent(domain.EvApprovalDone, p.GraphID, map[string]any{"id": "approval-one", "kind": "tool", "status": "approved", "detail": map[string]any{"tool": "mcp.quota.search", "args": "private-argument"}, "comment": "private-comment"}).WithNode(p.NodeID, 0)
	if err := a.Store.AppendEvent(t.Context(), approval); err != nil {
		t.Fatal(err)
	}
	var page mcpconfig.HistoryPage
	path := "/api/v1/mcp/history?connection=quota&graph=quota-graph&node=second&type=approval.decided&outcome=approved&limit=1"
	if code := call(t, srv, "GET", path, viewerTok, "", "", &page); code != 200 || len(page.Entries) != 1 || page.Entries[0].Approval != "approval-one" {
		t.Fatal(code, page)
	}
	safe, _ := json.Marshal(page)
	for _, secret := range []string{"private-comment", "private-argument", "fixture-mcp-token"} {
		if strings.Contains(string(safe), secret) {
			t.Fatal("history exposed private data")
		}
	}
	for _, query := range []string{"limit=0", "before=-1", "type=log", "outcome=bad", "since=not-a-date", "unknown=1", "connection=x&connection=y"} {
		code := call(t, srv, "GET", "/api/v1/mcp/history?"+query, viewerTok, "", "", nil)
		if code != 400 && code != 422 {
			t.Fatal(query, code)
		}
	}
	if code := call(t, srv, "GET", "/api/v1/mcp/history", "", "", "", nil); code != 401 {
		t.Fatal(code)
	}
}

func TestMCPTemplateParametersAPI(t *testing.T) {
	srv, a := setupApp(t, nil)
	in := mcpconfig.Input{ID: "template-profile", Name: "Profile", Service: "github", Endpoint: "https://api.githubcopilot.com/mcp/x/repos/readonly", Template: &mcpconfig.TemplateRef{ID: "github", Version: 2}, TemplateParameters: map[string]string{"toolset": "repos"}}
	body, _ := json.Marshal(in)
	if c := call(t, srv, "POST", "/api/v1/mcp/connections", viewerTok, "application/json", string(body), nil); c != 403 {
		t.Fatal(c)
	}
	if c := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), nil); c != 201 {
		t.Fatal(c)
	}
	got, err := a.MCP.Get(t.Context(), in.ID)
	if err != nil || got.TemplateParameters["toolset"] != "repos" || len(got.Tools) != 0 {
		t.Fatal(got, err)
	}
	in.TemplateParameters["toolset"] = "issues"
	body, _ = json.Marshal(in)
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/mcp/connections/"+in.ID, adminTok, string(body), map[string]string{"If-Match": "1"}); c != 422 {
		t.Fatal("parameter mismatch", c)
	}
	in.Endpoint = "https://api.githubcopilot.com/mcp/x/issues/readonly"
	body, _ = json.Marshal(in)
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/mcp/connections/"+in.ID, adminTok, string(body), map[string]string{"If-Match": "1"}); c != 200 {
		t.Fatal(c)
	}
	got, err = a.MCP.Get(t.Context(), in.ID)
	if err != nil || got.Revision != 2 || got.TemplateParameters["toolset"] != "issues" || len(got.Tools) != 0 {
		t.Fatal(got, err)
	}
	if c, _, _ := rawCall(t, "PUT", srv.URL+"/api/v1/mcp/connections/"+in.ID, adminTok, string(body), map[string]string{"If-Match": "1"}); c != 409 {
		t.Fatal("stale template update", c)
	}
}

func TestSentryTemplateConfigurationDoesNotAuthorize(t *testing.T) {
	srv, a := setupApp(t, nil)
	in := mcpconfig.Input{ID: "sentry-profile", Name: "Sentry profile", Service: "sentry", Endpoint: "https://mcp.sentry.dev/mcp", Template: &mcpconfig.TemplateRef{ID: "sentry", Version: 1}, OAuth: &mcpconfig.OAuthConfig{Issuer: "https://mcp.sentry.dev", ClientID: "registered-client", Scopes: []string{"org:read"}}}
	body, _ := json.Marshal(in)
	path := "/api/v1/mcp/connections"
	if code := call(t, srv, "POST", path, viewerTok, "application/json", string(body), nil); code != 403 {
		t.Fatal(code)
	}
	var saved mcpconfig.View
	if code := call(t, srv, "POST", path, adminTok, "application/json", string(body), &saved); code != 201 {
		t.Fatal(code)
	}
	loaded, err := a.MCP.Get(t.Context(), in.ID)
	if err != nil || loaded.OAuth == nil || loaded.OAuth.Status != "authorization_required" || len(loaded.Tools) != 0 || loaded.CredentialConfigured {
		t.Fatal(loaded, err)
	}
	in.ID = "sentry-bearer"
	in.OAuth = nil
	body, _ = json.Marshal(in)
	if code := call(t, srv, "POST", path, adminTok, "application/json", string(body), nil); code != 422 {
		t.Fatal(code)
	}
	if _, err := a.MCP.Get(t.Context(), in.ID); err == nil {
		t.Fatal("invalid template persisted")
	}
}

func TestDatabricksTemplatePersistsReviewedParameters(t *testing.T) {
	srv, a := setupApp(t, nil)
	in := mcpconfig.Input{ID: "workspace-profile", Name: "Workspace profile", Service: "databricks", Endpoint: "https://data.example/ai-gateway/mcp-services/team.data.reader", Template: &mcpconfig.TemplateRef{ID: "databricks", Version: 1}, TemplateParameters: map[string]string{"workspace": "https://data.example", "service": "team.data.reader"}, Credential: &mcpconfig.Credential{Value: "synthetic-databricks-token"}}
	body, _ := json.Marshal(in)
	var saved mcpconfig.View
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), &saved); code != 201 {
		t.Fatal(code)
	}
	loaded, err := a.MCP.Get(t.Context(), in.ID)
	if err != nil || loaded.TemplateParameters["service"] != "team.data.reader" || !loaded.CredentialConfigured || len(loaded.Tools) != 0 {
		t.Fatal(loaded, err)
	}
	public, _ := json.Marshal(loaded)
	if strings.Contains(string(public), "synthetic-databricks-token") {
		t.Fatal("secret leaked")
	}
	in.ID = "bad-workspace-profile"
	in.TemplateParameters["service"] = "a.b/../c"
	body, _ = json.Marshal(in)
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), nil); code != 422 {
		t.Fatal(code)
	}
	if _, err := a.MCP.Get(t.Context(), in.ID); err == nil {
		t.Fatal("invalid profile persisted")
	}
}

func TestLocalMCPRegistrationRequiresNativeCapabilityAndAdministrator(t *testing.T) {
	srv, a := setupApp(t, nil)
	body := `{"id":"local","name":"Local fixture","service":"generic","endpoint":"","enabled":false,"localProcess":{"command":"/nonexistent-fixture","args":[],"directory":"/tmp"}}`
	for _, token := range []string{viewerTok, operatorTok} {
		if code := call(t, srv, "POST", "/api/v1/mcp/connections", token, "application/json", body, nil); code != 403 {
			t.Fatal(code)
		}
	}
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", body, nil); code != 422 {
		t.Fatal("server profile accepted host process", code)
	}
	if views, err := a.MCP.List(t.Context()); err != nil || len(views) != 0 {
		t.Fatal("rejected process persisted", views, err)
	}
}
