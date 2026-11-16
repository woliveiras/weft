package trigger

import "testing"

func TestConsentScopeMatchesLegacyExplicitEntry(t *testing.T) {
	in := Input{Workflow: "factory", NodeID: "entry", ResultNodeID: "entry"}
	c := Consent{WorkflowVersion: 1}
	old := triggerFingerprint(struct {
		Policy  int
		Input   Input
		Consent Consent
	}{1, in, c})
	c.Scope = "canonical"
	if !consentScopeMatches(old, in, c) {
		t.Fatal("legacy explicit entry consent rejected")
	}
	in.NodeID = "other"
	if consentScopeMatches(old, in, c) {
		t.Fatal("different entry granted old consent")
	}
}
