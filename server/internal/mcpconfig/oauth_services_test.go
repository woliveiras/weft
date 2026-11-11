package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestGitLabPublicClientBoundaries(t *testing.T) {
	for _, variant := range []string{"valid", "custom-instance", "generic", "other-issuer", "other-token", "other-authorize", "empty-methods", "wrong-path", "missing-mcp", "private-scheme"} {
		t.Run(variant, func(t *testing.T) {
			origin := "https://gitlab.com"
			if variant == "custom-instance" {
				origin = "https://gitlab.example.org"
			}
			r := record{View: View{Service: "gitlab", Endpoint: origin + "/api/v4/mcp", OAuth: &OAuthView{OAuthConfig: OAuthConfig{Issuer: origin, ClientID: "public", Scopes: []string{"mcp"}}}}}
			meta := oauthMetadata{Issuer: origin, Authorization: origin + "/oauth/authorize", Token: origin + "/oauth/token", Methods: []string{"client_secret_basic", "client_secret_post"}}
			switch variant {
			case "generic":
				r.Service = "generic"
			case "other-issuer":
				meta.Issuer = "https://other.example"
			case "other-token":
				meta.Token = "https://other.example/oauth/token"
			case "other-authorize":
				meta.Authorization = "https://other.example/oauth/authorize"
			case "empty-methods":
				meta.Methods = []string{}
			case "wrong-path":
				r.Endpoint = origin + "/api/v4/projects"
			case "missing-mcp":
				r.OAuth.Scopes = []string{"api"}
			case "private-scheme":
				r.OAuth.Issuer = "http://gitlab.com"
			}
			method, err := connectionClientMethod(r, meta)
			valid := variant == "valid" || variant == "custom-instance"
			if valid && (err != nil || method != "none") || !valid && err == nil {
				t.Fatalf("%s %v", method, err)
			}
		})
	}
}

