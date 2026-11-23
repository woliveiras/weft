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

func TestAtlassianDCRMethodSelection(t *testing.T) {
	for _, tc := range []struct {
		name, service, endpoint, issuer, authorization, token string
		methods                                               []string
		want                                                  string
	}{
		{name: "basic preferred", methods: []string{"none", "client_secret_post", "client_secret_basic"}, want: "client_secret_basic"},
		{name: "post advertised", methods: []string{"none", "client_secret_post"}, want: "client_secret_post"},
		{name: "public only", methods: []string{"none"}, want: "none"},
		{name: "unsupported only", methods: []string{"private_key_jwt"}},
		{name: "empty methods", methods: []string{}},
		{name: "default basic", want: "client_secret_basic"},
		{name: "jira", service: "jira", methods: []string{"none", "client_secret_basic"}, want: "client_secret_basic"},
		{name: "confluence", service: "confluence", methods: []string{"none", "client_secret_basic"}, want: "client_secret_basic"},
		{name: "bitbucket", service: "bitbucket", methods: []string{"none", "client_secret_basic"}, want: "client_secret_basic"},
		{name: "flat", endpoint: atlassianFlatEndpoint, methods: []string{"none", "client_secret_basic"}, want: "client_secret_basic"},
		{name: "generic unchanged", service: "generic", methods: []string{"none", "client_secret_basic"}, want: "none"},
		{name: "other resource", endpoint: "https://other.example/mcp", methods: []string{"none", "client_secret_basic"}, want: "none"},
		{name: "other issuer", issuer: "https://other.example", methods: []string{"none", "client_secret_basic"}, want: "none"},
		{name: "other authorize", authorization: "https://other.example/authorize", methods: []string{"none", "client_secret_basic"}, want: "none"},
		{name: "other token", token: "https://other.example/token", methods: []string{"none", "client_secret_basic"}, want: "none"},
		{name: "gitlab unchanged", service: "gitlab", endpoint: "https://gitlab.example/api/v4/mcp", issuer: "https://gitlab.example", authorization: "https://gitlab.example/oauth/authorize", token: "https://gitlab.example/oauth/token", methods: []string{"client_secret_basic"}, want: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.service == "" {
				tc.service = "atlassian"
			}
			if tc.endpoint == "" {
				tc.endpoint = atlassianMCPEndpoint
			}
			if tc.issuer == "" {
				tc.issuer = mcptest.AtlassianIssuer
			}
			if tc.authorization == "" {
				tc.authorization = "https://auth.atlassian.com/authorize"
			}
			if tc.token == "" {
				tc.token = "https://auth.atlassian.com/oauth/token"
			}
			r := record{View: View{Service: tc.service, Endpoint: tc.endpoint, OAuth: &OAuthView{OAuthConfig: OAuthConfig{Issuer: tc.issuer, RegistrationMode: "dcr"}}}}
			meta := oauthMetadata{Issuer: tc.issuer, Authorization: tc.authorization, Token: tc.token, Registration: "https://auth.atlassian.com/register", Methods: tc.methods}
			method, err := registrationClientMethod(r, meta)
			if tc.want == "" {
				if err == nil {
					t.Fatal("unsupported authentication accepted")
				}
				return
			}
			if err != nil || method != tc.want {
				t.Fatalf("method = %q, error = %v; want %q", method, err, tc.want)
			}
			plan, err := registrationPlan(r, meta, registrationCallback)
			if err != nil || !strings.Contains(plan.Reason, tc.want) {
				t.Fatalf("method absent from setup preview: %+v %v", plan, err)
			}
		})
	}
}

