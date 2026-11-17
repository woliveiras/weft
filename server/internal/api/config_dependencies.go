package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/installation"
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
		case *domain.Graph:
			err = policy.Check(v.Spec.Environment)
			if err != nil {
				err = fmt.Errorf("spec.environment: %w", err)
			}
		case trigger.Input:
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
