package installation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKinds(t *testing.T) {
	for _, kind := range Kinds {
		t.Run(kind, func(t *testing.T) {
			ds, err := Parse([]string{"apiVersion: weft.io/v1\nkind: " + kind + "\nmetadata: {name: fixture}\nspec: {}"})
			if err != nil || len(ds) != 1 {
				t.Fatalf("%v", err)
			}
		})
	}
}
func TestParseRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"secret: [synthetic-secret", "apiVersion: v2\nkind: Graph", "apiVersion: weft.io/v1\nkind: Unknown", "a: 1\na: 2", "a: &x 1\nb: *x", "apiVersion: weft.io/v1\nkind: ModelProvider\nmetadata: {name: inline}\nspec: {apiKey: {value: synthetic-secret}}"} {
		if _, err := Parse([]string{raw}); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatalf("unsafe or accepted: %v", err)
		}
	}
}
func TestResolverConfinement(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(outside, []byte("synthetic-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key"), []byte("synthetic-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "large"), []byte(strings.Repeat("x", MaxSecretBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	r := Resolver{Dir: dir, DeniedEnv: []string{"WEFT_SECRET_CUSTOM_KEY"}}
	t.Setenv("WEFT_SECRET_FIXTURE", "synthetic-secret")
	for _, ref := range []SecretRef{{Env: "WEFT_ADMIN_TOKEN"}, {Env: "POSTGRES_PASSWORD"}, {Env: "WEFT_RUNTIME_SECRET"}, {Env: "WEFT_SECRET_KEY"}, {Env: "WEFT_SECRET_CUSTOM_KEY"}, {Env: "WEFT_SECRET_MISSING"}, {File: outside}, {File: "../key"}, {File: "a/../key"}, {File: "escape"}, {File: "large"}, {File: "."}, {Env: "WEFT_SECRET_FIXTURE", File: "key"}} {
		if value, err := r.Resolve(ref); err == nil || value != "" || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatalf("unsafe ref %#v: %q %v", ref, value, err)
		}
	}
	for _, ref := range []SecretRef{{Env: "WEFT_SECRET_FIXTURE"}, {File: "key"}} {
		if v, e := r.Resolve(ref); e != nil || v != "synthetic-secret" {
			t.Fatalf("%q %v", v, e)
		}
	}
}

func TestGraphExtensionsAndDuplicateDocuments(t *testing.T) {
	raw := "apiVersion: weft.io/v1\nkind: Graph\nx-owner: platform\nmetadata: {name: Demo Graph, x-team: tooling}\nspec: {nodes: [{id: worker}]}"
	docs, err := Parse([]string{raw})
	if err != nil {
		t.Fatal(err)
	}
	if docs[0].Key() != "Graph/demo-graph" || docs[0].Extensions["x-owner"] != "platform" {
		t.Fatal(docs)
	}
	exported, err := Export(docs)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse([]string{exported})
	if err != nil || again[0].Extensions["x-owner"] != "platform" {
		t.Fatal(err)
	}
	if _, err = Parse([]string{raw, raw}); err == nil {
		t.Fatal("duplicate graph accepted")
	}
}

func TestWorkflowMappingKindOrdersAfterConnectionBeforeTrigger(t *testing.T) {
	docs := []Document{New("IssueTrigger", "issues", map[string]any{}), New("IssueWorkflowMapping", "review", map[string]any{}), New("IssueConnection", "native", map[string]any{})}
	Sort(docs)
	for i, kind := range []string{"IssueConnection", "IssueWorkflowMapping", "IssueTrigger"} {
		if docs[i].Kind != kind {
			t.Fatal(docs)
		}
	}
}

func TestReviewRouteKindOrdersAfterSavedSCMAndGraph(t *testing.T) {
	docs := []Document{New("PullRequestReviewRoute", "review", map[string]any{}), New("Graph", "review", map[string]any{}), New("SCMConnection", "team", map[string]any{})}
	Sort(docs)
	for i, kind := range []string{"SCMConnection", "Graph", "PullRequestReviewRoute"} {
		if docs[i].Kind != kind {
			t.Fatal(docs)
		}
	}
}

func TestCloudWorkflowKindsRemainSeparateAndPrecedeTrigger(t *testing.T) {
	docs := []Document{New("IssueTrigger", "issues", map[string]any{}), New("JiraCloudWorkflowMapping", "review", map[string]any{}), New("JiraCloudProfile", "cloud", map[string]any{}), New("MCPConnection", "cloud", map[string]any{})}
	Sort(docs)
	for i, kind := range []string{"MCPConnection", "JiraCloudProfile", "JiraCloudWorkflowMapping", "IssueTrigger"} {
		if docs[i].Kind != kind {
			t.Fatal(docs)
		}
	}
}
