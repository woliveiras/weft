package mcpconfig

import (
	"fmt"
	"net/url"
	"slices"
)

func validateServiceOAuth(in Input) error {
	if in.Service != "gitlab" || in.OAuth == nil {
		return nil
	}
	issuer, err := url.Parse(in.OAuth.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.Path != "" || issuer.RawQuery != "" || issuer.Fragment != "" || issuer.User != nil || in.Endpoint != issuer.String()+"/api/v4/mcp" {
		return fmt.Errorf("%w: GitLab requires an HTTPS instance origin as issuer and its /api/v4/mcp endpoint", ErrInvalid)
	}
	if !slices.Contains(in.OAuth.Scopes, "mcp") {
		return fmt.Errorf("%w: GitLab MCP requires the mcp scope; REST api scope is not equivalent", ErrInvalid)
	}
	return nil
}

func connectionClientMethod(r record, meta oauthMetadata) (string, error) {
	if r.OAuth != nil && !manualOAuth(&r.OAuth.OAuthConfig) {
		if r.Registration == nil || r.Registration.Status != "ready" {
			return "", oauthError("Client registration required")
		}
		return r.Registration.Method, nil
	}
	// GitLab's MCP registration guide explicitly supports public, pre-registered
	// clients with PKCE, while current metadata lists only confidential methods.
	// Apply that contract only to the selected GitLab instance's canonical paths.
	if r.Service == "gitlab" && r.OAuth != nil && r.OAuthClientSealed == "" && !slices.Contains(meta.Methods, "none") {
		if err := validateServiceOAuth(Input{Service: r.Service, Endpoint: r.Endpoint, OAuth: &r.OAuth.OAuthConfig}); err != nil {
			return "", err
		}
		origin := r.OAuth.Issuer
		if meta.Issuer == origin && meta.Authorization == origin+"/oauth/authorize" && meta.Token == origin+"/oauth/token" && (slices.Contains(meta.Methods, "client_secret_basic") || slices.Contains(meta.Methods, "client_secret_post")) {
			return "none", nil
		}
	}
	return oauthClientMethod(meta, r.OAuthClientSealed != "")
}
