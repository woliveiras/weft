package mcpconfig

import "errors"

// Only exact, platform-authored transport statuses become actionable guidance.
// Never infer a specific upstream policy from a 403 or echo response headers/body.
func accessGuidance(err error) string {
	var detail *DiscoveryError
	if !errors.As(err, &detail) {
		return ""
	}
	switch detail.Reason {
	case "MCP HTTP status 401":
		return "MCP credentials rejected (401). Open the connection and reauthorize OAuth or replace its credential, then test the connection."
	case "MCP HTTP status 403":
		return "MCP access denied (403). Check the requested resource, credential scopes and organization/site/SSO policies with the provider administrator. A 403 alone does not identify the cause; authorizing again may not resolve it."
	case "MCP HTTP status 429":
		return "MCP provider rate limit reached (429). Wait and check the provider quota before trying again; this is separate from the Weft connection quota."
	default:
		return ""
	}
}
