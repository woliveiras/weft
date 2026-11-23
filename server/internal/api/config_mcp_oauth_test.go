package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/storage"
)

func TestDeclarativeOAuthWaitsForExplicitAuthorization(t *testing.T) {
	srv, a := setupApp(t, nil)
	provider := mcptest.NewOAuth("")
	defer provider.Close()
	upstream := mcptest.New()
	defer upstream.Close()
	a.MCP.OAuthClient = provider.Client()
	var discoveries atomic.Int32
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		discoveries.Add(1)
		return mcpconfig.DiscoverWithClient(ctx, upstream.URL, token, upstream.Client())
	}
	raw := yamlObject("MCPConnection", "oauth-config", "  name: OAuth fixture\n  service: generic\n  endpoint: "+provider.URL+"/mcp\n  enabled: true\n  oauth:\n    issuer: "+provider.URL+"\n    clientId: client\n    scopes: [read]")
	invalid := strings.Replace(raw, "scopes: [read]", "scopes: ['read scope']", 1)
	plan := configRequest(t, srv, "plan", installation.Request{Files: []string{invalid}})
	if plan.OK || plan.Objects[0].Action != "error" || discoveries.Load() != 0 {
		t.Fatalf("invalid OAuth configuration accepted: %+v", plan)
	}
	if _, err := a.MCP.Get(t.Context(), "oauth-config"); err == nil {
		t.Fatal("invalid plan wrote a connection")
	}
	report := configApplyRequest(t, srv, installation.Request{Files: []string{raw}})
	if !report.OK || report.Objects[0].Result != "applied" || !strings.HasPrefix(report.Objects[0].Status, "authorization_required:") || report.Objects[0].Revision == "absent" {
		t.Fatalf("saved OAuth connection reported as failed: %+v", report)
	}
	v, err := a.MCP.Get(t.Context(), "oauth-config")
	if err != nil || !v.Enabled || v.OAuth == nil || v.OAuth.Status != "authorization_required" || v.CredentialConfigured || v.LastTest != nil || discoveries.Load() != 0 {
		t.Fatalf("apply attempted discovery before consent: %+v %v, discoveries %d", v, err, discoveries.Load())
	}
	// A successful apply records the saved baseline; retry is a true no-op.
	retry := configApplyRequest(t, srv, installation.Request{Files: []string{raw}})
	if !retry.OK || retry.Objects[0].Result != "unchanged" || discoveries.Load() != 0 {
		t.Fatalf("pending authorization retry: %+v", retry)
	}
	// The same store can be read by a restarted manager without inventing access.
	restarted := mcpconfig.New(a.MCP.Store, a.MCP.Box, nil)
	persisted, err := restarted.Get(t.Context(), v.ID)
	if err != nil || persisted.Revision != v.Revision || persisted.OAuth.Status != "authorization_required" || persisted.CredentialConfigured {
		t.Fatalf("saved authorization requirement lost: %+v %v", persisted, err)
	}
	// Explicit consent still goes through real TLS metadata, PKCE and token exchange.
	start, err := a.MCP.StartOAuth(t.Context(), v.ID, "admin", "http://localhost:8080/api/v1/mcp/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	authorize, err := url.Parse(start.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := authorize.Query()
	q.Set("decision", "allow")
	authorize.RawQuery = q.Encode()
	client := provider.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	consent, err := client.Get(authorize.String())
	if err != nil {
		t.Fatal(err)
	}
	consent.Body.Close()
	callback, err := url.Parse(consent.Header.Get("Location"))
	if err != nil || consent.StatusCode != http.StatusSeeOther {
		t.Fatalf("consent failed: %d %v", consent.StatusCode, err)
	}
	if err := a.MCP.CaptureOAuth(start.State, start.Binding, callback.Query().Get("code"), callback.Query().Get("iss"), ""); err != nil {
		t.Fatal(err)
	}
	v, err = a.MCP.CompleteOAuth(t.Context(), start.State, start.Binding, "admin")
	if err != nil || v.OAuth.Status != "authorized" {
		t.Fatalf("explicit OAuth completion: %+v %v", v, err)
	}
	before := discoveries.Load()
	updated := strings.Replace(raw, "name: OAuth fixture", "name: Renamed OAuth fixture", 1)
	report = configApplyRequest(t, srv, installation.Request{Files: []string{updated}, Force: []string{"MCPConnection/oauth-config"}})
	if !report.OK || report.Objects[0].Result != "applied" || report.Objects[0].Status != "ready" || discoveries.Load() != before+1 {
		t.Fatalf("authorized connection did not discover: %+v, discoveries %d", report, discoveries.Load())
	}
	// A genuine failure after authenticated discovery must remain a failure.
	store := a.MCP.Store
	a.MCP.Store = &configMCPDiscoveryFailStore{Store: store}
	failed := strings.Replace(updated, "name: Renamed OAuth fixture", "name: Saved before discovery failure", 1)
	report = configApplyRequest(t, srv, installation.Request{Files: []string{failed}, Force: []string{"MCPConnection/oauth-config"}})
	a.MCP.Store = store
	if report.OK || report.Objects[0].Result != "failed" || !strings.Contains(report.Objects[0].Error, "fixture discovery storage unavailable") || discoveries.Load() != before+2 {
		t.Fatalf("authenticated discovery failure masked: %+v", report)
	}
	v, err = a.MCP.Get(t.Context(), v.ID)
	if err != nil || v.Name != "Saved before discovery failure" || v.OAuth.Status != "authorized" {
		t.Fatalf("discovery failure lost saved configuration: %+v %v", v, err)
	}
	// A scope change invalidates access and returns to explicit authorization.
	updated = strings.Replace(updated, "scopes: [read]", "scopes: [read, write]", 1)
	before = discoveries.Load()
	report = configApplyRequest(t, srv, installation.Request{Files: []string{updated}, Force: []string{"MCPConnection/oauth-config"}})
	if !report.OK || !strings.HasPrefix(report.Objects[0].Status, "authorization_required:") || discoveries.Load() != before {
		t.Fatalf("scope change used previous access: %+v", report)
	}
}

