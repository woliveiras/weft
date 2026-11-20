package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func (s *Server) validateConfigDependencies(ctx context.Context, ds []installation.Document, ps []configPrepared, current map[string]configObject, plan *installation.Plan) {
	defaultProviders := 0
	for _, p := range ps {
		if p.Doc.Kind == "ModelProvider" && p.Default {
			defaultProviders++
		}
	}
	graphs := map[string]*domain.Graph{}
	connections := map[string]mcpconfig.Input{}
	policy := s.Config.Isolation.Docker.EnvironmentPolicy
	if o, ok := current["NetworkPolicy/installation"]; ok {
		var n environmentpolicy.NetworkSettings
		b, _ := json.Marshal(o.Doc.Spec)
		if json.Unmarshal(b, &n) == nil {
			policy.SetupNetworks, policy.RunNetworks, policy.EgressHosts = n.SetupNetworks, n.RunNetworks, n.EgressHosts
		}
	}
	for i, p := range ps {
		if plan.Objects[i].Action == "error" {
			continue
		}
		switch v := p.Input.(type) {
		case *domain.Graph:
			graphs[v.Metadata.ID] = v
		case environmentpolicy.NetworkSettings:
			policy.SetupNetworks, policy.RunNetworks, policy.EgressHosts = v.SetupNetworks, v.RunNetworks, v.EgressHosts
		case mcpconfig.Input:
			connections[v.ID] = v
		}
	}
	for i, p := range ps {
		item := &plan.Objects[i]
		if item.Action == "error" || item.Action == "unchanged" {
			continue
		}
		var err error
		if p.Doc.Kind == "ModelProvider" && p.Default && defaultProviders > 1 {
			err = fmt.Errorf("spec.default: at most one explicit default provider")
		}
		switch v := p.Input.(type) {
		case jiradc.MappingInput:
			err = validateMappingDependency(v, current[p.Doc.Key()], ps, current, plan)
		case *domain.Graph:
			err = policy.Check(v.Spec.Environment)
			if err != nil {
				err = fmt.Errorf("spec.environment: %w", err)
			}
		case trigger.Input:
			if err = validatePlannedLifecycleMappings(v, ps, plan); err != nil {
				break
			}
			g := graphs[v.Workflow]
			if g == nil {
				g, err = s.Store.GetGraph(ctx, v.Workflow, 0)
				if err != nil {
					err = fmt.Errorf("spec.workflow: saved or planned workflow required")
				}
			}
			if err == nil {
				manager := s.triggers()
				original := manager.TrackerRevision
				manager.TrackerRevision = func(ctx context.Context, id, kind string) (int64, error) {
					if c, ok := connections[id]; ok {
						credential := c.Credential != nil || current["MCPConnection/"+id].Secrets["credential"]
						view := mcpconfig.View{ID: id, Service: c.Service, Endpoint: c.Endpoint, Enabled: c.Enabled, CredentialType: c.CredentialType, CredentialConfigured: credential}
						if mcpconfig.TrackerCompatible(view, kind) {
							return 1, nil
						}
						return 0, fmt.Errorf("spec.source.connectionId: compatible tracker connection required")
					}
					if original != nil {
						return original(ctx, id, kind)
					}
					return 0, nil
				}
				raw, marshalErr := json.Marshal(g)
				if marshalErr != nil {
					err = marshalErr
				} else {
					var copy domain.Graph
					if err = json.Unmarshal(raw, &copy); err == nil {
						_, err = manager.PreviewGraphConsent(ctx, v, &copy, "preview")
					}
				}
			}
			if err == nil && v.SCMConnection != "" {
				_, present := current["SCMConnection/"+v.SCMConnection]
				for j, doc := range ds {
					if doc.Key() == "SCMConnection/"+v.SCMConnection && plan.Objects[j].Action != "error" {
						present = true
					}
				}
				if !present {
					err = fmt.Errorf("spec.scmConnection: saved or planned connection required")
				}
			}
		}
		if err != nil {
			item.Action = "error"
			item.Error = err.Error()
			plan.OK = false
		}
	}
}

// A mapping pins the numeric connection revision explicitly. Never rewrite that
// pin to accommodate another object in the batch. New connections start at 1;
// each effective configuration save advances the reviewed native revision once.
func validateMappingDependency(in jiradc.MappingInput, old configObject, ps []configPrepared, current map[string]configObject, plan *installation.Plan) error {
	if old.Doc.Kind != "" && !in.Enabled {
		var previous jiradc.MappingInput
		if err := installation.Decode(old.Doc.Spec, &previous, "spec"); err != nil {
			return err
		}
		previous.Name, previous.Enabled = in.Name, false
		if in == previous {
			return nil // Exact disabling remains available while Jira is inaccessible.
		}
	}
	key := "IssueConnection/" + in.Connection
	c, found := current[key]
	var connection jiradc.ConnectionInput
	if found {
		if err := installation.Decode(c.Doc.Spec, &connection, "spec"); err != nil {
			return err
		}
	}
	revision := c.NativeRevision
	for i, p := range ps {
		if p.Doc.Key() != key {
			continue
		}
		if plan.Objects[i].Action == "error" || plan.Objects[i].Action == "conflict" {
			return fmt.Errorf("spec.connection: reviewed connection plan required")
		}
		connection = p.Input.(jiradc.ConnectionInput)
		found = true
		if plan.Objects[i].Action != "unchanged" {
			revision++
		}
		break
	}
	if !found || connection.Kind != "jira-dc" || !connection.Enabled || !connection.ProjectRestricted {
		return fmt.Errorf("spec.connection: enabled project-restricted native Jira connection required")
	}
	if revision != in.ConnectionRevision {
		return fmt.Errorf("spec.connectionRevision: must explicitly match the saved or planned connection revision (%d); discover and review the sample for that scope", revision)
	}
	return nil
}

// Trigger consent can review only persisted mapping definitions. Do not validate
// against the old definition while an earlier object in this batch replaces it.
func validatePlannedLifecycleMappings(in trigger.Input, ps []configPrepared, plan *installation.Plan) error {
	for _, pin := range []trigger.MappingPin{in.LifecycleMappings.WorkStarted, in.LifecycleMappings.PRDelivered} {
		if pin.Name == "" {
			continue
		}
		for i, p := range ps {
			if p.Doc.Kind == "IssueWorkflowMapping" && p.Doc.Name() == pin.Name && plan.Objects[i].Action != "unchanged" {
				return fmt.Errorf("spec.lifecycleMappings: apply mapping definitions first, then select their saved revisions and review trigger consent")
			}
		}
	}
	return nil
}
