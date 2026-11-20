package api

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestMappingDependencyRequiresExplicitPlannedRevision(t *testing.T) {
	connection := jiradc.ConnectionInput{Name: "native", Kind: "jira-dc", SiteURL: "https://jira.example", ProjectID: "10001", Enabled: true, ProjectRestricted: true}
	in := jiradc.MappingInput{Name: "review-step", Connection: "native", ConnectionRevision: 1, Enabled: true}
	current := map[string]configObject{}
	p := configPrepared{Doc: installation.New("IssueConnection", "native", connection), Input: connection}
	plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: "create"}}}
	if err := validateMappingDependency(in, configObject{}, []configPrepared{p}, current, &plan); err != nil {
		t.Fatal(err)
	}
	in.ConnectionRevision = 2
	if err := validateMappingDependency(in, configObject{}, []configPrepared{p}, current, &plan); err == nil {
		t.Fatal("planned create rewrote the explicit pin")
	}
	c := newConfigObject("IssueConnection", "native", connection, 7)
	delete(c.Doc.Spec, "name")
	c.NativeRevision = 7
	current[c.Doc.Key()] = c
	in.ConnectionRevision = 8
	plan.Objects[0].Action = "update"
	if err := validateMappingDependency(in, configObject{}, []configPrepared{p}, current, &plan); err != nil {
		t.Fatal(err)
	}
	in.ConnectionRevision = 7
	if err := validateMappingDependency(in, configObject{}, []configPrepared{p}, current, &plan); err == nil {
		t.Fatal("planned update silently retained an obsolete pin")
	}
	plan.Objects[0].Action = "unchanged"
	if err := validateMappingDependency(in, configObject{}, []configPrepared{p}, current, &plan); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"error", "conflict"} {
		plan.Objects[0].Action = action
		if err := validateMappingDependency(in, configObject{}, []configPrepared{p}, current, &plan); err == nil {
			t.Fatal("invalid dependency accepted", action)
		}
	}
	if err := validateMappingDependency(in, configObject{}, nil, nil, &installation.Plan{}); err == nil {
		t.Fatal("missing dependency accepted")
	}
	for _, field := range []string{"enabled", "projectRestricted"} {
		saved := c
		saved.Doc = installation.Clone(c.Doc)
		saved.Doc.Spec[field] = false
		if err := validateMappingDependency(in, configObject{}, nil, map[string]configObject{c.Doc.Key(): saved}, &installation.Plan{}); err == nil {
			t.Fatal("unqualified connection accepted", field)
		}
	}
}

func TestMappingDependencyAllowsOnlyExactOfflineDisable(t *testing.T) {
	in := jiradc.MappingInput{Name: "review-step", Connection: "native", ConnectionRevision: 1, TransitionID: "31", Enabled: true}
	old := newConfigObject("IssueWorkflowMapping", in.Name, in, 1)
	delete(old.Doc.Spec, "name")
	in.Enabled = false
	if err := validateMappingDependency(in, old, nil, nil, &installation.Plan{}); err != nil {
		t.Fatal(err)
	}
	in.TransitionID = "32"
	if err := validateMappingDependency(in, old, nil, nil, &installation.Plan{}); err == nil {
		t.Fatal("offline disable changed the definition")
	}
}

func TestLifecycleConsentCannotReviewPlannedMappingReplacement(t *testing.T) {
	for _, pin := range []trigger.LifecycleMappings{{WorkStarted: trigger.MappingPin{Name: "review-step", Revision: 1}}, {PRDelivered: trigger.MappingPin{Name: "review-step", Revision: 2}}} {
		in := trigger.Input{LifecycleMappings: pin}
		ps := []configPrepared{{Doc: installation.New("IssueWorkflowMapping", "review-step", map[string]any{})}}
		for _, action := range []string{"create", "update", "error", "conflict"} {
			plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: action}}}
			if err := validatePlannedLifecycleMappings(in, ps, &plan); err == nil {
				t.Fatal("planned replacement accepted", action)
			}
		}
		plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: "unchanged"}}}
		if err := validatePlannedLifecycleMappings(in, ps, &plan); err != nil {
			t.Fatal(err)
		}
		if err := validatePlannedLifecycleMappings(in, nil, &installation.Plan{}); err != nil {
			t.Fatal(err)
		}
		plan.Objects[0].Action = "update"
		if err := validatePlannedLifecycleMappings(trigger.Input{}, ps, &plan); err != nil {
			t.Fatal("unmapped trigger affected", err)
		}
	}
}
