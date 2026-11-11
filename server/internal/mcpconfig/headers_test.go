package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestTypedMetadataHeaders(t *testing.T) {
	for _, tc := range []struct {
		mode, name, value string
		valid             bool
	}{
		{"bearer", "x-dbt-prod-environment-id", "123", true},
		{"bearer", "x-dbt-dev-environment-id", "45", true},
		{"bearer", "x-dbt-user-id", "67", true},
		{"bearer", "x-dbt-user-id", "0", false},
		{"bearer", "x-dbt-user-id", "12\r\nAuthorization: secret", false},
		{"bearer", "X-Dbt-User-Id", "123", false},
		{"bearer", "Authorization", "secret", false},
		{"bearer", "Host", "internal", false},
		{"bearer", "Mcp-Session-Id", "injected", false},
		{"bearer", "x-dbt-user-id", "https://example.com/123", false},
		{"langsmith", "langsmith-workspace-id", "11111111-1111-1111-1111-111111111111", true},
		{"langsmith", "langsmith-endpoint", "https://eu.api.smith.langchain.com", true},
		{"langsmith", "langsmith-endpoint", "https://internal.example", false},
		{"bearer", "langsmith-endpoint", "https://api.smith.langchain.com", false},
		{"langsmith", "x-dbt-user-id", "123", false},
		{"langsmith", "langsmith-api-key", "secret", false},
	} {
		in := input()
		in.Service = "generic"
		in.CredentialType = tc.mode
		in.MetadataHeaders = map[string]string{tc.name: tc.value}
		if (validate(in) == nil) != tc.valid {
			t.Fatal(tc.name, tc.value, "valid", tc.valid)
		}
	}
	req := httptest.NewRequest("POST", "https://fixture.example/mcp", nil)
	ctx := credentialContext(t.Context(), View{CredentialType: "langsmith", MetadataHeaders: map[string]string{"langsmith-workspace-id": "11111111-1111-1111-1111-111111111111"}})
	applyTransportHeaders(ctx, req, "secret-fixture")
	if req.Header.Get("Langsmith-Api-Key") != "secret-fixture" || req.Header.Get("Authorization") != "" || req.Header.Get("Langsmith-Workspace-Id") == "" {
		t.Fatal("wrong wire profile")
	}
}
func TestTypedHeaderPersistenceTransportAndIsolation(t *testing.T) {
	for _, mode := range []string{"bearer", "langsmith"} {
		t.Run(mode, func(t *testing.T) {
			in := input()
			in.Service = "generic"
			in.CredentialType = mode
			in.Credential = &Credential{Value: "fixture-mcp-token"}
			expected := map[string]string{"Authorization": "Bearer fixture-mcp-token", "x-dbt-prod-environment-id": "123"}
			in.MetadataHeaders = map[string]string{"x-dbt-prod-environment-id": "123"}
			if mode == "langsmith" {
				expected = map[string]string{"Langsmith-Api-Key": "fixture-mcp-token", "langsmith-workspace-id": "11111111-1111-1111-1111-111111111111"}
				in.MetadataHeaders = map[string]string{"langsmith-workspace-id": "11111111-1111-1111-1111-111111111111"}
			}
			fixture := mcptest.NewWithHeaders(expected)
			defer fixture.Close()
			m := testManager(t)
			ctx := t.Context()
			m.Discover = func(ctx context.Context, _, token string) ([]Tool, error) {
				return DiscoverWithClient(ctx, fixture.URL, token, fixture.Client())
			}
			m.Call = func(ctx context.Context, _, token, name string, args json.RawMessage) (json.RawMessage, error) {
				return CallWithClient(ctx, fixture.URL, token, name, args, fixture.Client())
			}
			v, err := m.Put(ctx, in, 0, "admin")
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(v)
			stored, err := m.Store.GetSetting(ctx, prefix+in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), in.Credential.Value) || strings.Contains(string(stored.Value), in.Credential.Value) {
				t.Fatal("plaintext key exposed")
			}
			v, err = m.Refresh(ctx, in.ID, "admin")
			if err != nil || v.Status != "ready" || len(v.Tools) != 2 {
				t.Fatal(v, err)
			}
			gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
			gateway.ResolveTool = m.Resolve
			name := "mcp." + in.ID + ".search"
			p := &tools.Principal{GraphID: "g", NodeID: "n", Grants: []domain.ToolGrant{{Name: name}}}
			result, err := gateway.Invoke(ctx, p, name, json.RawMessage(`{}`))
			if err != nil || result == nil || result.IsError {
				t.Fatal(result, err)
			}
			in.MetadataHeaders = nil
			in.Credential = nil
			if _, err = m.Put(ctx, in, v.Revision, "admin"); !errors.Is(err, ErrInvalid) {
				t.Fatal("metadata context changed without credential review", err)
			}
			in.Credential = &Credential{Value: "fixture-mcp-token"}
			v, err = m.Put(ctx, in, v.Revision, "admin")
			if err != nil {
				t.Fatal(err)
			}
			v, err = m.Refresh(ctx, in.ID, "admin")
			if err != nil || v.Status != "unavailable" || len(v.Tools) != 0 {
				t.Fatal("old headers retained", v, err)
			}
		})
	}
}

func TestMetadataChangeRequiresNewOAuthAuthorization(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	in := input()
	in.Service = "generic"
	in.Credential = nil
	in.OAuth = &OAuthConfig{Issuer: "https://issuer.example", ClientID: "client", Scopes: []string{"read"}}
	in.MetadataHeaders = map[string]string{"x-dbt-prod-environment-id": "123"}
	if _, err := m.Put(ctx, in, 0, "admin"); err != nil {
		t.Fatal(err)
	}
	r, err := m.load(ctx, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Sealed, err = m.Box.Seal([]byte("fixture-token"), prefix+in.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.OAuth.Status = "authorized"
	r.Status = "ready"
	r.Tools = []Tool{{Name: "search"}}
	v, err := m.save(ctx, r, r.Revision, "fixture", "mcp.connection.changed")
	if err != nil {
		t.Fatal(err)
	}
	in.MetadataHeaders = map[string]string{"x-dbt-prod-environment-id": "456"}
	v, err = m.Put(ctx, in, v.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.OAuth.Status != "authorization_required" || v.CredentialConfigured || len(v.Tools) != 0 {
		t.Fatal("changed context retained OAuth authority", v)
	}
}
