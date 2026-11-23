package api_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig/mcptest"
)

func TestMCPOAuthRecoveryAPI(t *testing.T) {
	srv, a := setupApp(t, func(c *config.Config) { c.Server.PublicURL = "http://localhost:8080" })
	provider := mcptest.NewOAuth("")
	defer provider.Close()
	provider.FailRegistration.Store(true)
	a.MCP.OAuthClient = provider.Client()
	in := mcpconfig.Input{ID: "recovery", Name: "Recovery", Service: "generic", Endpoint: provider.URL + "/mcp", Enabled: true, OAuth: &mcpconfig.OAuthConfig{Issuer: provider.URL, RegistrationMode: "dcr", Scopes: []string{"read"}}}
	raw, _ := json.Marshal(in)
	if code := call(t, srv, "POST", "/api/v1/mcp/connections", adminTok, "application/json", string(raw), nil); code != 201 {
		t.Fatal(code)
	}
	start := "/api/v1/mcp/connections/recovery/oauth/start?registrationMode=dcr"
	if code := call(t, srv, "POST", start, adminTok, "", "", nil); code != 502 {
		t.Fatal(code)
	}
	var old mcpconfig.View
	if code := call(t, srv, "GET", "/api/v1/mcp/connections/recovery", adminTok, "", "", &old); code != 200 {
		t.Fatal(code)
	}
	path := "/api/v1/mcp/connections/recovery/oauth/registration/recover"
	valid := fmt.Sprintf(`{"expectRevision":%d,"acknowledgeDuplicateClientRisk":true}`, old.Revision)
	for _, tc := range []struct {
		name, token, body string
		want              int
	}{
		{"viewer", viewerTok, valid, 403}, {"operator", operatorTok, valid, 403},
		{"missing-ack", adminTok, fmt.Sprintf(`{"expectRevision":%d}`, old.Revision), 422},
		{"false-ack", adminTok, fmt.Sprintf(`{"expectRevision":%d,"acknowledgeDuplicateClientRisk":false}`, old.Revision), 422},
		{"stale", adminTok, `{"expectRevision":1,"acknowledgeDuplicateClientRisk":true}`, 409},
		{"unknown-field", adminTok, `{"expectRevision":3,"acknowledgeDuplicateClientRisk":true,"bypass":true}`, 400},
		{"extra-json", adminTok, valid + `{}`, 400},
		{"oversized-body", adminTok, valid + strings.Repeat(" ", 1025) + `{}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := call(t, srv, "POST", path, tc.token, "application/json", tc.body, nil); code != tc.want {
				t.Fatal(code)
			}
		})
	}
	unchanged, err := a.MCP.Get(t.Context(), in.ID)
	if err != nil || unchanged.Revision != old.Revision || provider.Registrations.Load() != 1 {
		t.Fatal("invalid requests mutated state", err)
	}
	var recovered mcpconfig.View
	if code := call(t, srv, "POST", path, adminTok, "application/json", valid, &recovered); code != 200 {
		t.Fatal(code)
	}
	if recovered.OAuth.RegistrationStatus != "" || len(recovered.OAuth.RegistrationHistory) != 1 || provider.Registrations.Load() != 1 {
		t.Fatal("unexpected recovery", recovered.OAuth)
	}
	if code := call(t, srv, "POST", path, adminTok, "application/json", valid, nil); code != 409 {
		t.Fatal("replay", code)
	}
	// Fresh reviewed start has its own claim and preserves archived uncertainty.
	provider.FailRegistration.Store(false)
	if code := call(t, srv, "POST", start, adminTok, "", "", nil); code != 200 {
		t.Fatal(code)
	}
	final, err := a.MCP.Get(t.Context(), in.ID)
	if err != nil || len(final.OAuth.RegistrationHistory) != 1 || final.OAuth.RegistrationStatus != "ready" || provider.Registrations.Load() != 2 {
		t.Fatal("new attempt lost evidence", err)
	}
	if code := call(t, srv, "POST", path, adminTok, "application/json", fmt.Sprintf(`{"expectRevision":%d,"acknowledgeDuplicateClientRisk":true}`, final.Revision), nil); code != 422 {
		t.Fatal("ready reset", code)
	}
}
