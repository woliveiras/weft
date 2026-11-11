package mcpconfig

import (
	"net/url"
	"strings"
)

const atlassianMCPEndpoint = "https://mcp.atlassian.com/v2/mcp"
const atlassianFlatEndpoint = atlassianMCPEndpoint + "?tools=all"

// This is the only reviewed MCP query option. Never generalize it to arbitrary
// hosts, decoded query aliases, repeated parameters or credentials in URLs.
func flatAtlassianEndpoint(endpoint string) bool { return endpoint == atlassianFlatEndpoint }
func canonicalMCPResource(endpoint string) string {
	if flatAtlassianEndpoint(endpoint) {
		return atlassianMCPEndpoint
	}
	return endpoint
}
func oauthResource(endpoint string, metadata oauthMetadata) string {
	if metadata.Resource != "" {
		return metadata.Resource
	}
	return canonicalMCPResource(endpoint)
}

// Service labels are user-editable; the official host independently identifies
// Atlassian so relabeling a connection cannot bypass operation-level grants.
func atlassianConnection(v View) bool {
	switch v.Service {
	case "atlassian", "jira", "confluence", "bitbucket":
		return true
	}
	u, err := url.Parse(v.Endpoint)
	return err == nil && strings.EqualFold(u.Hostname(), "mcp.atlassian.com")
}
func blockedDispatcher(v View, name string) bool {
	if !atlassianConnection(v) {
		return false
	}
	switch name {
	case "executeRead", "executeWrite", "executeDestructive":
		return true
	}
	return false
}
func operationCatalogue(v View) View {
	if !atlassianConnection(v) && len(v.ArgumentPolicy) == 0 {
		return v
	}
	filtered := make([]Tool, 0, len(v.Tools))
	for _, tool := range v.Tools {
		if !blockedDispatcher(v, tool.Name) && atlassianPolicyTool(v, tool) && argumentPolicyTool(v.ArgumentPolicy, tool) {
			filtered = append(filtered, tool)
		}
	}
	v.Tools = filtered
	return v
}
