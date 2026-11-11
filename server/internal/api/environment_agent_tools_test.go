//go:build !windows

package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/tools"
)

func TestEnvironmentAgentToolRegistrationAndScope(t *testing.T) {
	srv, a := setupApp(t, nil)
	var catalog []tools.ToolInfo
	if code := call(t, srv, "GET", "/api/v1/tools", viewerTok, "", "", &catalog); code != 200 {
		t.Fatal(code)
	}
	for _, name := range []string{"environment.status", "environment.propose"} {
		found := false
		for _, entry := range catalog {
			if entry.Name == name {
				found = true
				if entry.Capability != name {
					t.Fatal(entry)
				}
			}
		}
		if !found {
			t.Fatal("tool not registered", name)
		}
	}
	doc := "apiVersion: weft.io/v1\nkind: Graph\nmetadata: {name: agent-tools}\nspec:\n  nodes: [{id: worker, runtime: {kind: echo}, isolation: {mode: docker}}]\n"
	if code := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", doc, nil); code != 201 {
		t.Fatal(code)
	}
	principal := &tools.Principal{GraphID: "agent-tools", NodeID: "worker"}
	if _, err := a.Gateway.Invoke(context.Background(), principal, "environment.status", json.RawMessage(`{}`)); !errors.Is(err, tools.ErrDenied) {
		t.Fatal(err)
	}
	principal.Grants = []domain.ToolGrant{{Name: "environment.status"}}
	result, err := a.Gateway.Invoke(context.Background(), principal, "environment.status", json.RawMessage(`{}`))
	if err != nil || result.IsError || len(result.Data) == 0 {
		t.Fatal(result, err)
	}
	result, err = a.Gateway.Invoke(context.Background(), principal, "environment.status", json.RawMessage(`{"graphId":"other"}`))
	if err != nil || !result.IsError {
		t.Fatal(result, err)
	}
}
