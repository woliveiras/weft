package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestFlatAtlassianEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{atlassianMCPEndpoint, atlassianFlatEndpoint} {
		in := input()
		in.Endpoint = endpoint
		if err := validate(in); err != nil {
			t.Fatal(err)
		}
	}
	for _, endpoint := range []string{atlassianMCPEndpoint + "?", atlassianMCPEndpoint + "?tools=all&token=secret", atlassianMCPEndpoint + "?tools=all&tools=all", atlassianMCPEndpoint + "?tools=%61ll", "https://other.example/v2/mcp?tools=all", "https://mcp.atlassian.com/v1/mcp?tools=all", "https://user:pass@mcp.atlassian.com/v2/mcp?tools=all"} {
		in := input()
		in.Endpoint = endpoint
		if !errors.Is(validate(in), ErrInvalid) {
			t.Fatal("unreviewed query accepted", endpoint)
		}
	}
	if canonicalMCPResource(atlassianFlatEndpoint) != atlassianMCPEndpoint {
		t.Fatal("flat resource not canonical")
	}
	if oauthResource(atlassianFlatEndpoint, oauthMetadata{Resource: atlassianFlatEndpoint}) != atlassianFlatEndpoint {
		t.Fatal("advertised resource not preserved")
	}
}
func TestFlatAtlassianDiscoveryDriftAndGateway(t *testing.T) {
	fixture := mcptest.NewAtlassianFlat()
	defer fixture.Close()
	m := testManager(t)
	in := Input{ID: "work", Name: "Work", Service: "atlassian", Endpoint: atlassianFlatEndpoint, Enabled: true, Credential: &Credential{Value: "fixture-mcp-token"}}
	if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	m.Discover = func(ctx context.Context, endpoint, token string) ([]Tool, error) {
		if endpoint != atlassianFlatEndpoint {
			t.Error("flat endpoint lost")
		}
		return DiscoverWithClient(ctx, fixture.URL+"?tools=all", token, fixture.Client())
	}
	m.Call = func(ctx context.Context, endpoint, token, name string, args json.RawMessage) (json.RawMessage, error) {
		return CallWithClient(ctx, fixture.URL+"?tools=all", token, name, args, fixture.Client())
	}
	view, err := m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || len(view.Tools) != 2 {
		t.Fatal(view, err)
	}
	stale, err := m.Resolve(t.Context(), "mcp.work.getConfluenceContent")
	if err != nil {
		t.Fatal(err)
	}
	principal := &tools.Principal{GraphID: "g", NodeID: "worker", Grants: []domain.ToolGrant{{Name: stale.Name()}}}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = m.Resolve
	if _, err = gateway.Invoke(t.Context(), principal, stale.Name(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	fixture.Drift.Store(true)
	if _, err = m.Refresh(t.Context(), in.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err = stale.Invoke(t.Context(), &tools.Invocation{Principal: principal, Args: json.RawMessage(`{}`)}); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("stale catalogue allowed", err)
	}
	if _, err = gateway.Invoke(t.Context(), principal, "mcp.work.listJiraProjects", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal("new tool inherited access", err)
	}
	if fixture.Calls.Load() != 1 {
		t.Fatal("unauthorized request dispatched")
	}
	fixture.Fail.Store(true)
	view, err = m.Refresh(t.Context(), in.ID, "admin")
	if err != nil || view.Status != "unavailable" || len(view.Tools) != 0 {
		t.Fatal("failed discovery retained authority", view, err)
	}
}

func TestAtlassianOperationCatalogue(t *testing.T) {
	for _, v := range []View{
		{Service: "atlassian", Endpoint: "https://custom.example/mcp"},
		{Service: "jira"}, {Service: "confluence"}, {Service: "bitbucket"},
		{Service: "generic", Endpoint: "https://MCP.ATLASSIAN.COM/v2/mcp"},
	} {
		v.Tools = []Tool{{Name: "executeRead"}, {Name: "getJiraIssue"}, {Name: "executeWrite"}, {Name: "executeDestructive"}}
		filtered := operationCatalogue(v)
		if len(filtered.Tools) != 1 || filtered.Tools[0].Name != "getJiraIssue" || len(v.Tools) != 4 {
			t.Fatal("unsafe or mutating filter", filtered)
		}
	}
	v := View{Service: "generic", Endpoint: "https://mcp.atlassian.com.attacker.example/mcp", Tools: []Tool{{Name: "executeRead"}}}
	if len(operationCatalogue(v).Tools) != 1 {
		t.Fatal("unrelated server filtered")
	}
}

func TestAtlassianLegacyDispatchersDeniedAtEveryBoundary(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	in := input()
	in.ID = "legacy"
	in.Service = "generic"
	in.Endpoint = atlassianMCPEndpoint
	if _, err := m.Put(ctx, in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	r, err := m.load(ctx, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Status = "ready"
	r.Tools = []Tool{{Name: "executeRead"}, {Name: "executeWrite"}, {Name: "executeDestructive"}, {Name: "getJiraIssue"}}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a persisted catalogue/grant created before dispatcher filtering.
	revision, err := m.Store.PutSetting(ctx, prefix+in.ID, raw, r.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	m.Call = func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("blocked operation reached transport")
		return nil, nil
	}
	got, err := m.Get(ctx, in.ID)
	if err != nil || len(got.Tools) != 1 {
		t.Fatal(got, err)
	}
	available, err := m.Available(ctx)
	if err != nil || len(available) != 1 {
		t.Fatal(available, err)
	}
	for _, name := range []string{"executeRead", "executeWrite", "executeDestructive"} {
		qualified := "mcp.legacy." + name
		if _, err := m.Resolve(ctx, qualified); !errors.Is(err, tools.ErrDenied) {
			t.Fatal("dispatcher resolved", err)
		}
		g := &domain.Graph{}
		g.Spec.Defaults.Tools = []domain.ToolGrant{{Name: qualified}}
		if !errors.Is(m.ValidateGrants(ctx, g), ErrInvalid) {
			t.Fatal("dispatcher grant accepted")
		}
		stale := &remoteTool{m: m, connection: in.ID, revision: revision, tool: Tool{Name: name}}
		principal := &tools.Principal{GraphID: "g", NodeID: "worker", Grants: g.Spec.Defaults.Tools}
		if _, err := stale.Invoke(ctx, &tools.Invocation{Principal: principal, Args: json.RawMessage(`{"tool":"deleteIssue"}`)}); !errors.Is(err, tools.ErrDenied) {
			t.Fatal("legacy dispatcher invoked", err)
		}
	}
}
