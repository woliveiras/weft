package mcptest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// OAuthServer is a real TLS identity-provider fixture, not a production issuer.
// It validates the PKCE verifier, redirect URI and resource at code exchange.
type OAuthServer struct {
	*httptest.Server
	mu                        sync.Mutex
	codes                     map[string]url.Values
	hosts                     []string
	Registrations             atomic.Int32
	FailRegistration          atomic.Bool
	RegistrationFailureStatus atomic.Int32
	ClientDocument            func(string) bool
	Refreshes                 atomic.Int32
	Revocations               atomic.Int32
}

func NewOAuth(resource string) *OAuthServer { return newOAuth(resource, "generic") }

// NewGitHubOAuth reproduces GitHub.com metadata and token wire formats locally.
func NewGitHubOAuth() *OAuthServer { return newOAuth("https://api.githubcopilot.com/mcp/", "github") }

const AtlassianIssuer = "https://auth.atlassian.com/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3"

func NewAtlassianOAuth() *OAuthServer {
	return newOAuth("https://mcp.atlassian.com/v2/mcp", "atlassian")
}
func NewGitLabOAuth(origin string) *OAuthServer { return newOAuth(origin+"/api/v4/mcp", "gitlab") }
func newOAuth(resource string, service string) *OAuthServer {
	github := service == "github"
	issuer := ""
	if github {
		issuer = "https://github.com/login/oauth"
	}
	if service == "atlassian" {
		issuer = AtlassianIssuer
	}
	if service == "gitlab" {
		issuer = strings.TrimSuffix(resource, "/api/v4/mcp")
	}
	f := &OAuthServer{codes: map[string]url.Values{}}
	if issuer != "" {
		for _, raw := range []string{issuer, resource} {
			u, _ := url.Parse(raw)
			f.hosts = append(f.hosts, u.Host)
		}
	}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if service == "atlassian" || service == "gitlab" {
			u, _ := url.Parse(issuer)
			origin := u.Scheme + "://" + u.Host
			if r.URL.Path == "/.well-known/oauth-authorization-server"+u.Path {
				methods := []string{"none", "client_secret_post", "client_secret_basic"}
				if service == "gitlab" {
					methods = []string{"client_secret_basic", "client_secret_post"}
				}
				auth := origin + "/authorize"
				if service == "gitlab" {
					auth = origin + "/oauth/authorize"
				}
				json.NewEncoder(w).Encode(map[string]any{"registration_endpoint": origin + "/register", "client_id_metadata_document_supported": service == "atlassian", "issuer": issuer, "authorization_endpoint": auth, "token_endpoint": origin + "/oauth/token", "revocation_endpoint": origin + "/oauth/revoke", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": methods})
				return
			}
			ru, _ := url.Parse(resource)
			if r.URL.Path == ru.Path {
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+ru.Scheme+"://"+ru.Host+"/.well-known/oauth-protected-resource"+ru.Path+`"`)
				w.WriteHeader(401)
				return
			}
			if r.URL.Path == "/.well-known/oauth-protected-resource"+ru.Path {
				json.NewEncoder(w).Encode(map[string]any{"resource": resource, "authorization_servers": []string{issuer}})
				return
			}
		}
		switch r.URL.Path {
		case "/registration-failure":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			f.FailRegistration.Store(r.URL.Query().Get("enabled") == "true")
			fmt.Fprint(w, `{}`)
		case "/register":
			registrationNumber := f.Registrations.Add(1)
			if f.FailRegistration.Load() {
				status := int(f.RegistrationFailureStatus.Load())
				if status == 0 {
					status = 503
				}
				http.Error(w, "synthetic-registration-secret", status)
				return
			}
			var in map[string]any
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&in) != nil || in["token_endpoint_auth_method"] != "none" {
				http.Error(w, "invalid registration", 400)
				return
			}
			in["client_id"] = fmt.Sprintf("registered-fixture-client-%d", registrationNumber)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(in)

		case "/.well-known/oauth-authorization-server/login/oauth":
			if !github {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"issuer": "https://github.com/login/oauth", "authorization_endpoint": "https://github.com/login/oauth/authorize", "token_endpoint": "https://github.com/login/oauth/access_token", "code_challenge_methods_supported": []string{"S256"}})
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"registration_endpoint": f.URL + "/register", "client_id_metadata_document_supported": true, "issuer": f.URL, "authorization_endpoint": f.URL + "/authorize", "token_endpoint": f.URL + "/token", "revocation_endpoint": f.URL + "/revoke", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"}})
		case "/.well-known/oauth-protected-resource/mcp/":
			json.NewEncoder(w).Encode(map[string]any{"resource": resource, "authorization_servers": []string{"https://github.com/login/oauth"}})
		case "/.well-known/oauth-protected-resource/mcp", "/resource":
			json.NewEncoder(w).Encode(map[string]any{"resource": resource, "authorization_servers": []string{f.URL}})
		case "/mcp/":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://api.githubcopilot.com/.well-known/oauth-protected-resource/mcp/"`)
			w.WriteHeader(401)
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+f.URL+`/resource"`)
			w.WriteHeader(401)
		case "/authorize", "/login/oauth/authorize", "/oauth/authorize":
			q := r.URL.Query()
			if strings.HasPrefix(q.Get("client_id"), "https://") && (f.ClientDocument == nil || !f.ClientDocument(q.Get("client_id"))) {
				http.Error(w, "client document unavailable", 400)
				return
			}
			if q.Get("code_challenge_method") != "S256" || q.Get("resource") != resource {
				http.Error(w, "invalid authorization", 400)
				return
			}
			if q.Get("decision") == "" {
				w.Header().Set("Content-Type", "text/html")
				form := template.Must(template.New("consent").Parse(`<h1>Fixture OAuth consent</h1><p>Requested scopes: {{.Get "scope"}}</p><form method="get">{{range $k,$v:=.}}{{range $v}}<input type="hidden" name="{{$k}}" value="{{.}}">{{end}}{{end}}<button name="decision" value="allow">Allow fixture access</button><button name="decision" value="deny">Deny fixture access</button></form>`))
				if err := form.Execute(w, q); err != nil {
					http.Error(w, "render failed", 500)
				}
				return
			}
			target, err := url.Parse(q.Get("redirect_uri"))
			if err != nil {
				http.Error(w, "invalid redirect", 400)
				return
			}
			out := target.Query()
			out.Set("state", q.Get("state"))
			out.Set("iss", f.URL)
			if issuer != "" {
				out.Set("iss", issuer)
			}
			if q.Get("decision") == "deny" {
				out.Set("error", "access_denied")
			} else {
				code := "fixture-code-" + q.Get("state")
				f.mu.Lock()
				f.codes[code] = q
				f.mu.Unlock()
				out.Set("code", code)
			}
			target.RawQuery = out.Encode()
			http.Redirect(w, r, target.String(), 303)
		case "/token", "/login/oauth/access_token", "/oauth/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid form", 400)
				return
			}
			if (service == "gitlab" || service == "atlassian") && (r.Form.Get("client_secret") != "" || r.Header.Get("Authorization") != "") {
				http.Error(w, "public fixture client required", 401)
				return
			}
			if github && (r.Form.Get("client_secret") != "fixture-client-secret" || r.Header.Get("Authorization") != "") {
				json.NewEncoder(w).Encode(map[string]string{"error": "incorrect_client_credentials", "error_description": "fixture-client-secret must not leak"})
				return
			}
			if r.Form.Get("resource") != resource {
				http.Error(w, "wrong resource", 400)
				return
			}
			expiry := 1
			if r.Form.Get("grant_type") == "authorization_code" {
				f.mu.Lock()
				q, ok := f.codes[r.Form.Get("code")]
				delete(f.codes, r.Form.Get("code"))
				f.mu.Unlock()
				hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if !ok || q.Get("client_id") != r.Form.Get("client_id") || q.Get("redirect_uri") != r.Form.Get("redirect_uri") || q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
					http.Error(w, "invalid code proof", 400)
					return
				}
			} else if r.Form.Get("grant_type") == "refresh_token" {
				if r.Form.Get("refresh_token") != "fixture-refresh-token" {
					http.Error(w, "invalid refresh", 400)
					return
				}
				f.Refreshes.Add(1)
				expiry = 3600
				if r.Form.Get("client_id") == "fail-refresh" {
					http.Error(w, "fixture provider unavailable", 503)
					return
				}
			} else {
				http.Error(w, "invalid grant", 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-mcp-token", "refresh_token": "fixture-refresh-token", "token_type": "Bearer", "expires_in": expiry, "scope": func() string {
				if github {
					return "read:user,read:org"
				}
				if service == "gitlab" {
					return "mcp"
				}
				if service == "atlassian" {
					return "read:me offline_access read:jira:agent-interface read:confluence:agent-interface"
				}
				return "read"
			}()})
		case "/revoke", "/oauth/revoke":
			f.Revocations.Add(1)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", 400)
				return
			}
			if r.Form.Get("client_id") == "fail-revoke" {
				http.Error(w, "revocation unavailable", 503)
				return
			}
			fmt.Fprint(w, `{}`)
		case "/stats":
			json.NewEncoder(w).Encode(map[string]int32{"registrations": f.Registrations.Load(), "refreshes": f.Refreshes.Load(), "revocations": f.Revocations.Load()})
		default:
			w.WriteHeader(404)
		}
	}))
	if resource == "" {
		resource = f.URL + "/mcp"
	}
	return f
}

// GitHubClient routes only these fixture public URLs to the TLS server. It is
// injected exclusively by tests; production retains public-network validation.
func (f *OAuthServer) GitHubClient() *http.Client { return OAuthClient(f) }

// OAuthClient routes public contract URLs only inside the fixture harness.
func OAuthClient(fixtures ...*OAuthServer) *http.Client {
	transport := fixtures[0].Client().Transport
	return &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		for _, f := range fixtures {
			for _, host := range f.hosts {
				if r.URL.Host == host {
					target, _ := url.Parse(f.URL)
					r = r.Clone(r.Context())
					u := *r.URL
					u.Scheme, u.Host = target.Scheme, target.Host
					r.URL = &u
					r.Host = target.Host
					return f.Client().Transport.RoundTrip(r)
				}
			}
		}
		return transport.RoundTrip(r)
	})}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
