package mcpconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
	"github.com/woliveiras/weft/server/internal/tools"
)

func basicInput() Input {
	return Input{ID: "basic", Name: "Personal Atlassian", Service: "atlassian", Endpoint: atlassianFlatEndpoint, Enabled: true, CredentialType: "basic", Credential: &Credential{Username: "fixture@example.test", Value: "fixture-mcp-token"}}
}
func TestBasicCredentialValidation(t *testing.T) {
	if err := validate(basicInput()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Input){
		func(in *Input) { in.CredentialType = "custom" },
		func(in *Input) { in.Service = "github"; in.Endpoint = "https://example.com/mcp" },
		func(in *Input) { in.Credential.Username = "bad:email@example.test" },
		func(in *Input) { in.Credential.Username = "Name <fixture@example.test>" },
		func(in *Input) { in.Credential.Username = "" },
		func(in *Input) { in.Credential.Value = "token\r\nHeader: secret" },
		func(in *Input) { in.Credential.Clear = true },
		func(in *Input) { in.Credential.Value = "" },
		func(in *Input) { in.CredentialType = "bearer" },
		func(in *Input) { in.OAuth = &OAuthConfig{} },
	} {
		in := basicInput()
		change(&in)
		if !errors.Is(validate(in), ErrInvalid) {
			t.Fatal("invalid credential accepted")
		}
	}
}
func TestBasicWireSchemeAndRedaction(t *testing.T) {
	token := base64.StdEncoding.EncodeToString([]byte("fixture@example.test:fixture-mcp-token"))
	ctx := credentialContext(t.Context(), View{CredentialType: "basic"})
	if authorizationHeader(ctx, token) != "Basic "+token || authorizationHeader(t.Context(), token) != "Bearer "+token {
		t.Fatal("scheme confusion")
	}
	for _, value := range []string{token, "fixture@example.test", "fixture-mcp-token", "fixture@example.test:fixture-mcp-token"} {
		raw, _ := json.Marshal(map[string]string{"text": value})
		if !containsTransportCredential(ctx, raw, token) {
			t.Fatal("credential echo accepted")
		}
	}
	if containsTransportCredential(ctx, json.RawMessage(`{"text":"safe response"}`), token) {
		t.Fatal("safe response rejected")
	}
}
func TestBasicEncryptedPersistenceDiscoveryAndInvocation(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	in := basicInput()
	v, err := m.Put(ctx, in, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(v)
	setting, err := m.Store.GetSetting(ctx, prefix+in.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{in.Credential.Username, in.Credential.Value} {
		if strings.Contains(string(encoded), secret) || strings.Contains(string(setting.Value), secret) {
			t.Fatal("credential exposed")
		}
	}
	fixture := mcptest.NewAtlassianFlat()
	defer fixture.Close()
	m.Discover = func(ctx context.Context, _, token string) ([]Tool, error) {
		return DiscoverWithClient(ctx, fixture.URL+"?tools=all", token, fixture.Client())
	}
	m.Call = func(ctx context.Context, _, token, name string, args json.RawMessage) (json.RawMessage, error) {
		return CallWithClient(ctx, fixture.URL+"?tools=all", token, name, args, fixture.Client())
	}
	v, err = m.Refresh(ctx, in.ID, "admin")
	if err != nil || v.Status != "ready" {
		t.Fatal(v, err)
	}
	principal := &tools.Principal{GraphID: "g", NodeID: "n", Grants: []domain.ToolGrant{{Name: "mcp.basic.getJiraIssue"}}}
	gateway := tools.NewGateway(tools.NewRegistry(), nil, nil)
	gateway.ResolveTool = m.Resolve
	result, err := gateway.Invoke(ctx, principal, "mcp.basic.getJiraIssue", json.RawMessage(`{"cloudId":"fixture-site"}`))
	if err != nil || result == nil || result.IsError {
		t.Fatal(result, err)
	}
	fixture.EchoCredential.Store(true)
	result, err = gateway.Invoke(ctx, principal, "mcp.basic.getJiraIssue", json.RawMessage(`{}`))
	if err != nil || result == nil || !result.IsError || strings.Contains(result.Content, "fixture@example.test") || strings.Contains(result.Content, "fixture-mcp-token") {
		t.Fatal("unsafe echo result", result, err)
	}
	fixture.EchoCredential.Store(false)
	in.Credential = nil
	in.Name = "Renamed"
	v, err = m.Put(ctx, in, v.Revision, "admin")
	if err != nil || !v.CredentialConfigured {
		t.Fatal(v, err)
	}
	in.CredentialType = "bearer"
	if _, err = m.Put(ctx, in, v.Revision, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatal("credential reinterpreted", err)
	}
	in.Credential = &Credential{Value: "fixture-mcp-token"}
	v, err = m.Put(ctx, in, v.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Refresh(ctx, in.ID, "admin")
	if err != nil || v.Status != "ready" {
		t.Fatal("bearer replacement failed", v, err)
	}
	in.CredentialType = "basic"
	in.Credential = &Credential{Username: "fixture@example.test", Value: "wrong"}
	v, err = m.Put(ctx, in, v.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Refresh(ctx, in.ID, "admin")
	if err != nil || v.Status != "unavailable" || len(v.Tools) != 0 {
		t.Fatal("bad credential retained tools", v, err)
	}
	in.Credential = &Credential{Clear: true}
	v, err = m.Put(ctx, in, v.Revision, "admin")
	if err != nil || v.CredentialConfigured || len(v.Tools) != 0 {
		t.Fatal("clear retained authority", v, err)
	}
	if fixture.Calls.Load() != 2 {
		t.Fatal("unexpected dispatch")
	}
}
