package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const extDoc = `
apiVersion: weft.io/v1
kind: Graph
x-editor: {theme: dark}
metadata:
  name: ext
  x-owner: platform
spec:
  nodes:
    - id: a
      prompt: hi
      x-color: "#f00"
      x-tags: [one, two]
  channels:
    - id: in
      to: [a]
      x-note: {weight: 2, nested: {ok: true}}
`

func TestExtensionsRoundTripYAMLAndJSON(t *testing.T) {
	var g Graph
	if err := yaml.Unmarshal([]byte(extDoc), &g); err != nil {
		t.Fatal(err)
	}
	if g.Extensions["x-editor"] == nil || g.Metadata.Extensions["x-owner"] != "platform" {
		t.Fatalf("graph/metadata extensions: %#v %#v", g.Extensions, g.Metadata.Extensions)
	}
	if g.Spec.Nodes[0].Extensions["x-color"] != "#f00" || g.Spec.Nodes[0].Prompt != "hi" {
		t.Fatalf("node: %#v", g.Spec.Nodes[0])
	}
	if g.Spec.Channels[0].Extensions["x-note"] == nil || g.Spec.Channels[0].To[0] != "a" {
		t.Fatalf("channel: %#v", g.Spec.Channels[0])
	}

	// JSON is how documents are stored: it must carry the extensions.
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var back Graph
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	// YAML yields ints where JSON yields float64s, so compare encodings.
	b2, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(b2) {
		t.Fatalf("JSON round trip changed the document:\n%s\n%s", b, b2)
	}

	// YAML export keeps them too.
	y, err := yaml.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"x-editor:", "x-owner: platform", "x-color: '#f00'", "x-note:"} {
		if !strings.Contains(string(y), want) {
			t.Errorf("YAML export lacks %q:\n%s", want, y)
		}
	}
	var again Graph
	if err := yaml.Unmarshal(y, &again); err != nil {
		t.Fatal(err)
	}
	if b3, _ := json.Marshal(again); string(b3) != string(b) {
		t.Fatalf("YAML round trip changed the document:\n%s\n%s", b, b3)
	}
}

func TestNoExtensionsEncodesAsBefore(t *testing.T) {
	n := Node{ID: "a", Prompt: "p"}
	got, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(nodePlain(n))
	if string(got) != string(want) {
		t.Fatalf("got %s want %s", got, want)
	}
	if strings.Contains(string(got), "Extensions") {
		t.Fatalf("extensions field leaked: %s", got)
	}
}

func TestExtensionKeyRules(t *testing.T) {
	for k, want := range map[string]bool{"x-a": true, "x-": false, "x": false, "xa": false, "X-a": false} {
		if IsExtensionKey(k) != want {
			t.Errorf("IsExtensionKey(%q) = %v", k, !want)
		}
	}
	// "x-" alone is not an extension: it is just an unknown key, and dropped.
	var n Node
	if err := json.Unmarshal([]byte(`{"id":"a","x-":1}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Extensions != nil {
		t.Fatalf("got %#v", n.Extensions)
	}
}

func TestExtensionErrors(t *testing.T) {
	// A YAML mapping with a non-string key cannot be stored as JSON.
	var n Node
	err := yaml.Unmarshal([]byte("id: a\nx-bad: {1: one}\n"), &n)
	if err == nil || !strings.Contains(err.Error(), `extension "x-bad"`) {
		t.Fatalf("want non-string key error, got %v", err)
	}
	// Syntax errors in the plain fields still surface.
	if err := json.Unmarshal([]byte(`{"id": 3}`), &n); err == nil {
		t.Fatal("want type error")
	}
	// An unencodable extension value is reported with its key.
	_, err = json.Marshal(Channel{ID: "c", Extensions: Extensions{"x-f": func() {}}})
	if err == nil || !strings.Contains(err.Error(), "x-f") {
		t.Fatalf("want encode error, got %v", err)
	}
}
