package mcpconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

type clientRegistration struct {
	Mode       string                  `json:"mode"`
	Status     string                  `json:"status"`
	ClientID   string                  `json:"clientId"`
	Redirect   string                  `json:"redirect"`
	Method     string                  `json:"method"`
	Diagnostic *RegistrationDiagnostic `json:"diagnostic,omitempty"`
}

// RegistrationDiagnostic contains only locally authored categories, field names and HTTP status.
// Never retain upstream descriptions, response bodies, headers or credentials.
type RegistrationDiagnostic struct {
	Code       string   `json:"code"`
	HTTPStatus int      `json:"httpStatus,omitempty"`
	Fields     []string `json:"fields,omitempty"`
}

type RegistrationPlan struct {
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}

func manualOAuth(c *OAuthConfig) bool {
	return c.RegistrationMode == "" || c.RegistrationMode == "manual"
}
func oauthClientID(r record) string {
	if r.Registration != nil {
		return r.Registration.ClientID
	}
	return r.OAuth.ClientID
}
func clientDocumentURL(redirect, id string) (string, error) {
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), "localhost") || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/v1/mcp/oauth/callback" {
		return "", oauthError("CIMD requires a provider-reachable public HTTPS installation URL; use DCR or manual registration on localhost")
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !publicIP(ip) {
		return "", oauthError("CIMD requires a public HTTPS installation URL")
	}
	u.Path = "/api/v1/mcp/oauth/client/" + id
	return u.String(), nil
}
func registrationPlan(r record, meta oauthMetadata, redirect string) (RegistrationPlan, error) {
	if manualOAuth(&r.OAuth.OAuthConfig) {
		return RegistrationPlan{"manual", "Use the configured pre-registered client"}, nil
	}
	if r.Registration != nil {
		if r.Registration.Redirect != redirect {
			return RegistrationPlan{}, oauthError("Callback changed; review and save changed OAuth configuration before authorizing")
		}
		if r.Registration.Status != "ready" {
			return RegistrationPlan{}, oauthError("Registration is pending or its outcome is uncertain; inspect the provider before changing registration configuration. No automatic retry was made")
		}
		return RegistrationPlan{r.Registration.Mode, "Reuse the saved client registration for this connection"}, nil
	}
	mode := r.OAuth.RegistrationMode
	_, cimdErr := clientDocumentURL(redirect, r.ID)
	if (mode == "auto" || mode == "cimd") && meta.CIMD && cimdErr == nil && slices.Contains(meta.Methods, "none") {
		return RegistrationPlan{"cimd", "Use a public HTTPS client metadata document; provider consent is still required"}, nil
	}
	if mode == "cimd" {
		if cimdErr != nil {
			return RegistrationPlan{}, cimdErr
		}
		return RegistrationPlan{}, oauthError("Provider does not advertise supported public-client CIMD; choose DCR or manual registration")
	}
	if (mode == "auto" || mode == "dcr") && meta.Registration != "" {
		if err := oauthURL(meta.Registration); err != nil {
			return RegistrationPlan{}, err
		}
		method, err := registrationClientMethod(r, meta)
		if err != nil {
			return RegistrationPlan{}, err
		}
		return RegistrationPlan{"dcr", "Register a client using " + method + " authentication with the advertised provider endpoint; provider consent is still required. CIMD is unavailable or was not selected"}, nil
	}
	return RegistrationPlan{}, oauthError("Automatic client setup unavailable; configure a pre-registered client manually")
}
func (m *Manager) PreviewOAuth(ctx context.Context, id, redirect string) (RegistrationPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, err := m.load(ctx, id)
	if err != nil {
		return RegistrationPlan{}, err
	}
	if r.OAuth == nil || !r.Enabled {
		return RegistrationPlan{}, fmt.Errorf("%w: enabled OAuth connection required", ErrInvalid)
	}
	meta, err := m.metadata(ctx, r)
	if err != nil {
		return RegistrationPlan{}, err
	}
	return registrationPlan(r, meta, redirect)
}

