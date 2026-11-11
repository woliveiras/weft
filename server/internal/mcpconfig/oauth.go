package mcpconfig

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/domain"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

var ErrOAuth = errors.New("OAuth connection failed")

func oauthError(message string) error { return fmt.Errorf("%w: %s", ErrOAuth, message) }

type OAuthConfig struct {
	RegistrationMode string   `json:"registrationMode,omitempty"`
	Issuer           string   `json:"issuer"`
	ClientID         string   `json:"clientId"`
	Scopes           []string `json:"scopes"`
}
type OAuthView struct {
	RegistrationDiagnostic    *RegistrationDiagnostic `json:"registrationDiagnostic,omitempty"`
	RegistrationStatus        string                  `json:"registrationStatus,omitempty"`
	EffectiveRegistrationMode string                  `json:"effectiveRegistrationMode,omitempty"`
	OAuthConfig
	Status                 string     `json:"status"`
	ExpiresAt              *time.Time `json:"expiresAt,omitempty"`
	GrantedScopes          []string   `json:"grantedScopes,omitempty"`
	ClientSecretConfigured bool       `json:"clientSecretConfigured"`
	Revocation             string     `json:"revocation,omitempty"`
}
type oauthMetadata struct {
	Resource      string   `json:"resource,omitempty"`
	Registration  string   `json:"registration_endpoint"`
	CIMD          bool     `json:"client_id_metadata_document_supported"`
	Issuer        string   `json:"issuer"`
	Authorization string   `json:"authorization_endpoint"`
	Token         string   `json:"token_endpoint"`
	Revocation    string   `json:"revocation_endpoint"`
	Challenges    []string `json:"code_challenge_methods_supported"`
	Methods       []string `json:"token_endpoint_auth_methods_supported"`
}
type oauthPending struct {
	ID, Actor, Binding, Verifier, Redirect, Code string
	Revision                                     int64
	Metadata                                     oauthMetadata
	Expires                                      time.Time
	Returned, Denied                             bool
}
type OAuthStart struct {
	URL     string `json:"url"`
	State   string `json:"state"`
	Binding string `json:"-"`
}

