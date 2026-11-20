package api

import (
	"context"
	"fmt"

	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/trigger"
)

// An unchanged profile and exact disabling do not need MCP to remain available.
// Any other edit must go through the current local compatibility preview.
func exactCloudProfileInput(in jiracloud.Input, old configObject) (bool, error) {
	if old.Doc.Kind == "" {
		return false, nil
	}
	var previous jiracloud.Input
	if err := installation.Decode(old.Doc.Spec, &previous, "spec"); err != nil {
		return false, err
	}
	previous.Name = in.Name
	if previous == in {
		return true, nil
	}
	previous.Enabled = false
	return previous == in, nil
}

func validateCloudProfileDependency(in jiracloud.Input, old configObject, ps []configPrepared, plan *installation.Plan) error {
	if offline, err := exactCloudProfileInput(in, old); err != nil || offline {
		return err
	}
	for i, p := range ps {
		if p.Doc.Key() == "MCPConnection/"+in.MCPConnection && plan.Objects[i].Action != "unchanged" {
			return fmt.Errorf("spec.mcpRevision: save the MCP connection first, then review its exact saved native revision for the Cloud profile")
		}
	}
	return nil
}

func (s *Server) validateCloudMappingDependency(ctx context.Context, in jiradc.MappingInput, old configObject, ps []configPrepared, current map[string]configObject, plan *installation.Plan) error {
	if old.Doc.Kind != "" && !in.Enabled {
		var previous jiradc.MappingInput
		if err := installation.Decode(old.Doc.Spec, &previous, "spec"); err != nil {
			return err
		}
		previous.Name, previous.Enabled = in.Name, false
		if previous == in {
			return nil
		}
	}
	key := "JiraCloudProfile/" + in.Connection
	for i, p := range ps {
		if p.Doc.Key() == key && plan.Objects[i].Action != "unchanged" {
			return fmt.Errorf("spec.connectionRevision: save the Cloud profile first, then discover the sample and select its saved revision")
		}
	}
	profile, ok := current[key]
	if !ok || profile.NativeRevision != in.ConnectionRevision {
		return fmt.Errorf("spec.connectionRevision: select an existing Cloud profile's exact saved native revision")
	}
	var scope jiracloud.Input
	if err := installation.Decode(profile.Doc.Spec, &scope, "spec"); err != nil {
		return err
	}
	scope.Name = in.Connection
	if !scope.Enabled || !scope.ProjectRestricted {
		return fmt.Errorf("spec.connection: enabled Cloud profile with project-restricted account confirmation required")
	}
	if err := validateCloudProfileDependency(scope, configObject{}, ps, plan); err != nil {
		return err
	}
	manager, err := s.cloudProfiles()
	if err != nil {
		return err
	}
	_, err = manager.Preview(ctx, scope) // Compatibility only; no remote discovery.
	return err
}

func validatePlannedCloudConsent(in trigger.Input, ps []configPrepared, plan *installation.Plan) error {
	if in.Source.Kind != "jira" || in.Source.ProfileID == "" {
		return nil
	}
	for i, p := range ps {
		if (p.Doc.Key() == "JiraCloudProfile/"+in.Source.ProfileID || p.Doc.Key() == "MCPConnection/"+in.Source.ConnectionID) && plan.Objects[i].Action != "unchanged" {
			return fmt.Errorf("spec.source: save the Cloud profile and MCP connection first, then review their persisted revisions for trigger consent")
		}
	}
	return nil
}
