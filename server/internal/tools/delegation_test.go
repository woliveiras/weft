package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestDelegationRequiresExactGrantAndActiveParent(t *testing.T) {
	for _, grants := range [][]domain.ToolGrant{nil, {{Name: "*"}}, {{Name: "delegation.*"}}, {{Name: "delegation.request"}}} {
		called := false
		p := &Principal{Grants: grants, Delegate: func(context.Context, json.RawMessage) (*Result, error) {
			called = true
			return &Result{Content: "child result"}, nil
		}}
		g := NewGateway(Builtin(BuiltinConfig{}), nil, nil)
		_, err := g.Invoke(context.Background(), p, "delegation.request", json.RawMessage(`{"template":"analysis","objective":"Review"}`))
		exact := len(grants) > 0 && grants[0].Name == "delegation.request"
		if called != exact || (exact && err != nil) || (!exact && !errors.Is(err, ErrDenied)) {
			t.Fatalf("grants %v called=%v err=%v", grants, called, err)
		}
	}
	p := &Principal{Grants: []domain.ToolGrant{{Name: "delegation.request"}}}
	_, err := NewGateway(Builtin(BuiltinConfig{}), nil, nil).Invoke(context.Background(), p, "delegation.request", nil)
	if !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
