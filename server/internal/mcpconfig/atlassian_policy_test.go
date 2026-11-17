package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

const allowedCloud = "11111111-1111-1111-1111-111111111111"

func policyTool(name string) Tool {
	return Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object","properties":{"cloudId":{"type":"string"}}}`)}
}
func TestAtlassianPolicyValidationAndCatalogue(t *testing.T) {
	base := Input{Service: "atlassian", Endpoint: atlassianFlatEndpoint}
	for _, p := range []*AtlassianPolicy{nil, {Products: []string{"jira"}, CloudIDs: []string{allowedCloud}}, {Products: []string{"jira", "confluence"}, CloudIDs: []string{allowedCloud}}} {
		base.AtlassianPolicy = p
		if err := validateAtlassianPolicy(base); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []*AtlassianPolicy{{}, {Products: []string{"jira"}}, {Products: []string{"bitbucket"}, CloudIDs: []string{allowedCloud}}, {Products: []string{"jira", "jira"}, CloudIDs: []string{allowedCloud}}, {Products: []string{"jira"}, CloudIDs: []string{"https://site.atlassian.net"}}, {Products: []string{"jira"}, CloudIDs: []string{allowedCloud, allowedCloud}}} {
		base.AtlassianPolicy = p
		if validateAtlassianPolicy(base) == nil {
			t.Fatal("invalid policy accepted", p)
		}
	}
	base.Service = "generic"
	base.Endpoint = "https://other.example/mcp"
	base.AtlassianPolicy = &AtlassianPolicy{Products: []string{"jira"}, CloudIDs: []string{allowedCloud}}
	if validateAtlassianPolicy(base) == nil {
		t.Fatal("non-Atlassian policy accepted")
	}
	base.Service = "atlassian"
	base.AtlassianPolicy.CloudIDs = make([]string, 33)
	if validateAtlassianPolicy(base) == nil {
		t.Fatal("unbounded site list accepted")
	}
	p := &AtlassianPolicy{Products: []string{"jira"}, CloudIDs: []string{allowedCloud}}
	view := View{Service: "atlassian", AtlassianPolicy: p}
	for _, tc := range []struct {
		name    string
		allowed bool
	}{{"getJiraIssue", true}, {"listJiraProjects", true}, {"searchJiraIssuesUsingJql", true}, {"getConfluenceContent", false}, {"executeRead", false}, {"getJiraIssueUnexpected", false}, {"search", false}, {"createJiraIssue", false}, {"getAccessibleAtlassianResources", false}} {
		if atlassianPolicyTool(view, policyTool(tc.name)) != tc.allowed {
			t.Fatal(tc.name)
		}
	}
	if atlassianPolicyTool(view, Tool{Name: "getJiraIssue", InputSchema: json.RawMessage(`{"type":"object"}`)}) {
		t.Fatal("missing site schema accepted")
	}
	for _, args := range []string{`{}`, `{"cloudId":null}`, `{"cloudId":1}`, `{"cloudId":"22222222-2222-2222-2222-222222222222"}`, `{"cloudId":"https://site.atlassian.net"}`} {
		var obj map[string]json.RawMessage
		json.Unmarshal([]byte(args), &obj)
		if atlassianPolicyArguments(view, policyTool("getJiraIssue"), obj) {
			t.Fatal("site escape accepted", args)
		}
	}
}

func TestAtlassianPolicyGatewayAndStoredRevision(t *testing.T) {
	m := testManager(t)
	calls := 0
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return []Tool{policyTool("getJiraIssue"), policyTool("getConfluenceContent"), policyTool("executeRead"), policyTool("search")}, nil
	}
	m.Call = func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"content":[{"type":"text","text":"scoped result"}]}`), nil
	}
	in := Input{ID: "bounded", Name: "Bounded", Service: "atlassian", Endpoint: atlassianFlatEndpoint, Enabled: true, AtlassianPolicy: &AtlassianPolicy{Products: []string{"jira"}, CloudIDs: []string{allowedCloud}}}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	view, err := m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || len(view.Tools) != 1 || view.Tools[0].Name != "getJiraIssue" {
		t.Fatal(view, err)
	}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = m.Resolve
	principal := &tools.Principal{GraphID: "one", NodeID: "reader", Grants: []domain.ToolGrant{{Name: "mcp.bounded.getJiraIssue"}, {Name: "mcp.bounded.getConfluenceContent"}}}
	valid := json.RawMessage(`{"cloudId":"` + allowedCloud + `","issueIdOrKey":"TEST-1"}`)
	for _, request := range []struct {
		name string
		args json.RawMessage
	}{{"mcp.bounded.getConfluenceContent", valid}, {"mcp.bounded.getJiraIssue", json.RawMessage(`{"cloudId":"22222222-2222-2222-2222-222222222222"}`)}, {"mcp.bounded.getJiraIssue", json.RawMessage(`{}`)}} {
		if _, err := gateway.Invoke(t.Context(), principal, request.name, request.args); !errors.Is(err, tools.ErrDenied) {
			t.Fatal("policy not denied", err)
		}
	}
	if calls != 0 {
		t.Fatal("denied calls reached upstream")
	}
	if _, err := gateway.Invoke(t.Context(), principal, "mcp.bounded.getJiraIssue", valid); err != nil {
		t.Fatal(err)
	}
	stale, err := m.Resolve(t.Context(), "mcp.bounded.getJiraIssue")
	if err != nil {
		t.Fatal(err)
	}
	in.AtlassianPolicy = &AtlassianPolicy{Products: []string{"confluence"}, CloudIDs: []string{allowedCloud}}
	saved, err := m.Put(t.Context(), in, view.Revision, "admin")
	if err != nil || len(saved.Tools) != 0 {
		t.Fatal(saved, err)
	}
	if _, err := stale.Invoke(t.Context(), &tools.Invocation{Principal: principal, Args: valid}); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("stale authority accepted", err)
	}
	view, err = m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || len(view.Tools) != 1 || view.Tools[0].Name != "getConfluenceContent" {
		t.Fatal(view, err)
	}
	if calls != 1 {
		t.Fatal("unexpected upstream dispatch", calls)
	}
}
