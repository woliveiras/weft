package graph

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const strictDoc = `
apiVersion: weft.io/v1
kind: Graph
x-editor: {zoom: 1}
metadata: {name: strict, x-owner: me, colour: red}
spec:
  nodes:
    - id: a
      promt: typo
      prompt: ok
      x-color: blue
      config: {anything: {goes: here}}
      tools: [filesystem.read, {name: shell.exec, comands: [go]}]
      loop: {mode: event, x-nope: 1}
      position: {x: 1, y: 2, z: 3}
  channels:
    - {id: in, to: [a], x-note: fine, wen: "true"}
`

func TestUnknownFieldsReportsPaths(t *testing.T) {
	_, issues, err := Decode([]byte(strictDoc), true)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, i := range issues {
		got[i.Path] = i.Message
	}
	want := map[string]string{
		"metadata.colour":                `unknown field "colour"`,
		"spec.nodes[0].promt":            `unknown field "promt"`,
		"spec.nodes[0].tools[1].comands": `unknown field "comands"`,
		"spec.nodes[0].loop.x-nope":      "extension keys are only kept on the graph, metadata, nodes and channels",
		"spec.nodes[0].position.z":       `unknown field "z"`,
		"spec.channels[0].wen":           `unknown field "wen"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues:\n got %v\nwant %v", got, want)
	}
}

func TestDecodeJSONAndCleanDocuments(t *testing.T) {
	g, issues, err := Decode([]byte(`{"kind":"Graph","metadata":{"name":"j"},"spec":{"nodes":[{"id":"a","x-k":[1]}]}}`), false)
	if err != nil || len(issues) != 0 {
		t.Fatalf("clean JSON: %v %v", err, issues)
	}
	if g.Spec.Nodes[0].Extensions["x-k"] == nil {
		t.Fatalf("extension lost: %#v", g.Spec.Nodes[0])
	}
	// The shipped sample uses every common construct and must stay clean.
	if _, issues, err := Decode([]byte(sample), true); err != nil || len(issues) != 0 {
		t.Fatalf("sample: %v %v", err, issues)
	}
}

func TestDecodeSyntaxErrors(t *testing.T) {
	if _, _, err := Decode([]byte(`{"spec": [`), false); err == nil {
		t.Fatal("want JSON syntax error")
	}
	if _, _, err := Decode([]byte("spec: [\n"), true); err == nil {
		t.Fatal("want YAML syntax error")
	}
	if _, _, err := Decode([]byte("spec: {nodes: [{id: [x]}]}\n"), true); err == nil {
		t.Fatal("want type error")
	}
}

func TestUnknownFieldsIgnoresWrongShapes(t *testing.T) {
	// Shape errors belong to the typed decoder; the key check must not panic.
	if issues := UnknownFields(map[string]any{"spec": "nope", "metadata": []any{1}}); len(issues) != 0 {
		t.Fatalf("got %v", issues)
	}
	if issues := UnknownFields(nil); len(issues) != 0 {
		t.Fatalf("got %v", issues)
	}
}

// Shipped examples must load under the strict schema.
func TestExamplesHaveNoUnknownFields(t *testing.T) {
	files, err := filepath.Glob("../../../examples/graphs/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("examples: %v %v", files, err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, issues, err := Decode(b, true); err != nil || len(issues) != 0 {
			t.Errorf("%s: %v %v", f, err, issues)
		}
	}
}
