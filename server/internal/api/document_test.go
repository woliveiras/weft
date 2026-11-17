package api_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
)

// Graph documents are stored as written: defaults are applied when graphs
// run, not copied into each node, so editing spec.defaults reaches every
// node that does not set its own value.
func TestGraphDocumentsAreStoredAsWritten(t *testing.T) {
	srv, a := setupApp(t, nil)
	const written = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: Doc Demo}
spec:
  defaults:
    runtime: {kind: echo}
    model: {provider: mock, name: first}
  nodes:
    - {name: Planner}
    - {id: coder, model: {name: own}}
  channels:
    - {from: planner, to: [coder]}
`
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", written, nil); c != 201 {
		t.Fatalf("create: %d", c)
	}
	var got struct {
		Graph struct {
			Metadata struct{ ID string }
			Spec     struct {
				Nodes []struct {
					ID    string
					Model *struct{ Name string }
					Loop  *struct{ Mode string }
				}
				Channels []struct{ ID string }
			}
		}
		Record struct{ Version int }
	}
	if c := call(t, srv, "GET", "/api/v1/graphs/doc-demo", viewerTok, "", "", &got); c != 200 {
		t.Fatalf("get: %d", c)
	}
	nodes := got.Graph.Spec.Nodes
	// Identity derived from names is kept; defaults are not baked in.
	if got.Graph.Metadata.ID != "doc-demo" || nodes[0].ID != "planner" || got.Graph.Spec.Channels[0].ID != "planner-to-coder" {
		t.Fatalf("identity: %+v", got.Graph)
	}
	if nodes[0].Model != nil && nodes[0].Model.Name != "" || nodes[0].Loop != nil && nodes[0].Loop.Mode != "" {
		t.Fatalf("defaults were copied into the node: model=%+v loop=%+v", *nodes[0].Model, *nodes[0].Loop)
	}
	if nodes[1].Model == nil || nodes[1].Model.Name != "own" {
		t.Fatalf("explicit values are kept: %+v", nodes[1])
	}

	// Execution sees the defaults applied.
	eff, _, err := a.Engine.GetGraph(context.Background(), "doc-demo", 0)
	if err != nil || eff.Spec.Nodes[0].Model.Name != "first" || eff.Spec.Nodes[1].Model.Name != "own" || eff.Spec.Nodes[1].Loop.Mode != "event" {
		t.Fatalf("effective: %+v %v", eff.Spec.Nodes, err)
	}

	// Changing the default reaches the node that did not override it.
	changed := `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: Doc Demo}
spec:
  defaults:
    runtime: {kind: echo}
    model: {provider: mock, name: second}
  nodes:
    - {name: Planner}
    - {id: coder, model: {name: own}}
  channels:
    - {from: planner, to: [coder]}
`
	if c := call(t, srv, "PUT", "/api/v1/graphs/doc-demo", adminTok, "application/yaml", changed, nil); c != 200 {
		t.Fatalf("update: %d", c)
	}
	eff, _, _ = a.Engine.GetGraph(context.Background(), "doc-demo", 0)
	if eff.Spec.Nodes[0].Model.Name != "second" || eff.Spec.Nodes[1].Model.Name != "own" {
		t.Fatalf("after changing defaults: %+v", eff.Spec.Nodes)
	}
}

// Bootstrap graph files are stored as written and not re-saved when they did
// not change, so restarts do not add versions.
func TestBootstrapStoresFilesAsWrittenOnce(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "boot.yaml")
	if err := os.WriteFile(file, []byte(`
apiVersion: weft.io/v1
kind: Graph
metadata: {name: Boot}
spec:
  defaults: {runtime: {kind: echo}, model: {name: shared}}
  nodes: [{name: Only One}]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, a := setupApp(t, func(c *config.Config) { c.Bootstrap.Graphs = []string{file} })
	for i := 0; i < 3; i++ {
		if err := a.Bootstrap(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var versions []map[string]any
	call(t, srv, "GET", "/api/v1/graphs/boot/versions", viewerTok, "", "", &versions)
	if len(versions) != 1 {
		t.Fatalf("bootstrap must not add versions for an unchanged file: %d", len(versions))
	}
	doc, _, err := a.Engine.GetGraphDocument(context.Background(), "boot", 0)
	if err != nil || doc.Spec.Nodes[0].ID != "only-one" || doc.Spec.Nodes[0].Model.Name != "" {
		t.Fatalf("stored document: %+v %v", doc.Spec.Nodes, err)
	}
}
