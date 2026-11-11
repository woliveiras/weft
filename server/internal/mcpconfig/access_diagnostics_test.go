package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestAccessGuidanceAllowlist(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		err := fmt.Errorf("wrapped: %w", discoveryError(fmt.Sprintf("MCP HTTP status %d", status)))
		if got := accessGuidance(err); !strings.Contains(got, fmt.Sprint(status)) {
			t.Fatal(got)
		}
	}
	for _, err := range []error{nil, errors.New("MCP HTTP status 403 secret"), discoveryError("MCP HTTP status 403 secret"), discoveryError("MCP HTTP status 500")} {
		if got := accessGuidance(err); got != "" {
			t.Fatal("untrusted diagnostic", got)
		}
	}
	if !strings.Contains(accessGuidance(discoveryError("MCP HTTP status 403")), "does not identify the cause") {
		t.Fatal("403 must not imply a verified scope/SSO diagnosis")
	}
}

func TestRemoteAccessDiagnosticsThroughGatewayAndDiscovery(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			peer := mcptest.NewAccessFailure(status, "tools/call")
			defer peer.Close()
			m := testManager(t)
			m.Discover = func(ctx context.Context, _, token string) ([]Tool, error) {
				return DiscoverWithClient(ctx, peer.URL, token, peer.Client())
			}
			calls := 0
			m.Call = func(ctx context.Context, _, token, name string, args json.RawMessage) (json.RawMessage, error) {
				calls++
				return CallWithClient(ctx, peer.URL, token, name, args, peer.Client())
			}
			in := input()
			in.Credential = &Credential{Value: "fixture-mcp-token"}
			if _, err := m.Put(t.Context(), in, 0, "admin"); err != nil {
				t.Fatal(err)
			}
			view, err := m.Refresh(t.Context(), in.ID, "admin")
			if err != nil || view.Status != "ready" {
				t.Fatal(view, err)
			}
			gw := tools.NewGateway(tools.NewRegistry(), nil, nil)
			gw.ResolveTool = m.Resolve
			p := &tools.Principal{GraphID: "test", NodeID: "reader", Grants: []domain.ToolGrant{{Name: "mcp.team.search"}}}
			result, err := gw.Invoke(t.Context(), p, "mcp.team.search", json.RawMessage(`{"query":"issue"}`))
			if err != nil || result == nil || !result.IsError || !strings.Contains(result.Content, fmt.Sprint(status)) || !strings.Contains(result.Content, "no automatic retry") || calls != 1 {
				t.Fatal(err, calls)
			}
			if strings.Contains(result.Content, "fixture-mcp-token") || strings.Contains(result.Content, "private-detail") {
				t.Fatal("upstream diagnostic leaked")
			}
			denied := mcptest.NewAccessFailure(status, "tools/list")
			defer denied.Close()
			m.Discover = func(ctx context.Context, _, token string) ([]Tool, error) {
				return DiscoverWithClient(ctx, denied.URL, token, denied.Client())
			}
			view, err = m.Refresh(t.Context(), in.ID, "admin")
			if err != nil || view.Status != "unavailable" || len(view.Tools) != 0 || !strings.Contains(view.Error, fmt.Sprint(status)) {
				t.Fatal(view, err)
			}
			stored, err := m.Get(t.Context(), in.ID)
			if err != nil || stored.Error != view.Error {
				t.Fatal(stored, err)
			}
			raw, err := json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "fixture-mcp-token") || strings.Contains(string(raw), "private-detail") {
				t.Fatal("stored view leaked upstream content")
			}
			if _, err = gw.Invoke(t.Context(), p, "mcp.team.search", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrDenied) {
				t.Fatal("unavailable connection accepted", err)
			}
			if calls != 1 {
				t.Fatal("failed call replayed")
			}
		})
	}
}

func TestForbiddenDiscoveryDoesNotRevokeOAuthOrOtherConnections(t *testing.T) {
	m, _ := oauthSetup(t)
	authorizeFixture(t, m)
	before, err := m.Get(t.Context(), "team")
	if err != nil {
		t.Fatal(err)
	}
	other := input()
	other.ID = "other"
	other.Credential = &Credential{Value: "other-fixture-token"}
	original, err := m.Put(t.Context(), other, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	m.Discover = func(context.Context, string, string) ([]Tool, error) {
		return nil, discoveryError("MCP HTTP status 403")
	}
	after, err := m.Refresh(t.Context(), "team", "admin")
	if err != nil || after.OAuth.Status != before.OAuth.Status || !after.CredentialConfigured || len(after.Tools) != 0 || !strings.Contains(after.Error, "403") {
		t.Fatal(after, err)
	}
	unchanged, err := m.Get(t.Context(), "other")
	if err != nil || unchanged.Revision != original.Revision || !unchanged.CredentialConfigured {
		t.Fatal("other connection affected", unchanged, err)
	}
}
