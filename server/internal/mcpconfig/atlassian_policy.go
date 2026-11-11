package mcpconfig

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// AtlassianPolicy is a reviewed read-operation boundary, not a replacement for
// upstream scopes, site permissions or exact agent grants.
type AtlassianPolicy struct {
	Products []string `json:"products"`
	CloudIDs []string `json:"cloudIds"`
}

// Deliberately finite: new catalogue entries need review, never prefix matching.
var atlassianReadProducts = map[string]string{
	"getJiraIssue":             "jira",
	"listJiraProjects":         "jira",
	"searchJiraIssuesUsingJql": "jira",
	"getConfluenceContent":     "confluence",
	"listConfluenceSpaces":     "confluence",
	"getConfluenceSpace":       "confluence",
}

func validateAtlassianPolicy(in Input) error {
	p := in.AtlassianPolicy
	if p == nil {
		return nil
	}
	if !atlassianConnection(View{Service: in.Service, Endpoint: in.Endpoint}) || len(p.Products) < 1 || len(p.Products) > 2 || len(p.CloudIDs) < 1 || len(p.CloudIDs) > 32 {
		return fmt.Errorf("%w: select Atlassian products and 1–32 cloud IDs", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, product := range p.Products {
		if (product != "jira" && product != "confluence") || seen[product] {
			return fmt.Errorf("%w: unsupported or duplicate Atlassian product", ErrInvalid)
		}
		seen[product] = true
	}
	seen = map[string]bool{}
	for _, id := range p.CloudIDs {
		if !workspaceHeaderID.MatchString(id) || seen[strings.ToLower(id)] {
			return fmt.Errorf("%w: cloud IDs must be distinct UUIDs", ErrInvalid)
		}
		seen[strings.ToLower(id)] = true
	}
	return nil
}
func atlassianPolicyTool(v View, tool Tool) bool {
	if v.AtlassianPolicy == nil {
		return true
	}
	product := atlassianReadProducts[tool.Name]
	if product == "" || !slices.Contains(v.AtlassianPolicy.Products, product) {
		return false
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	return json.Unmarshal(tool.InputSchema, &schema) == nil && schema.Properties["cloudId"].Type == "string"
}
func atlassianPolicyArguments(v View, tool Tool, args map[string]json.RawMessage) bool {
	if v.AtlassianPolicy == nil {
		return true
	}
	if !atlassianPolicyTool(v, tool) {
		return false
	}
	var id string
	if json.Unmarshal(args["cloudId"], &id) != nil {
		return false
	}
	for _, allowed := range v.AtlassianPolicy.CloudIDs {
		if strings.EqualFold(id, allowed) {
			return true
		}
	}
	return false
}
