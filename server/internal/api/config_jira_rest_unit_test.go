package api

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
)

func TestJiraRESTExportUsesIndependentPreserveReference(t *testing.T) {
	o := configObject{Doc: installation.New("MCPConnection", "jira", map[string]any{
		"jiraRest": map[string]any{"cloudId": "11111111-2222-3333-4444-555555555555"},
	}), Secrets: map[string]bool{"credential": true, "jiraRest": true}}
	d := exportedObject(o)
	for _, name := range []string{"secretRef", "jiraRestSecretRef"} {
		ref, ok := d.Spec[name].(map[string]any)
		if !ok || ref["preserve"] != true || len(ref) != 1 {
			t.Fatalf("missing independent preserve reference %s", name)
		}
	}
	if _, ok := d.Spec["jiraRestUsernameRef"]; ok {
		t.Fatal("export invented a saved email reference")
	}
	if _, ok := o.Doc.Spec["jiraRestSecretRef"]; ok {
		t.Fatal("export mutated the snapshot")
	}
}