var oauthScope = regexp.MustCompile(`^[\x21\x23-\x5b\x5d-\x7e]{1,128}$`)
var oauthState = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func oauthURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > 2048 {
		return fmt.Errorf("%w: OAuth endpoints must be HTTPS without query, user info or fragment", ErrInvalid)
	}
	return nil
}
func validateOAuth(c *OAuthConfig) error {
	if c == nil {
		return nil
	}
	if err := oauthURL(c.Issuer); err != nil {
		return err
	}
	if !slices.Contains([]string{"", "manual", "auto", "dcr", "cimd"}, c.RegistrationMode) || (manualOAuth(c) && strings.TrimSpace(c.ClientID) == "") || (!manualOAuth(c) && c.ClientID != "") || len(c.ClientID) > 256 || len(c.Scopes) > 50 {
		return fmt.Errorf("%w: invalid OAuth client or scopes", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, scope := range c.Scopes {
		if !oauthScope.MatchString(scope) || seen[scope] {
			return fmt.Errorf("%w: invalid or duplicate OAuth scope", ErrInvalid)
		}
		seen[scope] = true
	}
	return nil
}
func nonce() string       { return base64.RawURLEncoding.EncodeToString(randomBytes()) }
func randomBytes() []byte { b := make([]byte, 32); _, _ = rand.Read(b); return b } // crypto/rand.Read cannot fail on supported Go platforms.
func (m *Manager) oauthHTTP() (*http.Client, func()) {
	if m.OAuthClient != nil {
		return m.OAuthClient, func() {}
	}
	return remoteClient()
}
func oauthJSON(ctx context.Context, client *http.Client, endpoint string, form url.Values, secret string, out any) error {
	if err := oauthURL(endpoint); err != nil {
		return err
	}
	var body io.Reader
	method := http.MethodGet
	if form != nil {
		method = http.MethodPost
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return oauthError("OAuth request invalid")
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if secret != "" {
		req.SetBasicAuth(url.QueryEscape(form.Get("client_id")), url.QueryEscape(secret))
	}
	res, err := client.Do(req)
	if err != nil {
		return oauthError("OAuth service unavailable")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(data) > 65536 {
		return oauthError("OAuth response exceeds limits")
	}
	if err := oauthProviderError(data); err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return oauthError("OAuth request rejected; authorize again or check provider settings")
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		return oauthError("OAuth response invalid")
	}
	return nil
}

// GitHub documents client_secret_post although its metadata omits auth methods.
// Keep this interoperability rule bound to the exact GitHub.com issuer/endpoints.
const githubOAuthIssuer = "https://github.com/login/oauth"

func oauthClientMethod(meta oauthMetadata, confidential bool) (string, error) {
	methods := meta.Methods
	if meta.Issuer == githubOAuthIssuer {
		if meta.Authorization != githubOAuthIssuer+"/authorize" || meta.Token != githubOAuthIssuer+"/access_token" {
			return "", fmt.Errorf("%w: unexpected GitHub OAuth endpoints", ErrInvalid)
		}
		if !confidential {
			return "", fmt.Errorf("%w: GitHub OAuth requires a registered client secret", ErrInvalid)
		}
		if methods == nil || slices.Contains(methods, "client_secret_post") {
			return "client_secret_post", nil
		}
	}
	// RFC 8414 defaults an omitted field to client_secret_basic, not public access.
	if methods == nil {
		methods = []string{"client_secret_basic"}
	}
	if confidential {
		for _, method := range []string{"client_secret_basic", "client_secret_post"} {
			if slices.Contains(methods, method) {
				return method, nil
			}
		}
	} else if slices.Contains(methods, "none") {
		return "none", nil
	}
	return "", fmt.Errorf("%w: OAuth client authentication method unsupported", ErrInvalid)
}

// Only allowlisted error codes become authored guidance; upstream descriptions,
// URIs and other payloads can contain credentials and must never be echoed.
func oauthProviderError(data []byte) error {
	var response struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &response) != nil || response.Error == "" {
		return nil
	}
	switch response.Error {
	case "invalid_client", "incorrect_client_credentials", "unauthorized_client":
		return oauthError("OAuth client registration rejected; check the client ID, authentication method and registered client secret")
	case "invalid_grant", "bad_verification_code", "redirect_uri_mismatch":
		return oauthError("OAuth authorization expired or callback rejected; verify the exact registered callback and start authorization again")
	case "invalid_scope", "insufficient_scope":
		return oauthError("OAuth scopes rejected; use the MCP scopes permitted by the client registration and administrator")
	case "access_denied":
		return oauthError("OAuth access denied; review consent and corporate site, domain, IP or SSO policies with the administrator")
	default:
		return oauthError("OAuth provider rejected the request; check client registration and access policy")
	}
}

func (m *Manager) metadata(ctx context.Context, r record) (oauthMetadata, error) {
	var meta oauthMetadata
	client, close := m.oauthHTTP()
	defer close()
	u, _ := url.Parse(r.Endpoint)
	probe, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Endpoint, nil)
	if err != nil {
		return meta, oauthError("Invalid MCP endpoint")
	}
	probe.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(probe)
	if err != nil {
		return meta, oauthError("OAuth resource discovery unavailable")
	}
	challenge := response.Header.Get("WWW-Authenticate")
	response.Body.Close()
	// Resource metadata is obtained without sending existing credentials.
	resourceURL := u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource" + strings.TrimRight(u.Path, "/")
	if match := regexp.MustCompile(`(?i)resource_metadata="([^"]+)"`).FindStringSubmatch(challenge); len(match) == 2 {
		resourceURL = match[1]
	}
	var resource struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	if err := oauthJSON(ctx, client, resourceURL, nil, "", &resource); err != nil {
		return meta, err
	}
	if (resource.Resource != r.Endpoint && resource.Resource != canonicalMCPResource(r.Endpoint)) || !slices.Contains(resource.Servers, r.OAuth.Issuer) {
		return meta, fmt.Errorf("%w: OAuth issuer is not advertised for this MCP resource", ErrInvalid)
	}
	issuer, _ := url.Parse(r.OAuth.Issuer)
	discovery := issuer.Scheme + "://" + issuer.Host + "/.well-known/oauth-authorization-server" + strings.TrimRight(issuer.Path, "/")
	if err := oauthJSON(ctx, client, discovery, nil, "", &meta); err != nil {
		return meta, err
	}
	meta.Resource = resource.Resource
	if meta.Issuer != r.OAuth.Issuer || !slices.Contains(meta.Challenges, "S256") {
		return meta, fmt.Errorf("%w: issuer mismatch or PKCE S256 unavailable", ErrInvalid)
	}
	for _, endpoint := range []string{meta.Authorization, meta.Token} {
		if err := oauthURL(endpoint); err != nil {
			return meta, err
		}
	}
	if meta.Revocation != "" {
		if err := oauthURL(meta.Revocation); err != nil {
			return meta, err
		}
	}
	if manualOAuth(&r.OAuth.OAuthConfig) {
		if _, err := connectionClientMethod(r, meta); err != nil {
			return meta, err
		}
	}
	if m.OAuthClient == nil {
		auth, _ := url.Parse(meta.Authorization)
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", auth.Hostname())
		if e != nil || len(ips) == 0 {
			return meta, oauthError("OAuth authorization host unavailable")
		}
		for _, ip := range ips {
			if !publicIP(netip.Addr(ip)) {
				return meta, fmt.Errorf("%w: OAuth authorization host must be public", ErrInvalid)
			}
		}
	}
	return meta, nil
}
func (m *Manager) StartOAuth(ctx context.Context, id, actor, redirect string) (OAuthStart, error) {
	return m.StartOAuthReviewed(ctx, id, actor, redirect, "")
}
func (m *Manager) StartOAuthReviewed(ctx context.Context, id, actor, redirect, approvedMode string) (OAuthStart, error) {
	r, err := m.load(ctx, id)
	if err != nil {
		return OAuthStart{}, err
	}
	if !r.Enabled || r.OAuth == nil {
		return OAuthStart{}, fmt.Errorf("%w: enabled OAuth connection required", ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	meta, err := m.metadata(ctx, r)
	if err != nil {
		return OAuthStart{}, err
	}
	if !manualOAuth(&r.OAuth.OAuthConfig) {
		plan, e := registrationPlan(r, meta, redirect)
		if e != nil {
			return OAuthStart{}, e
		}
		if approvedMode != plan.Mode {
			return OAuthStart{}, oauthError("Review the selected registration mechanism before authorizing")
		}
		r, err = m.prepareRegistration(ctx, r, meta, redirect, actor, plan.Mode)
		if err != nil {
			return OAuthStart{}, err
		}
	}
	state, binding, verifier := nonce(), nonce(), nonce()
	hash := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {oauthClientID(r)}, "redirect_uri": {redirect}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}, "scope": {strings.Join(r.OAuth.Scopes, " ")}, "resource": {oauthResource(r.Endpoint, meta)}}
	err = m.updatePending(ctx, func(entries map[string]oauthPending) error {
		if len(entries) >= maxOAuthPending {
			return fmt.Errorf("%w: too many pending OAuth authorizations", ErrInvalid)
		}
		entries[state] = oauthPending{ID: id, Actor: actor, Binding: binding, Verifier: verifier, Redirect: redirect, Revision: r.Revision, Metadata: meta, Expires: time.Now().Add(10 * time.Minute)}
		return nil
	})
	if err != nil {
		return OAuthStart{}, err
	}
	return OAuthStart{URL: meta.Authorization + "?" + q.Encode(), State: state, Binding: binding}, nil
}

// CaptureOAuth accepts the provider callback but defers token exchange until the
// original authenticated Weft session explicitly completes it.
func (m *Manager) CaptureOAuth(state, binding, code, issuer, providerError string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.updatePending(ctx, func(entries map[string]oauthPending) error {
		p, ok := entries[state]
		if !ok || !oauthState.MatchString(state) || p.Binding != binding || p.Returned {
			return fmt.Errorf("%w: invalid or expired OAuth return", ErrInvalid)
		}
		if issuer != "" && issuer != p.Metadata.Issuer {
			return fmt.Errorf("%w: OAuth issuer mismatch", ErrInvalid)
		}
		if len(code) > 8192 || (code == "") == (providerError == "") {
			return fmt.Errorf("%w: invalid OAuth response", ErrInvalid)
		}
		p.Code = code
		p.Denied = providerError != ""
		p.Returned = true
		entries[state] = p
		return nil
	})
}

type oauthTokens struct {
	Error   string `json:"error"`
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Type    string `json:"token_type"`
	Expires int64  `json:"expires_in"`
	Scope   string `json:"scope"`
}

func (m *Manager) tokenRequest(ctx context.Context, r record, meta oauthMetadata, form url.Values) (oauthTokens, error) {
	var result oauthTokens
	client, close := m.oauthHTTP()
	defer close()
	form.Set("client_id", oauthClientID(r))
	form.Set("resource", oauthResource(r.Endpoint, meta))
	secret := ""
	if r.OAuthClientSealed != "" {
		b, err := m.Box.Open(r.OAuthClientSealed, prefix+r.ID+"/oauth-client")
		if err != nil {
			return result, oauthError("OAuth client credential unavailable")
		}
		secret = string(b)
	}
	method, err := connectionClientMethod(r, meta)
	if err != nil {
		return result, err
	}
	if method == "client_secret_post" {
		form.Set("client_secret", secret)
		secret = ""
	}
	err = oauthJSON(ctx, client, meta.Token, form, secret, &result)
	if err != nil {
		return result, err
	}
	if result.Error != "" || result.Access == "" || len(result.Access) > 16384 || len(result.Refresh) > 16384 || !strings.EqualFold(result.Type, "Bearer") || result.Expires < 0 || result.Expires > 31536000 || strings.IndexFunc(result.Access, func(c rune) bool { return c < 33 || c > 126 }) >= 0 {
		return result, oauthError("OAuth token response invalid")
	}
	if meta.Issuer == githubOAuthIssuer {
		result.Scope = strings.Join(strings.FieldsFunc(result.Scope, func(c rune) bool { return c == ',' || c == ' ' }), " ")
	}
	if result.Scope != "" {
		for _, scope := range strings.Fields(result.Scope) {
			if !slices.Contains(r.OAuth.Scopes, scope) {
				return result, oauthError("OAuth scope expansion rejected")
			}
		}
	}
	return result, nil
}
func (m *Manager) installTokens(r *record, t oauthTokens) error {
	var err error
	r.Sealed, err = m.Box.Seal([]byte(t.Access), prefix+r.ID)
	if err != nil {
		return err
	}
	r.OAuthRefreshSealed = ""
	if t.Refresh != "" {
		r.OAuthRefreshSealed, err = m.Box.Seal([]byte(t.Refresh), prefix+r.ID+"/oauth-refresh")
		if err != nil {
			return err
		}
	}
	r.OAuth.ExpiresAt = nil
	if t.Expires > 0 {
		expires := time.Now().Add(time.Duration(t.Expires) * time.Second)
		r.OAuth.ExpiresAt = &expires
	}
	r.OAuth.Status = "authorized"
	r.OAuth.Revocation = ""
	if t.Scope != "" {
		r.OAuth.GrantedScopes = strings.Fields(t.Scope)
	} else if len(r.OAuth.GrantedScopes) == 0 {
		r.OAuth.GrantedScopes = append([]string{}, r.OAuth.Scopes...)
	}
	return nil
}
func (m *Manager) CompleteOAuth(ctx context.Context, state, binding, actor string) (View, error) {
	var p oauthPending
	err := m.updatePending(ctx, func(entries map[string]oauthPending) error {
		entry, ok := entries[state]
		if !ok || !oauthState.MatchString(state) || entry.Binding != binding || entry.Actor != actor || !entry.Returned {
			return fmt.Errorf("%w: OAuth session changed, expired or already completed", ErrInvalid)
		}
		p = entry
		delete(entries, state)
		return nil
	})
	if err != nil {
		return View{}, err
	}
	// The caller's session digest is encrypted in the pending transaction,
	// never used as an audit actor. Consumption precedes token exchange: a crash
	// here requires fresh authorization, never an uncertain code exchange retry.
	auditActor := actor
	if separator := strings.LastIndex(actor, ":"); separator >= 0 {
		auditActor = actor[:separator]
	}
	r, err := m.load(ctx, p.ID)
	if err != nil {
		return View{}, err
	}
	if r.Revision != p.Revision || !r.Enabled || r.OAuth == nil {
		return View{}, storage.ErrConflict
	}
	if p.Denied {
		return View{}, fmt.Errorf("%w: authorization cancelled or denied by provider", ErrInvalid)
	}
	r.OAuth.Status = "exchanging"
	r.Status = "untested"
	r.Tools = []Tool{}
	r.Sealed = ""
	r.OAuthRefreshSealed = ""
	v, err := m.save(ctx, r, r.Revision, auditActor, "mcp.oauth.exchanging")
	if err != nil {
		return View{}, err
	}
	r.Revision = v.Revision
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tokens, err := m.tokenRequest(ctx, r, p.Metadata, url.Values{"grant_type": {"authorization_code"}, "code": {p.Code}, "redirect_uri": {p.Redirect}, "code_verifier": {p.Verifier}})
	if err != nil {
		_, saveErr := m.requireAuthorization(context.WithoutCancel(ctx), r)
		if saveErr != nil {
			return View{}, saveErr
		}
		return View{}, err
	}
	r.OAuthMeta = p.Metadata
	if err = m.installTokens(&r, tokens); err != nil {
		return View{}, err
	}
	v, err = m.save(ctx, r, r.Revision, auditActor, "mcp.oauth.authorized")
	if err != nil {
		m.discardTokens(ctx, r, tokens)
		return View{}, err
	}
	// Discovery failure is visible in the connection, not a successful ready state.
	return m.Refresh(ctx, v.ID, auditActor)
}

// A provider-side invalidation is surfaced as reauthorization, never a replay.
func (m *Manager) noteOAuthUnauthorized(ctx context.Context, r record, err error) error {
	var detail *DiscoveryError
	if r.OAuth != nil && errors.As(err, &detail) && detail.Reason == "MCP HTTP status 401" {
		_, saveErr := m.requireAuthorization(context.WithoutCancel(ctx), r)
		return saveErr
	}
	return nil
}
func (m *Manager) requireAuthorization(ctx context.Context, r record) (View, error) {
	r.Sealed = ""
	r.OAuthRefreshSealed = ""
	r.OAuth.Status = "reauthorization_required"
	r.OAuth.ExpiresAt = nil
	r.Status = "unavailable"
	r.Tools = []Tool{}
	r.Error = "OAuth authorization required"
	return m.save(ctx, r, r.Revision, "oauth", "mcp.oauth.reauthorization_required")
}
func (m *Manager) accessToken(ctx context.Context, r record) (record, string, error) {
	if r.OAuth != nil {
		copyView := *r.OAuth
		r.OAuth = &copyView
	}
	deadline := time.Now().Add(15 * time.Second)
	for r.OAuth != nil {
		if !r.Enabled {
			return r, "", fmt.Errorf("%w: connection disabled", ErrInvalid)
		}
		if r.OAuth.Status == "refreshing" {
			if time.Since(r.UpdatedAt) > 45*time.Second {
				_, err := m.requireAuthorization(ctx, r)
				if err != nil {
					return r, "", err
				}
				return r, "", oauthError("OAuth renewal interrupted; authorize again")
			}
			if time.Now().After(deadline) {
				return r, "", oauthError("OAuth renewal is busy")
			}
			select {
			case <-ctx.Done():
				return r, "", ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
			var err error
			r, err = m.load(ctx, r.ID)
			if err != nil {
				return r, "", err
			}
			continue
		}
		if r.OAuth.Status != "authorized" {
			return r, "", fmt.Errorf("%w: OAuth authorization required", ErrInvalid)
		}
		if r.OAuth.ExpiresAt == nil || time.Until(*r.OAuth.ExpiresAt) > 5*time.Second {
			break
		}
		if r.OAuthRefreshSealed == "" {
			_, err := m.requireAuthorization(ctx, r)
			if err != nil {
				return r, "", err
			}
			return r, "", oauthError("OAuth token expired; authorize again")
		}
		b, err := m.Box.Open(r.OAuthRefreshSealed, prefix+r.ID+"/oauth-refresh")
		if err != nil {
			return r, "", oauthError("OAuth refresh credential unavailable")
		}
		refresh := string(b)
		r.OAuth.Status = "refreshing"
		v, err := m.save(ctx, r, r.Revision, "oauth", "mcp.oauth.refreshing")
		if errors.Is(err, storage.ErrConflict) {
			r, err = m.load(ctx, r.ID)
			if err != nil {
				return r, "", err
			}
			continue
		}
		if err != nil {
			return r, "", err
		}
		r.Revision = v.Revision
		opctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		tokens, err := m.tokenRequest(opctx, r, r.OAuthMeta, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
		cancel()
		if err != nil {
			_, saveErr := m.requireAuthorization(context.WithoutCancel(ctx), r)
			if saveErr != nil {
				return r, "", saveErr
			}
			return r, "", err
		}
		if tokens.Scope != "" {
			for _, scope := range strings.Fields(tokens.Scope) {
				if !slices.Contains(r.OAuth.GrantedScopes, scope) {
					_, saveErr := m.requireAuthorization(context.WithoutCancel(ctx), r)
					if saveErr != nil {
						return r, "", saveErr
					}
					m.discardTokens(ctx, r, tokens)
					return r, "", oauthError("OAuth renewal scope expansion rejected")
				}
			}
		}
		if tokens.Refresh == "" {
			tokens.Refresh = refresh
		}
		if err = m.installTokens(&r, tokens); err != nil {
			return r, "", err
		}
		v, err = m.save(ctx, r, r.Revision, "oauth", "mcp.oauth.renewed")
		if err != nil {
			m.discardTokens(ctx, r, tokens)
			return r, "", err
		}
		r.Revision = v.Revision
		break
	}
	if r.Sealed == "" {
		return r, "", nil
	}
	token, err := m.Box.Open(r.Sealed, prefix+r.ID)
	if err != nil {
		return r, "", oauthError("MCP credential unavailable")
	}
	if r.CredentialType == "basic" {
		return r, base64.StdEncoding.EncodeToString(token), nil
	}
	return r, string(token), nil
}
func (m *Manager) discardTokens(ctx context.Context, r record, t oauthTokens) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	status, err := m.revokeRemote(cleanup, r, t)
	if err != nil {
		status = "failed"
	}
	if m.Bus != nil {
		m.Bus.Publish(cleanup, domain.NewEvent("mcp.oauth.cleanup", "", map[string]any{"connectionId": r.ID, "outcome": status}).WithActor("oauth"))
	}
}
func (m *Manager) revokeRemote(ctx context.Context, r record, t oauthTokens) (string, error) {
	if r.OAuthMeta.Revocation == "" {
		return "unsupported", nil
	}
	client, close := m.oauthHTTP()
	defer close()
	secret := ""
	if r.OAuthClientSealed != "" {
		b, err := m.Box.Open(r.OAuthClientSealed, prefix+r.ID+"/oauth-client")
		if err != nil {
			return "failed", err
		}
		secret = string(b)
	}
	for _, entry := range []struct{ value, hint string }{{t.Refresh, "refresh_token"}, {t.Access, "access_token"}} {
		if entry.value == "" {
			continue
		}
		form := url.Values{"client_id": {oauthClientID(r)}, "token": {entry.value}, "token_type_hint": {entry.hint}}
		method, err := connectionClientMethod(r, r.OAuthMeta)
		if err != nil {
			return "failed", err
		}
		basicSecret := secret
		if method == "client_secret_post" {
			form.Set("client_secret", secret)
			basicSecret = ""
		}
		err = oauthJSON(ctx, client, r.OAuthMeta.Revocation, form, basicSecret, nil)
		if err != nil {
			return "failed", err
		}
	}
	return "requested", nil
}
func (m *Manager) RevokeOAuth(ctx context.Context, id, actor string) (View, error) {
	var r record
	var err error
	for attempts := 0; attempts < 5; attempts++ {
		r, err = m.load(ctx, id)
		if err != nil {
			return View{}, err
		}
		if r.OAuth == nil {
			return View{}, fmt.Errorf("%w: OAuth connection required", ErrInvalid)
		}
		local := r
		copyView := *r.OAuth
		local.OAuth = &copyView
		local.Sealed = ""
		local.OAuthRefreshSealed = ""
		local.OAuth.Status = "revoked"
		local.OAuth.ExpiresAt = nil
		local.OAuth.Revocation = "pending"
		local.Status = "unavailable"
		local.Tools = []Tool{}
		local.Error = "OAuth access revoked"
		v, e := m.save(ctx, local, r.Revision, actor, "mcp.oauth.revoked")
		if errors.Is(e, storage.ErrConflict) {
			continue
		}
		if e != nil {
			return View{}, e
		}
		tokens := oauthTokens{}
		// Local denial is committed before any upstream operation.
		if r.Sealed != "" {
			b, e := m.Box.Open(r.Sealed, prefix+r.ID)
			if e != nil {
				local.OAuth.Revocation = "failed"
			} else {
				tokens.Access = string(b)
			}
		}
		if r.OAuthRefreshSealed != "" {
			b, e := m.Box.Open(r.OAuthRefreshSealed, prefix+r.ID+"/oauth-refresh")
			if e != nil {
				local.OAuth.Revocation = "failed"
			} else {
				tokens.Refresh = string(b)
			}
		}
		if local.OAuth.Revocation != "failed" {
			op, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			status, _ := m.revokeRemote(op, r, tokens)
			cancel()
			local.OAuth.Revocation = status
		}
		local.Revision = v.Revision
		saved, e := m.save(context.WithoutCancel(ctx), local, v.Revision, actor, "mcp.oauth.revocation_result")
		if errors.Is(e, storage.ErrConflict) {
			return m.Get(ctx, id)
		}
		return saved, e
	}
	return View{}, storage.ErrConflict
}
