package api

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func TestCloudYAMLProfileRequiresPreviouslySavedMCPRevision(t *testing.T) {
	in := jiracloud.Input{Name: "profile", MCPConnection: "cloud", MCPRevision: 4, Enabled: true}
	ps := []configPrepared{{Doc: installation.New("MCPConnection", "cloud", map[string]any{})}}
	for _, action := range []string{"create", "update", "conflict", "error"} {
		plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: action}}}
		if validateCloudProfileDependency(in, configObject{}, ps, &plan) == nil {
			t.Fatal("unsaved MCP scope accepted", action)
		}
	}
	plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: "unchanged"}}}
	if err := validateCloudProfileDependency(in, configObject{}, ps, &plan); err != nil {
		t.Fatal(err)
	}
	old := newConfigObject("JiraCloudProfile", in.Name, in, 1)
	delete(old.Doc.Spec, "name")
	in.Enabled = false
	plan.Objects[0].Action = "update"
	if err := validateCloudProfileDependency(in, old, ps, &plan); err != nil {
		t.Fatal("exact disable required dependency availability", err)
	}
	in.MCPRevision++
	if validateCloudProfileDependency(in, old, ps, &plan) == nil {
		t.Fatal("disable changed the MCP pin")
	}
}

func TestCloudYAMLConsentCannotAdoptPlannedProfileOrMapping(t *testing.T) {
	in := trigger.Input{Source: trigger.SourceConfig{Kind: "jira", ConnectionID: "cloud", ProfileID: "profile", ProfileRevision: 1}, LifecycleMappings: trigger.LifecycleMappings{PRDelivered: trigger.MappingPin{Name: "review", Revision: 1}}}
	for _, dependency := range []struct{ kind, name string }{{"MCPConnection", "cloud"}, {"JiraCloudProfile", "profile"}, {"JiraCloudWorkflowMapping", "review"}} {
		ps := []configPrepared{{Doc: installation.New(dependency.kind, dependency.name, map[string]any{})}}
		for _, action := range []string{"create", "update", "error", "conflict"} {
			plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: action}}}
			err := validatePlannedCloudConsent(in, ps, &plan)
			if dependency.kind == "JiraCloudWorkflowMapping" {
				err = validatePlannedLifecycleMappings(in, ps, &plan)
			}
			if err == nil {
				t.Fatal("Cloud consent adopted planned authority", dependency, action)
			}
		}
	}
	// An equal local name in the other edition does not invalidate Cloud consent.
	ps := []configPrepared{{Doc: installation.New("IssueWorkflowMapping", "review", map[string]any{})}}
	plan := installation.Plan{Objects: []installation.ObjectPlan{{Action: "update"}}}
	if err := validatePlannedLifecycleMappings(in, ps, &plan); err != nil {
		t.Fatal("DC mapping collided with Cloud namespace", err)
	}
	in.Source = trigger.SourceConfig{Kind: "jira", ConnectionID: "cloud"}
	in.LifecycleMappings = trigger.LifecycleMappings{}
	if err := validatePlannedCloudConsent(in, ps, &plan); err != nil {
		t.Fatal("legacy unmapped source changed", err)
	}
}