func TestOAuthMultipleServiceConnectionsIntegration(t *testing.T) {
	t.Run("manual", func(t *testing.T) { testOAuthMultipleServiceConnections(t, false) })
	t.Run("dcr", func(t *testing.T) { testOAuthMultipleServiceConnections(t, true) })
}
func testOAuthMultipleServiceConnections(t *testing.T, automatic bool) {
	m := testManager(t)
	github := mcptest.NewGitHubOAuth()
	defer github.Close()
	atlassian := mcptest.NewAtlassianOAuth()
	defer atlassian.Close()
	gitlab := mcptest.NewGitLabOAuth("https://gitlab.example.org")
	defer gitlab.Close()
	m.OAuthClient = mcptest.OAuthClient(github, atlassian, gitlab)
	upstream := mcptest.New()
	defer upstream.Close()
	m.Discover = func(ctx context.Context, endpoint, token string) ([]Tool, error) {
		return DiscoverWithClient(ctx, upstream.URL, token, upstream.Client())
	}
	m.Call = func(ctx context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
		return CallWithClient(ctx, upstream.URL, token, name, args, upstream.Client())
	}
	inputs := []Input{
		{ID: "personal", Name: "Personal GitHub", Service: "github", Enabled: true, Endpoint: "https://api.githubcopilot.com/mcp/", OAuth: &OAuthConfig{Issuer: githubOAuthIssuer, ClientID: "personal", Scopes: []string{"read:user", "read:org"}}, OAuthClientSecret: &Credential{Value: "fixture-client-secret"}},
		{ID: "work", Name: "Work Atlassian", Service: "atlassian", Enabled: true, Endpoint: "https://mcp.atlassian.com/v2/mcp", OAuth: &OAuthConfig{Issuer: mcptest.AtlassianIssuer, ClientID: "work", Scopes: []string{"read:me", "offline_access", "read:jira:agent-interface", "read:confluence:agent-interface"}}},
		{ID: "gitlab", Name: "GitLab instance", Service: "gitlab", Enabled: true, Endpoint: "https://gitlab.example.org/api/v4/mcp", OAuth: &OAuthConfig{Issuer: "https://gitlab.example.org", ClientID: "gitlab", Scopes: []string{"mcp"}}},
	}
	second := inputs[1]
	second.Endpoint = atlassianFlatEndpoint
	second.ID = "work-two"
	second.Name = "Another work connection"
	inputs = append(inputs, second)
	starts := map[string]OAuthStart{}
	for _, in := range inputs {
		mode := ""
		if automatic && in.Service != "github" {
			in.OAuth.ClientID = ""
			in.OAuth.RegistrationMode = "dcr"
			mode = "dcr"
		}
		if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
			t.Fatal(err)
		}
		start, err := m.StartOAuthReviewed(t.Context(), in.ID, "admin-session", "http://localhost:8080/api/v1/mcp/oauth/callback", mode)
		if err != nil {
			t.Fatal(in.ID, err)
		}
		starts[in.ID] = start
	}
	// Complete in reverse order, with several pending browser transactions.
	client := *m.OAuthClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for i := len(inputs) - 1; i >= 0; i-- {
		in := inputs[i]
		start := starts[in.ID]
		u, _ := url.Parse(start.URL)
		q := u.Query()
		q.Set("decision", "allow")
		u.RawQuery = q.Encode()
		res, err := client.Get(u.String())
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		callback, err := url.Parse(res.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		cq := callback.Query()
		if err = m.CaptureOAuth(start.State, "another-binding", cq.Get("code"), cq.Get("iss"), ""); err == nil {
			t.Fatal("cross-session callback accepted")
		}
		if err = m.CaptureOAuth(start.State, start.Binding, cq.Get("code"), cq.Get("iss"), ""); err != nil {
			t.Fatal(err)
		}
		v, err := m.CompleteOAuth(t.Context(), start.State, start.Binding, "admin-session")
		if err != nil || v.Status != "ready" {
			t.Fatal(in.ID, v, err)
		}
		raw, _ := json.Marshal(v)
		for _, secret := range []string{"fixture-client-secret", "fixture-refresh-token", "fixture-mcp-token"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("secret in API view")
			}
		}
	}
	before, err := m.Get(t.Context(), "personal")
	if err != nil {
		t.Fatal(err)
	}
	beforeOther, err := m.Get(t.Context(), "work-two")
	if err != nil {
		t.Fatal(err)
	}
	p := &tools.Principal{GraphID: "g", NodeID: "worker", ExecutionID: "exec"}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = m.Resolve
	if _, err = gateway.Invoke(t.Context(), p, "mcp.work.search", nil); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("consent granted tool", err)
	}
	p.Grants = []domain.ToolGrant{{Name: "mcp.work.search"}, {Name: "mcp.personal.search"}, {Name: "mcp.work-two.search"}}
	if _, err = gateway.Invoke(t.Context(), p, "mcp.work.search", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = m.RevokeOAuth(t.Context(), "work", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = gateway.Invoke(t.Context(), p, "mcp.work.search", nil); err == nil {
		t.Fatal("revoked connection invoked")
	}
	for _, id := range []string{"personal", "work-two"} {
		if _, err = gateway.Invoke(t.Context(), p, "mcp."+id+".search", json.RawMessage(`{}`)); err != nil {
			t.Fatal(id, err)
		}
	}
	after, _ := m.Get(t.Context(), "personal")
	afterOther, _ := m.Get(t.Context(), "work-two")
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeOther, afterOther) {
		t.Fatal("another connection changed on revocation")
	}
	r, _ := m.load(t.Context(), "personal")
	if _, err = m.Box.Open(r.Sealed, prefix+"work"); err == nil {
		t.Fatal("credential decrypts for another connection")
	}
	if github.Refreshes.Load() != 1 || gitlab.Refreshes.Load() != 1 || atlassian.Refreshes.Load() != 2 {
		t.Fatal("refresh isolation")
	}
	if automatic {
		if atlassian.Registrations.Load() != 2 || gitlab.Registrations.Load() != 1 {
			t.Fatal("registration isolation")
		}
		work, _ := m.load(t.Context(), "work")
		other, _ := m.load(t.Context(), "work-two")
		if work.Registration.ClientID == other.Registration.ClientID {
			t.Fatal("shared registration")
		}
	}
	// Re-authorization of only the revoked connection must not alter other records.
	mode := ""
	if automatic {
		mode = "dcr"
	}
	if _, err = m.StartOAuthReviewed(t.Context(), "work", "admin-session", "http://localhost:8080/api/v1/mcp/oauth/callback", mode); err != nil {
		t.Fatal(err)
	}
}

func TestServiceChangeInvalidatesOnlyItsOAuth(t *testing.T) {
	m, _ := oauthSetup(t)
	v := authorizeFixture(t, m)
	in := input()
	in.Endpoint = v.Endpoint
	in.Service = "atlassian"
	in.OAuth = &v.OAuth.OAuthConfig
	changed, err := m.Put(t.Context(), in, v.Revision, "admin")
	if err != nil || changed.OAuth.Status != "authorization_required" || changed.CredentialConfigured || len(changed.Tools) != 0 {
		t.Fatal(changed, err)
	}
}

func TestOAuthProviderErrorsAreUsefulAndRedacted(t *testing.T) {
	for _, tc := range []struct{ code, hint string }{{"invalid_client", "registration"}, {"invalid_grant", "callback"}, {"invalid_scope", "MCP scopes"}, {"access_denied", "corporate"}, {"synthetic-secret", "access policy"}} {
		raw, _ := json.Marshal(map[string]string{"error": tc.code, "error_description": "synthetic-secret", "error_uri": "https://example.com/?token=synthetic-secret"})
		err := oauthProviderError(raw)
		if err == nil || !strings.Contains(err.Error(), tc.hint) || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal(err)
		}
	}
	if err := oauthProviderError([]byte(`{"access_token":"synthetic-secret"}`)); err != nil {
		t.Fatal(err)
	}
}
