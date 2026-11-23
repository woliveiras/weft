package api

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/mcpconfig"
)

func TestConfigMCPAwaitingAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name string
		view mcpconfig.View
		want bool
	}{
		{"new OAuth", mcpconfig.View{OAuth: &mcpconfig.OAuthView{Status: "authorization_required"}}, true},
		{"bearer", mcpconfig.View{}, false},
		{"authorized", mcpconfig.View{CredentialConfigured: true, OAuth: &mcpconfig.OAuthView{Status: "authorized"}}, false},
		{"missing authorized credential", mcpconfig.View{OAuth: &mcpconfig.OAuthView{Status: "authorized"}}, false},
		{"unexpected credential", mcpconfig.View{CredentialConfigured: true, OAuth: &mcpconfig.OAuthView{Status: "authorization_required"}}, false},
		{"unknown state", mcpconfig.View{OAuth: &mcpconfig.OAuthView{Status: "unknown"}}, false},
		{"busy", mcpconfig.View{OAuth: &mcpconfig.OAuthView{Status: "exchanging"}}, false},
		{"revoked", mcpconfig.View{OAuth: &mcpconfig.OAuthView{Status: "revoked"}}, false},
		{"reauthorization required", mcpconfig.View{OAuth: &mcpconfig.OAuthView{Status: "reauthorization_required"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := configMCPAwaitingAuthorization(tc.view); got != tc.want {
				t.Fatalf("awaiting authorization = %v, want %v", got, tc.want)
			}
		})
	}
}
