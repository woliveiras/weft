//go:build !windows

package api_test

import (
	"context"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
)

func TestNetworkPolicyAPIAndEnforcement(t *testing.T) {
	srv, app := setupApp(t, func(c *config.Config) { c.Isolation.Docker.EnvironmentPolicy.DisableAgentProposals = true })
	var initial environmentpolicy.Preview
	if code := call(t, srv, "GET", "/api/v1/network-policy", viewerTok, "", "", &initial); code != 200 || initial.Revision != 0 || initial.Source != "server" {
		t.Fatal(code, initial)
	}
	doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: policy-usage}\nspec:\n  environment:\n    image: alpine\n    network: {setup: bridge, run: none}\n  nodes: [{id: worker, runtime: {kind: echo}}]\n"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatal(code)
	}
	input := `{"revision":0,"network":{"setupNetworks":["none","egress"],"runNetworks":["none","egress"],"egressHosts":["example.com"]}}`
	for _, token := range []string{viewerTok, operatorTok} {
		for _, method := range []string{"PUT", "POST"} {
			path := "/api/v1/network-policy"
			if method == "POST" {
				path += "/preview"
			}
			if code := call(t, srv, method, path, token, "application/json", input, nil); code != 403 {
				t.Fatal("role bypass", method, code)
			}
		}
	}
	var preview environmentpolicy.Preview
	if code := call(t, srv, "POST", "/api/v1/network-policy/preview", adminTok, "application/json", input, &preview); code != 200 || len(preview.Usage) != 1 || !preview.Usage[0].Blocked {
		t.Fatal(code, preview)
	}
	var unchanged environmentpolicy.SettingsView
	call(t, srv, "GET", "/api/v1/network-policy", viewerTok, "", "", &unchanged)
	if unchanged.Revision != 0 {
		t.Fatal("preview wrote settings")
	}
	var saved environmentpolicy.SettingsView
	if code := call(t, srv, "PUT", "/api/v1/network-policy", adminTok, "application/json", input, &saved); code != 200 || saved.Revision != 1 || saved.UpdatedBy == "" {
		t.Fatal(code, saved)
	}
	if code := call(t, srv, "PUT", "/api/v1/network-policy", adminTok, "application/json", input, nil); code != 409 {
		t.Fatal("stale write", code)
	}
	if code := call(t, srv, "POST", "/api/v1/network-policy/preview", adminTok, "application/json", input, nil); code != 409 {
		t.Fatal("stale preview", code)
	}
	// A graph accepted before the edit cannot start a build with the old policy.
	if code := call(t, srv, "POST", "/api/v1/graphs/policy-usage/environment/build", adminTok, "application/json", `{}`, nil); code != 400 {
		t.Fatal("old graph build not blocked", code)
	}
	policy, err := app.Environments.CurrentPolicy(context.Background())
	if err != nil || !policy.DisableAgentProposals {
		t.Fatal("startup restrictions lost", policy, err)
	}
	if err := policy.Check(&domain.EnvironmentSpec{Image: "alpine", Network: domain.EnvironmentNetwork{Setup: "none", Run: "egress", AllowedHosts: []string{"example.com"}}}); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check(&domain.EnvironmentSpec{Image: "alpine", Network: domain.EnvironmentNetwork{Setup: "none", Run: "egress", AllowedHosts: []string{"denied.example"}}}); err == nil {
		t.Fatal("new ceiling bypassed")
	}
}

func TestNetworkPolicyAPIInvalidInputs(t *testing.T) {
	srv, _ := setupApp(t, nil)
	for _, body := range []string{`{}`, `{"revision":-1}`, `{"revision":0,"network":{"setupNetworks":[],"runNetworks":["none"]}}`, `{"revision":0,"network":{"setupNetworks":["host"],"runNetworks":["none"]}}`, `{"revision":0,"network":{"setupNetworks":["none"],"runNetworks":["none"],"egressHosts":["*.example.com"]}}`, `{"revision":0,"network":{"setupNetworks":["none"],"runNetworks":["none"],"disableSetup":false}}`, `{} {}`} {
		for _, method := range []string{"PUT", "POST"} {
			path := "/api/v1/network-policy"
			if method == "POST" {
				path += "/preview"
			}
			if code := call(t, srv, method, path, adminTok, "application/json", body, nil); code != 400 {
				t.Fatal(code, body)
			}
		}
	}
	var v environmentpolicy.SettingsView
	call(t, srv, "GET", "/api/v1/network-policy", viewerTok, "", "", &v)
	if v.Revision != 0 {
		t.Fatal("invalid input mutated policy")
	}
}
