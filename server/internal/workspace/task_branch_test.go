package workspace

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"testing"
)

func TestReplicaBranchSeparatesTaskRootNamespace(t *testing.T) {
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "fixture"}}
	if got := replicaBranch(g, "writer", 1); got != "weft/fixture/writer-1" {
		t.Fatal(got)
	}
	g.Metadata.Labels = map[string]string{"weft.task": "tsk_fixture"}
	if got := replicaBranch(g, "writer", 1); got != "weft-replicas/fixture/writer-1" {
		t.Fatal(got)
	}
}