type configMCPDiscoveryFailStore struct {
	storage.Store
	writes int
}

func (s *configMCPDiscoveryFailStore) PutSetting(ctx context.Context, key string, value []byte, revision int64, actor string) (int64, error) {
	if key == "mcp/connections/oauth-config" {
		s.writes++
		if s.writes == 2 {
			return 0, fmt.Errorf("fixture discovery storage unavailable")
		}
	}
	return s.Store.PutSetting(ctx, key, value, revision, actor)
}

func TestDeclarativeOAuthPendingPreservesIndependentJiraRESTAndRecoveryHistory(t *testing.T) {
	srv, a := setupApp(t, nil)
	provider := mcptest.NewAtlassianOAuth()
	defer provider.Close()
	a.MCP.OAuthClient = mcptest.OAuthClient(provider)
	in := mcpconfig.Input{ID: "jira-pending", Name: "Jira pending", Service: "jira", Endpoint: "https://mcp.atlassian.com/v2/mcp", Enabled: true,
		OAuth:    &mcpconfig.OAuthConfig{Issuer: mcptest.AtlassianIssuer, RegistrationMode: "dcr", Scopes: []string{"read:me", "offline_access"}},
		JiraREST: &mcpconfig.JiraRESTConfig{CloudID: "11111111-2222-3333-4444-555555555555"}, JiraRESTCredential: &mcpconfig.Credential{Username: "fixture@example.org", Value: "synthetic-independent-rest"}}
	v, err := a.MCP.Put(t.Context(), in, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	provider.FailRegistration.Store(true)
	if _, err := a.MCP.StartOAuthReviewed(t.Context(), v.ID, "admin", "http://localhost:8080/api/v1/mcp/oauth/callback", "dcr"); err == nil {
		t.Fatal("fixture registration failure not exercised")
	}
	v, err = a.MCP.Get(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err = a.MCP.RecoverOAuthRegistration(t.Context(), v.ID, v.Revision, true, "admin")
	if err != nil || len(v.OAuth.RegistrationHistory) != 1 {
		t.Fatalf("recover fixture: %+v %v", v, err)
	}
	raw := yamlObject("MCPConnection", v.ID, "  name: Jira pending\n  service: jira\n  endpoint: https://mcp.atlassian.com/v2/mcp\n  enabled: true\n  jiraRest: {cloudId: 11111111-2222-3333-4444-555555555555}\n  jiraRestSecretRef: {preserve: true}\n  oauth:\n    issuer: "+mcptest.AtlassianIssuer+"\n    registrationMode: dcr\n    scopes: [read:me, read:account, offline_access]")
	report := configApplyRequest(t, srv, installation.Request{Files: []string{raw}, Force: []string{"MCPConnection/jira-pending"}})
	if !report.OK || report.Objects[0].Result != "applied" || !strings.HasPrefix(report.Objects[0].Status, "authorization_required:") {
		t.Fatalf("pending OAuth with REST slot: %+v", report)
	}
	v, err = mcpconfig.New(a.MCP.Store, a.MCP.Box, nil).Get(t.Context(), v.ID)
	if err != nil || v.JiraREST == nil || !v.JiraREST.CredentialConfigured || len(v.OAuth.RegistrationHistory) != 1 || provider.Registrations.Load() != 1 || v.LastTest != nil {
		t.Fatalf("saved state lost or automatic registration/discovery: %+v %v", v, err)
	}
	exported := exportConfig(t, srv)
	if strings.Contains(exported, "synthetic-independent-rest") || strings.Contains(exported, "fixture@example.org") {
		t.Fatal("REST credential disclosed")
	}
}