// ClientDocument contains public metadata only, never names, credentials or scopes.
func (m *Manager) ClientDocument(ctx context.Context, id, redirect string) (map[string]any, error) {
	r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if !r.Enabled || r.OAuth == nil || r.Registration == nil || r.Registration.Mode != "cimd" || r.Registration.Status != "ready" || r.Registration.Redirect != redirect {
		return nil, storage.ErrNotFound
	}
	clientID, err := clientDocumentURL(redirect, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"client_id": clientID, "client_name": "Weft", "redirect_uris": []string{redirect}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}, nil
}
func (m *Manager) prepareRegistration(ctx context.Context, r record, meta oauthMetadata, redirect, actor, mode string) (record, error) {
	if r.Registration != nil {
		return r, nil
	}
	// CAS claims persist before any external POST, including across managers/restarts.
	r.Registration = &clientRegistration{Mode: mode, Status: "pending", Redirect: redirect}
	auditActor := actor
	if separator := strings.LastIndex(actor, ":"); separator >= 0 {
		auditActor = actor[:separator]
	}
	v, err := m.save(ctx, r, r.Revision, auditActor, "mcp.oauth.registration_started")
	if err != nil {
		return r, err
	}
	r.Revision = v.Revision
	if mode == "cimd" {
		r.Registration.ClientID, err = clientDocumentURL(redirect, r.ID)
		r.Registration.Method = "none"
	} else {
		err = m.registerClient(ctx, &r, meta, redirect)
	}
	if err != nil {
		r.Registration.Status = "uncertain"
		if r.Registration.Diagnostic == nil {
			r.Registration.Diagnostic = &RegistrationDiagnostic{Code: "local_setup_failed"}
		}
		_, saveErr := m.save(context.WithoutCancel(ctx), r, r.Revision, auditActor, "mcp.oauth.registration_failed")
		if saveErr != nil {
			return r, saveErr
		}
		return r, err
	}
	r.Registration.Status = "ready"
	v, err = m.save(ctx, r, r.Revision, auditActor, "mcp.oauth.registration_ready")
	r.Revision = v.Revision
	return r, err
}
func (m *Manager) registerClient(ctx context.Context, r *record, meta oauthMetadata, redirect string) error {
	r.Registration.Diagnostic = &RegistrationDiagnostic{Code: "request_setup_failed"}
	diagnostic := r.Registration.Diagnostic
	method, err := registrationClientMethod(*r, meta)
	if err != nil {
		return err
	}
	payload := map[string]any{"client_name": "Weft", "redirect_uris": []string{redirect}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": method, "scope": strings.Join(r.OAuth.Scopes, " ")}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err = oauthURL(meta.Registration); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.Registration, bytes.NewReader(b))
	if err != nil {
		return oauthError("Registration request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client, close := m.oauthHTTP()
	defer close()
	diagnostic.Code = "transport_outcome_unknown"
	res, err := client.Do(req)
	if err != nil {
		return oauthError("Registration outcome uncertain; inspect provider registration before retrying")
	}
	defer res.Body.Close()
	diagnostic.HTTPStatus = res.StatusCode
	diagnostic.Code = "response_unreadable"
	data, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(data) > 65536 {
		return oauthError("Registration response exceeds limits")
	}
	diagnostic.Code = "provider_error"
	if err = oauthProviderError(data); err != nil {
		return err
	}
	if res.StatusCode != 201 && res.StatusCode != 200 {
		diagnostic.Code = "http_rejected"
		return oauthError(fmt.Sprintf("Registration rejected or unavailable (provider HTTP %d); review provider policy or use manual registration", res.StatusCode))
	}
	diagnostic.Code = "response_incompatible"
	out, fields := decodeRegistrationResponse(data, method, redirect)
	if len(fields) != 0 {
		diagnostic.Fields = fields
		return oauthError("Registration response incompatible; inspect the safe field diagnostics before retrying")
	}
	for _, scope := range strings.Fields(out.Scope) {
		if !slices.Contains(r.OAuth.Scopes, scope) {
			diagnostic.Code = "scope_expansion"
			diagnostic.Fields = []string{"scope"}
			return oauthError("Registration scope expansion rejected")
		}
	}
	if out.Secret != "" {
		diagnostic.Code = "credential_storage_failed"
		r.OAuthClientSealed, err = m.Box.Seal([]byte(out.Secret), prefix+r.ID+"/oauth-client")
		if err != nil {
			return err
		}
	}
	r.Registration.ClientID = out.ClientID
	r.Registration.Method = method
	r.Registration.Diagnostic = nil
	return nil
}

// registrationResponse is used only after all known fields satisfy the requested
// contract. Values from rejected responses must never enter persisted diagnosis.
type registrationResponse struct {
	ClientID  string
	Secret    string
	Method    string
	Redirects []string
	Scope     string
	Expires   int64
}

// decodeRegistrationResponse reports only fixed, locally authored field names.
// Decode fields independently so JSON type errors cannot leak upstream values or
// cause a misleading diagnosis of an otherwise absent/zero-valued sibling field.
func decodeRegistrationResponse(data []byte, method, redirect string) (registrationResponse, []string) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return registrationResponse{}, []string{"response"}
	}
	var out registrationResponse
	var fields []string
	check := func(name string, target any, valid func() bool) {
		value, present := raw[name]
		if present && (bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, target) != nil) {
			fields = append(fields, name)
			return
		}
		if !valid() {
			fields = append(fields, name)
		}
	}
	check("client_id", &out.ClientID, func() bool { return strings.TrimSpace(out.ClientID) != "" && len(out.ClientID) <= 256 })
	check("client_secret", &out.Secret, func() bool {
		return len(out.Secret) <= 16384 && ((method == "none" && out.Secret == "") || (method != "none" && out.Secret != ""))
	})
	check("token_endpoint_auth_method", &out.Method, func() bool { return out.Method == method })
	check("redirect_uris", &out.Redirects, func() bool { return len(out.Redirects) == 1 && out.Redirects[0] == redirect })
	check("scope", &out.Scope, func() bool { return true })
	check("client_secret_expires_at", &out.Expires, func() bool { return out.Expires == 0 })
	if len(fields) != 0 {
		return registrationResponse{}, fields
	}
	return out, nil
}

// registrationClientMethod keeps the generic and GitLab public-client behavior.
// Weft is a server-side confidential client for the reviewed Atlassian profile
// when the issuer advertises a supported secret-based authentication method.
// Selecting that method never permits the response to substitute another one.
func registrationClientMethod(r record, meta oauthMetadata) (string, error) {
	atlassianService := slices.Contains([]string{"atlassian", "jira", "confluence", "bitbucket"}, r.Service)
	atlassianIssuer := "https://auth.atlassian.com/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3"
	if atlassianService && canonicalMCPResource(r.Endpoint) == atlassianMCPEndpoint && r.OAuth != nil && r.OAuth.Issuer == atlassianIssuer && meta.Issuer == atlassianIssuer && meta.Authorization == "https://auth.atlassian.com/authorize" && meta.Token == "https://auth.atlassian.com/oauth/token" && (slices.Contains(meta.Methods, "client_secret_basic") || slices.Contains(meta.Methods, "client_secret_post")) {
		return oauthClientMethod(meta, true)
	}
	// GitLab's DCR contract is public; the returned method must explicitly agree.
	if slices.Contains(meta.Methods, "none") || r.Service == "gitlab" {
		return "none", nil
	}
	return oauthClientMethod(meta, true)
}
