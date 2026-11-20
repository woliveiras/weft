package api

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/llmconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type configObject struct {
	Doc                  installation.Document
	NativeID             string
	NativeRevision       int64
	NativeObjectRevision int64
	Secrets              map[string]bool
	Status               string
	DefaultOnly          bool
	NativeTime           time.Time
	NativeConfig         string
	ConsentRequired      bool
}

func publicFields(v, shape any) map[string]any {
	src := installation.Map(v)
	dst := map[string]any{}
	t := reflect.TypeOf(shape)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if x, ok := src[name]; ok {
			dst[name] = x
		}
	}
	return dst
}
func newConfigObject(kind, name string, spec any, stamp any) configObject {
	d := installation.New(kind, name, spec)
	d.Metadata["revision"] = installation.Digest([]any{d.Spec, stamp})
	return configObject{Doc: d, NativeID: name, Secrets: map[string]bool{}}
}
func (s *Server) configSnapshot(ctx context.Context) (map[string]configObject, error) {
	out := map[string]configObject{}
	add := func(o configObject) error {
		if _, ok := out[o.Doc.Key()]; ok {
			return fmt.Errorf("configuration has ambiguous %s identities", o.Doc.Kind)
		}
		out[o.Doc.Key()] = o
		return nil
	}
	if s.Providers != nil {
		for _, v := range s.Providers.Plugins() {
			o := newConfigObject("InferencePlugin", v.ID, map[string]any{"enabled": v.Enabled}, v.UpdatedAt)
			o.NativeRevision = s.Providers.Meta().Revision
			o.DefaultOnly = v.UpdatedAt == nil && v.EnabledBy != "config"
			if err := add(o); err != nil {
				return nil, err
			}
		}
		for _, v := range s.Providers.List() {
			if !v.Editable {
				continue
			}
			spec := publicFields(v, llmconfig.ProviderInput{})
			delete(spec, "name")
			spec["default"] = s.Providers.Meta().Default == v.Name
			o := newConfigObject("ModelProvider", v.Name, spec, v.UpdatedAt)
			o.NativeRevision = s.Providers.Meta().Revision
			o.Secrets["credential"] = v.Credential.Configured
			o.Status = v.Status
			if err := add(o); err != nil {
				return nil, err
			}
		}
	}
	cat, rev, err := s.prices().Get(ctx)
	if err != nil {
		return nil, err
	}
	o := newConfigObject("PriceCatalog", "installation", cost.CatalogueInput{Currency: cat.Currency, Unpriced: cat.Unpriced, Prices: cat.Prices}, rev)
	o.NativeRevision = rev
	o.DefaultOnly = rev == 0
	if err = add(o); err != nil {
		return nil, err
	}
	policy, rev, err := s.budgets().Policy(ctx)
	if err != nil {
		return nil, err
	}
	o = newConfigObject("CostPolicy", "installation", map[string]any{"taskRunBudgetMicros": policy.TaskRunBudgetMicros, "warningPercent": policy.WarningPercent}, rev)
	o.NativeRevision = rev
	o.DefaultOnly = rev == 0
	if err = add(o); err != nil {
		return nil, err
	}
	defs, rev, objectRevisions, err := s.budgets().DefinitionsWithRevisions(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range defs {
		key := d.Scope + "/" + d.ID
		var stamp any
		if objectRevisions[key] > 0 {
			stamp = objectRevisions[key]
		}
		o = newConfigObject("Budget", key, d, stamp)
		o.NativeRevision = rev
		o.NativeObjectRevision = objectRevisions[key]
		o.DefaultOnly = rev == 0
		if err = add(o); err != nil {
			return nil, err
		}
	}
	net, err := s.networkSettings().Get(ctx)
	if err != nil {
		return nil, err
	}
	o = newConfigObject("NetworkPolicy", "installation", net.Network, net.Revision)
	o.NativeRevision = net.Revision
	o.DefaultOnly = net.Revision == 0
	if err = add(o); err != nil {
		return nil, err
	}
	if s.MCP != nil {
		vs, err := s.MCP.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range vs {
			spec := publicFields(v, mcpconfig.Input{})
			delete(spec, "id")
			if v.OAuth != nil {
				spec["oauth"] = installation.Map(v.OAuth.OAuthConfig)
			}
			stamp := v.UpdatedAt
			if v.ConfigurationUpdatedAt != nil {
				stamp = *v.ConfigurationUpdatedAt
			}
			o = newConfigObject("MCPConnection", v.ID, spec, stamp)
			o.NativeRevision = v.Revision
			o.Status = v.Status
			if v.Error != "" {
				o.Status += ": " + v.Error
			}
			o.Secrets, err = s.MCP.SecretSlots(ctx, v.ID)
			if err != nil {
				return nil, err
			}
			for _, k := range v.EnvironmentKeys {
				o.Secrets["environment/"+k] = true
			}
			if err = add(o); err != nil {
				return nil, err
			}
		}
	}
	if s.SCM.Store != nil {
		vs, err := s.SCM.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range vs {
			spec := publicFields(v, scm.Input{})
			delete(spec, "name")
			delete(spec, "token")
			o = newConfigObject("SCMConnection", v.Name, spec, v.UpdatedAt)
			o.Secrets["credential"] = v.TokenConfigured
			o.NativeTime = v.UpdatedAt
			if err = add(o); err != nil {
				return nil, err
			}
		}
	}
	if s.IssueConnections.Store != nil {
		vs, err := s.IssueConnections.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range vs {
			spec := publicFields(v, jiradc.ConnectionInput{})
			delete(spec, "name")
			delete(spec, "token")
			o := newConfigObject("IssueConnection", v.Name, spec, v.Revision)
			o.NativeRevision = v.Revision
			o.Secrets["credential"] = v.CredentialConfigured
			if err = add(o); err != nil {
				return nil, err
			}
		}
	}
	if s.IssueConnections.Store != nil {
		vs, err := (jiradc.WorkflowMappings{Connections: s.IssueConnections}).List(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range vs {
			spec := installation.Map(v.MappingInput)
			delete(spec, "name")
			o := newConfigObject("IssueWorkflowMapping", v.Name, spec, v.Revision)
			o.NativeRevision = v.Revision
			if err = add(o); err != nil {
				return nil, err
			}
		}
	}
	ps, err := (projectconfig.Manager{Store: s.Store}).List(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if p.ExecutionMode == "" {
			p.ExecutionMode = "workflow"
		}
		spec := installation.Map(configProject{URL: p.RepositoryURL, Ref: p.RepositoryRef, Checks: p.Checks, ExecutionMode: p.ExecutionMode})
		if p.RepositoryURL == "" {
			spec = installation.Map(configProject{Path: p.Path, DisplayName: p.Name, Checks: p.Checks, ExecutionMode: p.ExecutionMode})
		}
		o = newConfigObject("Project", p.ID, spec, p.Revision)
		o.NativeID = p.ID
		o.NativeRevision = p.Revision
		if err = add(o); err != nil {
			return nil, err
		}
	}
	gs, err := s.Store.ListGraphs(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		if strings.HasPrefix(g.Metadata.ID, "task-run-") {
			continue
		}
		b, err := json.Marshal(g)
		if err != nil {
			return nil, err
		}
		var d installation.Document
		if err = json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
		version := g.Metadata.Version
		delete(d.Metadata, "version")
		delete(d.Metadata, "createdAt")
		delete(d.Metadata, "updatedAt")
		d.Metadata["revision"] = installation.Digest([]any{d.Spec, d.Metadata, d.Extensions, version})
		o = configObject{Doc: d, NativeID: g.Metadata.ID, NativeRevision: int64(version), Secrets: map[string]bool{}}
		if err = add(o); err != nil {
			return nil, err
		}
	}
	ts, err := s.triggers().List(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		in := trigger.InputOf(t)
		spec := installation.Map(in)
		o = newConfigObject("IssueTrigger", t.ID, spec, []any{t.Consent, t.State, t.PausedReason})
		o.NativeID = t.ID
		o.NativeConfig = trigger.ConfigurationRevision(t)
		o.ConsentRequired = t.PausedReason == "consent required"
		if err = add(o); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func exportedObject(o configObject) installation.Document {
	d := installation.Clone(o.Doc)
	preserve := map[string]any{"preserve": true}
	if o.Secrets["credential"] {
		d.Spec["secretRef"] = preserve
	}
	if o.Secrets["oauthClient"] {
		d.Spec["oauthClientSecretRef"] = preserve
	}
	env := map[string]any{}
	for slot, set := range o.Secrets {
		if set && strings.HasPrefix(slot, "environment/") {
			env[strings.TrimPrefix(slot, "environment/")] = preserve
		}
	}
	if len(env) > 0 {
		d.Spec["processEnvironmentRefs"] = env
	}
	return d
}
