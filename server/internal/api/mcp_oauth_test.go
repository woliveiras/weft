package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
)

func TestMCPOAuthBrowserAPI(t *testing.T) {
	for _, github := range []bool{false, true} {
		name := "standard"
		if github {
			name = "github-contract"
		}
		t.Run(name, func(t *testing.T) { testMCPOAuthBrowserAPI(t, github, "") })
	}
}
func TestMCPAutomaticOAuthBrowserAPI(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd"} {
		t.Run(mode, func(t *testing.T) { testMCPOAuthBrowserAPI(t, false, mode) })
	}
}
func testMCPOAuthBrowserAPI(t *testing.T, github bool, mode string) {
	base := "http://localhost:8080"
	if mode == "cimd" {
		base = "https://weft.fixture.example"
	}
	srv, a := setupApp(t, func(c *config.Config) { c.Server.PublicURL = base })
	var callbackConfig map[string]string
	if code := call(t, srv, "GET", "/api/v1/mcp/oauth/config", viewerTok, "", "", &callbackConfig); code != 200 || callbackConfig["callbackUrl"] != base+"/api/v1/mcp/oauth/callback" {
		t.Fatal(code, callbackConfig)
	}
	var provider *mcptest.OAuthServer
	if github {
		provider = mcptest.NewGitHubOAuth()
	} else {
		provider = mcptest.NewOAuth("")
	}
	defer provider.Close()
	upstream := mcptest.New()
	defer upstream.Close()
	a.MCP.OAuthClient = provider.Client()
	if github {
		a.MCP.OAuthClient = provider.GitHubClient()
	}
	a.MCP.Discover = func(ctx context.Context, endpoint, token string) ([]mcpconfig.Tool, error) {
		return mcpconfig.DiscoverWithClient(ctx, upstream.URL, token, upstream.Client())
	}
	in := mcpconfig.Input{ID: "oauth", Name: "OAuth", Service: "generic", Endpoint: provider.URL + "/mcp", Enabled: true, OAuth: &mcpconfig.OAuthConfig{Issuer: provider.URL, ClientID: "client", Scopes: []string{"read"}}}
	if github {
		in.Service = "github"
		in.Endpoint = "https://api.githubcopilot.com/mcp/"
		in.OAuth = &mcpconfig.OAuthConfig{Issuer: "https://github.com/login/oauth", ClientID: "client", Scopes: []string{"read:user", "read:org"}}
		in.OAuthClientSecret = &mcpconfig.Credential{Value: "fixture-client-secret"}
	}
	if mode != "" {
		in.OAuth.RegistrationMode = mode
		in.OAuth.ClientID = ""
	}
	provider.ClientDocument = func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil {
			return false
		}
		resp, err := http.Get(srv.URL + u.Path)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var doc map[string]any
		return resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&doc) == nil && doc["client_id"] == raw && doc["client_name"] == "Weft"
	}
	body, _ := json.Marshal(in)
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(body), nil); code != 201 {
		t.Fatal(code)
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var bindingCookies []*http.Cookie
	request := func(method, path, token, body string) *http.Response {
		t.Helper()
		r, e := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		if mode == "cimd" {
			for _, c := range bindingCookies {
				r.AddCookie(c)
			}
		}
		r.Header.Set("Content-Type", "application/json")
		res, e := browser.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	res := request("POST", "/api/v1/mcp/connections/oauth/oauth/start", viewerTok, "")
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	if mode != "" {
		var preview mcpconfig.RegistrationPlan
		if code := call(t, srv, "GET", "/api/v1/mcp/connections/oauth/oauth/preview", adminTok, "", "", &preview); code != 200 || preview.Mode != mode {
			t.Fatal(code, preview)
		}
		if provider.Registrations.Load() != 0 {
			t.Fatal("preview registered a client")
		}
	}
	res = request("POST", "/api/v1/mcp/connections/oauth/oauth/start?registrationMode="+mode, adminTok, "")
	bindingCookies = res.Cookies()
	var start mcpconfig.OAuthStart
	if err := json.NewDecoder(res.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	// The configured callback is independent of Host and contains no bearer credential.
	authorize, _ := url.Parse(start.URL)
	q := authorize.Query()
	if strings.Contains(start.URL, adminTok) || q.Get("redirect_uri") != base+"/api/v1/mcp/oauth/callback" {
		t.Fatal("wrong callback")
	}
	q.Set("decision", "allow")
	authorize.RawQuery = q.Encode()
	providerClient := provider.Client()
	if github {
		providerClient = provider.GitHubClient()
	}
	providerClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	consent, e := providerClient.Get(authorize.String())
	if e != nil {
		t.Fatal(e)
	}
	consent.Body.Close()
	callback, _ := url.Parse(consent.Header.Get("Location"))
	res = request("GET", callback.RequestURI(), "", "")
	res.Body.Close()
	if res.StatusCode != 303 || strings.Contains(res.Header.Get("Location"), "code=") {
		t.Fatal("callback leaked code", res.StatusCode)
	}
	res = request("POST", "/api/v1/mcp/oauth/complete", operatorTok, `{"state":"`+start.State+`"}`)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	res = request("POST", "/api/v1/mcp/oauth/complete", adminTok, `{"state":"`+start.State+`"}`)
	raw, e := io.ReadAll(res.Body)
	res.Body.Close()
	if e != nil || res.StatusCode != 200 {
		t.Fatal(res.StatusCode, string(raw), e)
	}
	var connection mcpconfig.View
	if e = json.Unmarshal(raw, &connection); e != nil {
		t.Fatal(e)
	}
	if connection.Status != "ready" || connection.OAuth.Status != "authorized" || provider.Refreshes.Load() != 1 {
		t.Fatal(connection, provider.Refreshes.Load())
	}
	if strings.Contains(string(raw), "fixture-mcp-token") || strings.Contains(string(raw), "fixture-refresh-token") || strings.Contains(string(raw), "fixture-client-secret") {
		t.Fatal("secret disclosure")
	}
	res = request("POST", "/api/v1/mcp/oauth/complete", adminTok, `{"state":"`+start.State+`"}`)
	res.Body.Close()
	if res.StatusCode != 422 {
		t.Fatal("callback replay", res.StatusCode)
	}
	res = request("POST", "/api/v1/mcp/connections/oauth/oauth/revoke", adminTok, "")
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	if e = json.Unmarshal(raw, &connection); e != nil {
		t.Fatal(e)
	}
	wantRevocations := int32(2)
	if github {
		wantRevocations = 0
		if connection.OAuth.Revocation != "unsupported" {
			t.Fatal(connection)
		}
	}
	if mode == "dcr" && provider.Registrations.Load() != 1 {
		t.Fatal("registration count", provider.Registrations.Load())
	}
	if connection.OAuth.Status != "revoked" || connection.CredentialConfigured || provider.Revocations.Load() != wantRevocations {
		t.Fatal(connection)
	}
}

func TestMCPOAuthServiceConfigurationAPI(t *testing.T) {
	srv, _ := setupApp(t, nil)
	for _, in := range []mcpconfig.Input{
		{ID: "personal", Name: "Personal GitHub", Service: "github", Enabled: true, Endpoint: "https://api.githubcopilot.com/mcp/", OAuth: &mcpconfig.OAuthConfig{Issuer: "https://github.com/login/oauth", ClientID: "registered", Scopes: []string{"read:user"}}},
		{ID: "work", Name: "Work Atlassian", Service: "atlassian", Enabled: true, Endpoint: "https://mcp.atlassian.com/v2/mcp", OAuth: &mcpconfig.OAuthConfig{Issuer: mcptest.AtlassianIssuer, ClientID: "registered", Scopes: []string{"read:me"}}},
		{ID: "gitlab", Name: "GitLab", Service: "gitlab", Enabled: true, Endpoint: "https://gitlab.example.org/api/v4/mcp", OAuth: &mcpconfig.OAuthConfig{Issuer: "https://gitlab.example.org", ClientID: "registered", Scopes: []string{"mcp"}}},
	} {
		raw, _ := json.Marshal(in)
		if code := call(t, srv, "POST", "/api/v1/mcp/connections", viewerTok, "application/json", string(raw), nil); code != 403 {
			t.Fatal(code)
		}
		var saved mcpconfig.View
		if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(raw), &saved); code != 201 || saved.OAuth.Status != "authorization_required" || len(saved.Tools) != 0 {
			t.Fatal(code, saved)
		}
	}
	var list []mcpconfig.View
	if code := call(t, srv, "GET", "/api/v1/mcp/connections", viewerTok, "", "", &list); code != 200 || len(list) != 3 {
		t.Fatal(code, list)
	}
	for _, body := range []string{
		`{"id":"bad","name":"Bad","service":"gitlab","endpoint":"https://gitlab.example.org/api/v4/mcp","oauth":{"issuer":"https://other.example","clientId":"registered","scopes":["mcp"]}}`,
		`{"id":"bad","name":"Bad","service":"gitlab","endpoint":"https://gitlab.example.org/api/v4/mcp","oauth":{"issuer":"https://gitlab.example.org","clientId":"registered","scopes":["api"]}}`,
	} {
		if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", body, nil); code != 422 {
			t.Fatal(code)
		}
	}
}