func TestAtlassianConfidentialDCRLifecycleIntegration(t *testing.T) {
	for _, method := range []string{"client_secret_basic", "client_secret_post"} {
		t.Run(method, func(t *testing.T) {
			m := testManager(t)
			provider := mcptest.NewAtlassianOAuth()
			defer provider.Close()
			provider.AtlassianMethods = []string{"none", method}
			m.OAuthClient = mcptest.OAuthClient(provider)
			upstream := mcptest.New()
			defer upstream.Close()
			m.Discover = func(ctx context.Context, endpoint, token string) ([]Tool, error) {
				return DiscoverWithClient(ctx, upstream.URL, token, upstream.Client())
			}
			in := Input{ID: "confidential", Name: "Atlassian confidential", Service: "atlassian", Enabled: true, Endpoint: atlassianFlatEndpoint, OAuth: &OAuthConfig{Issuer: mcptest.AtlassianIssuer, RegistrationMode: "dcr", Scopes: []string{"read:me", "offline_access", "read:jira:agent-interface", "read:confluence:agent-interface"}}}
			if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			plan, err := m.PreviewOAuth(t.Context(), in.ID, registrationCallback)
			if err != nil || !strings.Contains(plan.Reason, method) || provider.Registrations.Load() != 0 {
				t.Fatalf("unsafe preview %+v %v", plan, err)
			}
			start, err := m.StartOAuthReviewed(t.Context(), in.ID, "admin-session", registrationCallback, "dcr")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.load(t.Context(), in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if r.Registration.Method != method || r.OAuthClientSealed == "" {
				t.Fatal("confidential client not persisted")
			}
			saved, err := m.Store.GetSetting(t.Context(), prefix+in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(saved.Value), "fixture-registered-client-secret") {
				t.Fatal("client secret persisted in plaintext")
			}
			// The real TLS fixture must reject missing or wrong client authentication
			// before any exchange/refresh can occur. It does not accept confidential
			// registration just because the client sends a syntactically valid form.
			for _, secret := range []string{"", "wrong-fixture-secret"} {
				form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"fixture-refresh-token"}, "client_id": {r.Registration.ClientID}, "resource": {atlassianMCPEndpoint}}
				basicSecret := secret
				if method == "client_secret_post" {
					form.Set("client_secret", secret)
					basicSecret = ""
				}
				if err := oauthJSON(t.Context(), m.OAuthClient, "https://auth.atlassian.com/oauth/token", form, basicSecret, nil); err == nil {
					t.Fatal("invalid client authentication accepted")
				}
			}
			if provider.Refreshes.Load() != 0 {
				t.Fatal("unauthenticated client refreshed")
			}
			u, err := url.Parse(start.URL)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			q.Set("decision", "allow")
			u.RawQuery = q.Encode()
			client := *m.OAuthClient
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			response, err := client.Get(u.String())
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			callback, err := url.Parse(response.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			cq := callback.Query()
			// The durable registration and pending browser transaction survive a manager restart.
			restarted := New(m.Store, m.Box, nil)
			restarted.OAuthClient = m.OAuthClient
			restarted.Discover = m.Discover
			if err = restarted.CaptureOAuth(start.State, start.Binding, cq.Get("code"), cq.Get("iss"), ""); err != nil {
				t.Fatal(err)
			}
			view, err := restarted.CompleteOAuth(t.Context(), start.State, start.Binding, "admin-session")
			if err != nil || view.Status != "ready" || view.OAuth.Status != "authorized" || !view.OAuth.ClientSecretConfigured {
				t.Fatalf("confidential OAuth failed: %s %v", view.Status, err)
			}
			if provider.Refreshes.Load() != 1 || provider.Registrations.Load() != 1 {
				t.Fatal("lifecycle did not authenticate exactly one registered client")
			}
			data, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "fixture-registered-client-secret") || strings.Contains(string(data), "fixture-refresh-token") || strings.Contains(string(data), "fixture-mcp-token") {
				t.Fatal("credential leaked in view")
			}
			revoked, err := restarted.RevokeOAuth(t.Context(), in.ID, "admin")
			if err != nil || revoked.OAuth.Revocation != "requested" || provider.Revocations.Load() != 2 {
				t.Fatal("access and refresh revocation did not authenticate", err)
			}
		})
	}
}
