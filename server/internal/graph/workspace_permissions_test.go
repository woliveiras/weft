package graph

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"testing"
)

func TestReadOnlyWorkspaceRuntimeValidation(t *testing.T) {
	for _, kind := range []domain.RuntimeKind{domain.RuntimeNative, domain.RuntimeExternal} {
		g := &domain.Graph{Metadata: domain.GraphMetadata{Name: "reader"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader", Prompt: "inspect", Runtime: domain.RuntimeRef{Kind: kind, Endpoint: "http://localhost:1234"}, Workspace: domain.NodeWorkspace{ReadOnly: true}}}}}
		r := Validate(g, nil)
		if r.Valid != (kind == domain.RuntimeNative) {
			t.Fatalf("%s: %+v", kind, r)
		}
	}
}
