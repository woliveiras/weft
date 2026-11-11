package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"testing"
)

func TestEnvironmentToolsAuthorizationAndScope(t *testing.T) {
	calls := 0
	handler := func(_ context.Context, inv *Invocation) (*Result, error) {
		calls++
		if inv.Principal.GraphID != "own" {
			t.Fatal(inv.Principal)
		}
		return &Result{Content: "ok"}, nil
	}
	reg := NewRegistry()
	reg.MustRegister(EnvironmentTools(handler, handler)...)
	gate := NewGateway(reg, nil, nil)
	p := &Principal{GraphID: "own", NodeID: "agent"}
	for _, name := range []string{"environment.status", "environment.propose"} {
		if _, err := gate.Invoke(context.Background(), p, name, json.RawMessage(`{}`)); !errors.Is(err, ErrDenied) {
			t.Fatal(err)
		}
		p.Grants = []domain.ToolGrant{{Name: name}}
		if result, err := gate.Invoke(context.Background(), p, name, json.RawMessage(`{}`)); err != nil || result.IsError {
			t.Fatal(result, err)
		}
		p.Grants = nil
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	p.NodeID = ""
	p.Grants = []domain.ToolGrant{{Name: "environment.status"}}
	if _, err := gate.Invoke(context.Background(), p, "environment.status", json.RawMessage(`{}`)); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestEnvironmentPreparationFollowsAuthorization(t *testing.T) {
	calls := 0
	reg := NewRegistry()
	reg.MustRegister(EnvironmentTools(func(context.Context, *Invocation) (*Result, error) {
		t.Fatal("failed preparation invoked tool")
		return nil, nil
	}, nil)...)
	gate := NewGateway(reg, nil, nil)
	p := &Principal{GraphID: "g", NodeID: "n", PrepareTool: func(context.Context, string) error { calls++; return errors.New("environment not ready") }}
	if _, err := gate.Invoke(context.Background(), p, "environment.status", json.RawMessage(`{}`)); !errors.Is(err, ErrDenied) || calls != 0 {
		t.Fatal(err, calls)
	}
	p.Grants = []domain.ToolGrant{{Name: "environment.status"}}
	result, err := gate.Invoke(context.Background(), p, "environment.status", json.RawMessage(`{}`))
	if err != nil || !result.IsError || calls != 1 {
		t.Fatal(result, err, calls)
	}
}
