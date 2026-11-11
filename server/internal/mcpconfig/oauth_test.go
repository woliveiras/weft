package mcpconfig

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

type oauthFixture struct {
	wantClientSecret string
	server           *httptest.Server
	refreshes        atomic.Int32
	revocations      atomic.Int32
	failRefresh      atomic.Bool
	failRevoke       atomic.Bool
	failDiscovery    atomic.Bool
	waitRefresh      chan struct{}
	refreshStarted   chan struct{}
}

func oauthSetup(t *testing.T) (*Manager, *oauthFixture) {
	t.Helper()
	m := testManager(t)
	f := &oauthFixture{}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := f.server.URL
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/resource"`)
			w.WriteHeader(401)
		case "/resource":
			json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "revocation_endpoint": base + "/revoke", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"}})
		case "/token":
			if f.wantClientSecret != "" {
				client, secret, ok := r.BasicAuth()
				if !ok || client != "client" || secret != f.wantClientSecret {
					http.Error(w, "bad client authentication", 401)
					return
				}
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("resource") != base+"/mcp" || r.Form.Get("client_id") != "client" {
				t.Error("resource/client binding missing")
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				f.refreshes.Add(1)
				if f.refreshStarted != nil {
					f.refreshStarted <- struct{}{}
					<-f.waitRefresh
				}
				if f.failRefresh.Load() {
					http.Error(w, "synthetic-refresh-secret", 400)
					return
				}
			} else if r.Form.Get("code_verifier") == "" || r.Form.Get("code") != "test-code" {
				http.Error(w, "bad proof", 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-access", "refresh_token": "synthetic-rotated-refresh", "expires_in": 3600, "token_type": "Bearer", "scope": "read"})
		case "/revoke":
			f.revocations.Add(1)
			if f.failRevoke.Load() {
				http.Error(w, "synthetic-secret", 503)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.server.Close)
	m.OAuthClient = f.server.Client()
	in := input()
	in.Endpoint = f.server.URL + "/mcp"
	in.OAuth = &OAuthConfig{Issuer: f.server.URL, ClientID: "client", Scopes: []string{"read"}}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	m.Discover = func(ctx context.Context, endpoint, token string) ([]Tool, error) {
		if f.failDiscovery.Load() {
			return nil, errors.New("offline")
		}
		if token != "synthetic-access" {
			t.Error("access token not used")
		}
		return []Tool{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
	}
	return m, f
}
func authorizeFixture(t *testing.T, m *Manager) View {
	t.Helper()
	start, err := m.StartOAuth(t.Context(), "team", "admin-session", "http://localhost:8080/api/v1/mcp/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(start.URL)
	q := u.Query()
	if q.Get("resource") == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatal("missing resource/PKCE")
	}
	entries, _, err := m.loadPending(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := entries[start.State]
	hash := sha256.Sum256([]byte(p.Verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
		t.Fatal("bad challenge")
	}
	if err = m.CaptureOAuth(start.State, start.Binding, "test-code", p.Metadata.Issuer, ""); err != nil {
		t.Fatal(err)
	}
	v, err := m.CompleteOAuth(t.Context(), start.State, start.Binding, "admin-session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CompleteOAuth(t.Context(), start.State, start.Binding, "admin-session"); err == nil {
		t.Fatal("replay accepted")
	}
	return v
}
func expireFixture(t *testing.T, m *Manager) record {
	t.Helper()
	r, err := m.load(t.Context(), "team")
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Minute)
	r.OAuth.ExpiresAt = &expired
	v, err := m.save(t.Context(), r, r.Revision, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Revision = v.Revision
	return r
}
func TestOAuthLifecycleAndRedaction(t *testing.T) {
	m, f := oauthSetup(t)
	v := authorizeFixture(t, m)
	if v.Status != "ready" || v.OAuth.Status != "authorized" || !v.CredentialConfigured {
		t.Fatal(v)
	}
	saved, err := m.Store.GetSetting(t.Context(), prefix+"team")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"synthetic-access", "synthetic-rotated-refresh", "test-code"} {
		if strings.Contains(string(saved.Value), secret) {
			t.Fatal("plaintext credential persisted")
		}
	}
	r := expireFixture(t, m)
	_, token, err := m.accessToken(t.Context(), r)
	if err != nil || token != "synthetic-access" || f.refreshes.Load() != 1 {
		t.Fatal(token, err)
	}
	// A new manager can use persisted encrypted OAuth credentials and metadata.
	restarted := New(m.Store, m.Box, nil)
	restarted.OAuthClient = f.server.Client()
	r = expireFixture(t, restarted)
	if _, _, err = restarted.accessToken(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	revoked, err := m.RevokeOAuth(t.Context(), "team", "admin")
	if err != nil || revoked.OAuth.Status != "revoked" || revoked.CredentialConfigured || revoked.OAuth.Revocation != "requested" {
		t.Fatal(revoked, err)
	}
	r, _ = m.load(t.Context(), "team")
	if r.Sealed != "" || r.OAuthRefreshSealed != "" {
		t.Fatal("tokens retained")
	}
	if _, err = m.Resolve(t.Context(), "mcp.team.read"); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("revocation did not deny tools", err)
	}
}
func TestOAuthInvalidReturnAndRevokedPending(t *testing.T) {
	m, _ := oauthSetup(t)
	start, err := m.StartOAuth(t.Context(), "team", "session", "http://localhost/callback")
	if err != nil {
		t.Fatal(err)
	}
	if m.CaptureOAuth(start.State, "wrong", "test-code", "", "") == nil {
		t.Fatal("wrong browser accepted")
	}
	if m.CaptureOAuth(start.State, start.Binding, "test-code", "https://wrong.example", "") == nil {
		t.Fatal("issuer mismatch accepted")
	}
	if err = m.CaptureOAuth(start.State, start.Binding, "test-code", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CompleteOAuth(t.Context(), start.State, start.Binding, "other-session"); err == nil {
		t.Fatal("wrong user accepted")
	}
	if _, err = m.RevokeOAuth(t.Context(), "team", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CompleteOAuth(t.Context(), start.State, start.Binding, "session"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("revoked flow restored", err)
	}
}
func TestOAuthRefreshSerializedAndRevocationRace(t *testing.T) {
	m, f := oauthSetup(t)
	authorizeFixture(t, m)
	r := expireFixture(t, m)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := m.accessToken(context.Background(), r); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.refreshes.Load() != 1 {
		t.Fatal("duplicate refresh", f.refreshes.Load())
	}
	r = expireFixture(t, m)
	f.refreshStarted = make(chan struct{}, 1)
	f.waitRefresh = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, _, err := m.accessToken(context.Background(), r); done <- err }()
	<-f.refreshStarted
	if _, err := m.RevokeOAuth(t.Context(), "team", "admin"); err != nil {
		t.Fatal(err)
	}
	close(f.waitRefresh)
	if err := <-done; !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	v, err := m.Get(t.Context(), "team")
	if err != nil || v.OAuth.Status != "revoked" || v.CredentialConfigured {
		t.Fatal("refresh resurrected authorization", v, err)
	}
}
func TestOAuthFailures(t *testing.T) {
	t.Run("refresh", func(t *testing.T) {
		m, f := oauthSetup(t)
		authorizeFixture(t, m)
		r := expireFixture(t, m)
		f.failRefresh.Store(true)
		if _, _, err := m.accessToken(t.Context(), r); err == nil || strings.Contains(err.Error(), "synthetic") {
			t.Fatal(err)
		}
		v, _ := m.Get(t.Context(), "team")
		if v.OAuth.Status != "reauthorization_required" {
			t.Fatal(v)
		}
	})
	t.Run("revocation", func(t *testing.T) {
		m, f := oauthSetup(t)
		authorizeFixture(t, m)
		f.failRevoke.Store(true)
		v, err := m.RevokeOAuth(t.Context(), "team", "admin")
		if err != nil || v.OAuth.Revocation != "failed" || v.CredentialConfigured {
			t.Fatal(v, err)
		}
	})
	t.Run("discovery", func(t *testing.T) {
		m, f := oauthSetup(t)
		f.failDiscovery.Store(true)
		v := authorizeFixture(t, m)
		if v.Status != "unavailable" || v.OAuth.Status != "authorized" {
			t.Fatal(v)
		}
	})
	t.Run("denied", func(t *testing.T) {
		m, _ := oauthSetup(t)
		s, e := m.StartOAuth(t.Context(), "team", "actor", "http://localhost/callback")
		if e != nil {
			t.Fatal(e)
		}
		if e = m.CaptureOAuth(s.State, s.Binding, "", "", "access_denied"); e != nil {
			t.Fatal(e)
		}
		if _, e = m.CompleteOAuth(t.Context(), s.State, s.Binding, "actor"); e == nil {
			t.Fatal("denial ignored")
		}
	})
	t.Run("expired-state", func(t *testing.T) {
		m, _ := oauthSetup(t)
		s, e := m.StartOAuth(t.Context(), "team", "actor", "http://localhost/callback")
		if e != nil {
			t.Fatal(e)
		}
		if e := m.updatePending(t.Context(), func(entries map[string]oauthPending) error {
			p := entries[s.State]
			p.Expires = time.Now().Add(-time.Minute)
			entries[s.State] = p
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		if m.CaptureOAuth(s.State, s.Binding, "test-code", "", "") == nil {
			t.Fatal("expired state accepted")
		}
	})
}
func TestOAuthConfigValidation(t *testing.T) {
	for _, c := range []*OAuthConfig{{Issuer: "http://issuer.example", ClientID: "a"}, {Issuer: "https://issuer.example?token=x", ClientID: "a"}, {Issuer: "https://issuer.example", ClientID: ""}, {Issuer: "https://issuer.example", ClientID: "a", Scopes: []string{"read", "read"}}, {Issuer: "https://issuer.example", ClientID: "a", Scopes: []string{"bad scope"}}} {
		if validateOAuth(c) == nil {
			t.Fatal("invalid config accepted")
		}
	}
}

func TestOAuthExternalRevocationAndConfigurationChange(t *testing.T) {
	m, f := oauthSetup(t)
	authorizeFixture(t, m)
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return nil, discoveryError("MCP HTTP status 401")
	}
	v, err := m.Refresh(t.Context(), "team", "admin")
	if err != nil || v.OAuth.Status != "reauthorization_required" || v.CredentialConfigured || len(v.Tools) != 0 {
		t.Fatal(v, err)
	}
	in := input()
	in.Endpoint = f.server.URL + "/mcp"
	in.OAuth = &OAuthConfig{Issuer: f.server.URL, ClientID: "client", Scopes: []string{"read"}}
	in.OAuthClientSecret = &Credential{Value: "synthetic-client-secret"}
	v, err = m.Put(t.Context(), in, v.Revision, "admin")
	if err != nil || !v.OAuth.ClientSecretConfigured {
		t.Fatal(v, err)
	}
	r, err := m.Store.GetSetting(t.Context(), prefix+"team")
	if err != nil || strings.Contains(string(r.Value), "synthetic-client-secret") {
		t.Fatal("client credential not encrypted", err)
	}
	in.OAuth.ClientID = "other-client"
	in.OAuthClientSecret = nil
	v, err = m.Put(t.Context(), in, v.Revision, "admin")
	if err != nil || v.OAuth.ClientSecretConfigured {
		t.Fatal("client secret forwarded to different client", v, err)
	}
}
func TestOAuthNoRefreshAndInterruptedRefresh(t *testing.T) {
	for _, status := range []string{"authorized", "refreshing"} {
		t.Run(status, func(t *testing.T) {
			m, _ := oauthSetup(t)
			authorizeFixture(t, m)
			r := expireFixture(t, m)
			r.OAuth.Status = status
			r.OAuthRefreshSealed = ""
			r.UpdatedAt = time.Now().Add(-time.Minute)
			if _, _, err := m.accessToken(t.Context(), r); err == nil {
				t.Fatal("expired/interrupted token accepted")
			}
			v, err := m.Get(t.Context(), "team")
			if err != nil || v.OAuth.Status != "reauthorization_required" {
				t.Fatal(v, err)
			}
		})
	}
}

type oauthRoundTrip func(*http.Request) (*http.Response, error)

func (f oauthRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOAuthMetadataRejectsUntrustedContracts(t *testing.T) {
	for _, variant := range []string{"issuer", "pkce", "resource", "method", "http-token", "oversized", "bad-json"} {
		t.Run(variant, func(t *testing.T) {
			m := testManager(t)
			in := input()
			in.OAuth = &OAuthConfig{Issuer: "https://issuer.example", ClientID: "client", Scopes: []string{"read"}}
			if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			m.OAuthClient = &http.Client{Transport: oauthRoundTrip(func(req *http.Request) (*http.Response, error) {
				var result any
				switch req.URL.Path {
				case "/mcp":
					result = map[string]any{}
				case "/.well-known/oauth-protected-resource/mcp":
					resource := in.Endpoint
					if variant == "resource" {
						resource = "https://other.example/mcp"
					}
					result = map[string]any{"resource": resource, "authorization_servers": []string{in.OAuth.Issuer}}
				default:
					issuer := in.OAuth.Issuer
					challenge := "S256"
					method := "none"
					token := "https://issuer.example/token"
					switch variant {
					case "issuer":
						issuer = "https://other.example"
					case "pkce":
						challenge = "plain"
					case "method":
						method = "client_secret_post"
					case "http-token":
						token = "http://issuer.example/token"
					}
					result = map[string]any{"issuer": issuer, "authorization_endpoint": "https://issuer.example/authorize", "token_endpoint": token, "code_challenge_methods_supported": []string{challenge}, "token_endpoint_auth_methods_supported": []string{method}}
				}
				b, _ := json.Marshal(result)
				if variant == "oversized" {
					b = []byte(strings.Repeat("x", 65537))
				}
				if variant == "bad-json" {
					b = []byte("invalid")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}
			if _, err := m.StartOAuth(t.Context(), "team", "session", "http://localhost/callback"); err == nil {
				t.Fatal("invalid OAuth contract accepted")
			}
		})
	}
}

func TestOAuthConfidentialClient(t *testing.T) {
	m, f := oauthSetup(t)
	f.wantClientSecret = "synthetic-client-secret"
	v, err := m.Get(t.Context(), "team")
	if err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Endpoint = f.server.URL + "/mcp"
	in.OAuth = &OAuthConfig{Issuer: f.server.URL, ClientID: "client", Scopes: []string{"read"}}
	in.OAuthClientSecret = &Credential{Value: f.wantClientSecret}
	if _, err = m.Put(t.Context(), in, v.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	v = authorizeFixture(t, m)
	if !v.OAuth.ClientSecretConfigured {
		t.Fatal("client secret missing")
	}
	r := expireFixture(t, m)
	if _, _, err = m.accessToken(t.Context(), r); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthClientAuthenticationContracts(t *testing.T) {
	github := oauthMetadata{Issuer: githubOAuthIssuer, Authorization: githubOAuthIssuer + "/authorize", Token: githubOAuthIssuer + "/access_token"}
	for _, tc := range []struct {
		name   string
		meta   oauthMetadata
		secret bool
		want   string
	}{
		{"RFC omitted confidential", oauthMetadata{}, true, "client_secret_basic"},
		{"RFC omitted public denied", oauthMetadata{}, false, ""},
		{"empty methods denied", oauthMetadata{Methods: []string{}}, true, ""},
		{"public explicit", oauthMetadata{Methods: []string{"none"}}, false, "none"},
		{"post explicit", oauthMetadata{Methods: []string{"client_secret_post"}}, true, "client_secret_post"},
		{"GitHub omitted", github, true, "client_secret_post"},
		{"GitHub missing secret", github, false, ""},
		{"GitHub wrong token host", oauthMetadata{Issuer: githubOAuthIssuer, Authorization: github.Authorization, Token: "https://other.example/token"}, true, ""},
		{"GitHub wrong authorization", oauthMetadata{Issuer: githubOAuthIssuer, Authorization: "https://other.example/authorize", Token: github.Token}, true, ""},
		{"lookalike issuer", oauthMetadata{Issuer: githubOAuthIssuer + ".evil"}, true, "client_secret_basic"},
		{"GitHub empty methods", oauthMetadata{Issuer: github.Issuer, Authorization: github.Authorization, Token: github.Token, Methods: []string{}}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := oauthClientMethod(tc.meta, tc.secret)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("method=%q err=%v", got, err)
			}
		})
	}
}

func TestGitHubOAuthTokenContract(t *testing.T) {
	for _, variant := range []string{"success", "refresh", "scope-expansion", "oauth-error", "upstream-failure", "malformed"} {
		t.Run(variant, func(t *testing.T) {
			m := testManager(t)
			sealed, err := m.Box.Seal([]byte("synthetic-client-secret"), prefix+"team/oauth-client")
			if err != nil {
				t.Fatal(err)
			}
			r := record{OAuthClientSealed: sealed, View: View{ID: "team", Endpoint: "https://api.githubcopilot.com/mcp/", OAuth: &OAuthView{OAuthConfig: OAuthConfig{Issuer: githubOAuthIssuer, ClientID: "client", Scopes: []string{"read:user", "read:org"}}}}}
			m.OAuthClient = &http.Client{Transport: oauthRoundTrip(func(req *http.Request) (*http.Response, error) {
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if req.Header.Get("Authorization") != "" || req.Form.Get("client_secret") != "synthetic-client-secret" || req.Form.Get("client_id") != "client" || req.Form.Get("resource") != r.Endpoint {
					t.Error("incorrect confidential token request")
				}
				body := `{"access_token":"synthetic-access","token_type":"bearer","scope":"read:org,read:user"}`
				status := 200
				switch variant {
				case "scope-expansion":
					body = `{"access_token":"synthetic-access","token_type":"bearer","scope":"read:org,repo"}`
				case "oauth-error":
					body = `{"error":"bad_verification_code","error_description":"synthetic-client-secret","access_token":"must-not-accept","token_type":"bearer"}`
				case "upstream-failure":
					status = 503
					body = "synthetic-client-secret"
				case "malformed":
					body = "not json"
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})}
			form := url.Values{"grant_type": {"authorization_code"}, "code_verifier": {"proof"}, "code": {"code"}}
			if variant == "refresh" {
				form = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"synthetic-refresh"}}
			}
			result, err := m.tokenRequest(t.Context(), r, oauthMetadata{Issuer: githubOAuthIssuer, Authorization: githubOAuthIssuer + "/authorize", Token: githubOAuthIssuer + "/access_token"}, form)
			if variant == "success" || variant == "refresh" {
				if err != nil || result.Scope != "read:org read:user" {
					t.Fatal(result, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "synthetic-client-secret") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}
