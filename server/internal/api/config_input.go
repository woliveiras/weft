package api

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llmconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/trigger"
)

type configProject struct {
	URL           string         `json:"url,omitempty"`
	Ref           string         `json:"ref,omitempty"`
	Path          string         `json:"path,omitempty"`
	DisplayName   string         `json:"displayName,omitempty"`
	Checks        []domain.Check `json:"checks,omitempty"`
	ExecutionMode string         `json:"executionMode,omitempty"`
	Username      string         `json:"username,omitempty"`
}
type configPrepared struct {
	Doc     installation.Document
	Input   any
	Secret  string
	Default bool
	Values  []string
}

func (s *Server) prepareConfig(ctx context.Context, d installation.Document, old configObject, plugins map[string]bool) (configPrepared, error) {
	p := configPrepared{Doc: installation.Clone(d)}
	spec := p.Doc.Spec
	for _, field := range []string{"secretRef", "oauthClientSecretRef", "processEnvironmentRefs", "credentialUsernameRef"} {
		if _, ok := spec[field]; ok {
			supported := d.Kind == "MCPConnection" || (field == "secretRef" && (d.Kind == "ModelProvider" || d.Kind == "SCMConnection" || d.Kind == "IssueConnection" || d.Kind == "Project"))
			if !supported {
				return p, fmt.Errorf("spec.%s: not supported by this kind", field)
			}
		}
	}
	resolver := installation.Resolver{Dir: s.Config.Secrets.Dir, DeniedEnv: []string{s.Config.Secrets.KeyEnv, s.Config.Secrets.PreviousKeysEnv}}
	values := map[string]string{}
	for field, slot := range map[string]string{"secretRef": "credential", "oauthClientSecretRef": "oauthClient", "credentialUsernameRef": "credentialUsername"} {
		if raw, ok := spec[field]; ok {
			var ref installation.SecretRef
			if err := installation.Decode(raw, &ref, "spec."+field); err != nil {
				return p, err
			}
			value, err := resolver.Resolve(ref)
			if err != nil {
				return p, err
			}
			if ref.Preserve && !old.Secrets[slot] {
				return p, fmt.Errorf("spec.%s: no stored credential to preserve", field)
			}
			delete(spec, field)
			if !ref.Preserve {
				values[slot] = value
				p.Values = append(p.Values, value)
			}
		}
	}
	for _, forbidden := range []string{"apiKey", "credential", "token", "oauthClientSecret", "processEnvironment", "authentication"} {
		if _, ok := spec[forbidden]; ok {
			return p, fmt.Errorf("spec.%s: plaintext credential input is forbidden; use secret references", forbidden)
		}
	}
	envUpdates := map[string]string{}
	if raw, ok := spec["processEnvironmentRefs"]; ok {
		var refs map[string]installation.SecretRef
		if err := installation.Decode(raw, &refs, "spec.processEnvironmentRefs"); err != nil {
			return p, err
		}
		for name, ref := range refs {
			value, err := resolver.Resolve(ref)
			if err != nil {
				return p, err
			}
			if ref.Preserve && !old.Secrets["environment/"+name] {
				return p, fmt.Errorf("spec.processEnvironmentRefs: no stored value to preserve")
			}
			if !ref.Preserve {
				envUpdates[name] = value
				p.Values = append(p.Values, value)
			}
		}
		delete(spec, "processEnvironmentRefs")
	}
	if len(values) > 0 || len(envUpdates) > 0 {
		if d.Kind != "ModelProvider" && d.Kind != "MCPConnection" && d.Kind != "SCMConnection" && d.Kind != "IssueConnection" && d.Kind != "Project" {
			return p, fmt.Errorf("spec.secretRef: unsupported for kind")
		}
	}
	credential := values["credential"]
	decode := func(v any) error { return installation.Decode(spec, v, "spec") }
	switch d.Kind {
	case "InferencePlugin":
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decode(&in); err != nil {
			return p, err
		}
		if in.Enabled == nil {
			return p, fmt.Errorf("spec.enabled: required boolean")
		}
		if _, ok := llm.PluginManifest(d.Name()); !ok {
			return p, fmt.Errorf("metadata.name: plugin is not installed")
		}
		p.Input = *in.Enabled
		p.Doc.Spec = map[string]any{"enabled": *in.Enabled}
	case "ModelProvider":
		if _, ok := spec["default"]; !ok && old.Doc.Kind != "" {
			spec["default"] = old.Doc.Spec["default"]
		}
		var in struct {
			llmconfig.ProviderInput
			Default bool `json:"default"`
		}
		spec["name"] = d.Name()
		if err := decode(&in); err != nil {
			return p, err
		}
		if credential != "" {
			in.APIKey = &llmconfig.CredentialUpdate{Value: credential}
		}
		if s.Providers == nil {
			return p, fmt.Errorf("spec: provider management unavailable")
		}
		normalized := in.ProviderInput
		public := normalized
		public.APIKey = nil
		comparable := installation.Map(public)
		delete(comparable, "name")
		comparable["default"] = in.Default
		same := old.Doc.Kind != "" && installation.Digest(comparable) == installation.Digest(old.Doc.Spec)
		if same && credential != "" {
			var err error
			same, err = s.Providers.CredentialMatches(ctx, d.Name(), credential)
			if err != nil {
				return p, fmt.Errorf("spec.secretRef: stored credential unavailable")
			}
		}
		// Existing unavailable/disabled providers can round-trip without a write.
		if !same {
			var err error
			normalized, err = s.Providers.Preview(ctx, in.ProviderInput, plugins)
			if err != nil {
				return p, err
			}
		}
		p.Input = normalized
		p.Default = in.Default
		normalized.APIKey = nil
		p.Doc.Spec = installation.Map(normalized)
		delete(p.Doc.Spec, "name")
		p.Doc.Spec["default"] = in.Default
	case "PriceCatalog":
		var in cost.CatalogueInput
		if err := decode(&in); err != nil {
			return p, err
		}
		if err := cost.NormalizeCatalogue(&in); err != nil {
			return p, err
		}
		p.Input = in
		p.Doc.Spec = installation.Map(in)
	case "CostPolicy":
		var in struct {
			TaskRunBudgetMicros int64 `json:"taskRunBudgetMicros"`
			WarningPercent      int   `json:"warningPercent"`
		}
		if err := decode(&in); err != nil {
			return p, err
		}
		policy := cost.Policy{TaskRunBudgetMicros: in.TaskRunBudgetMicros, WarningPercent: in.WarningPercent}
		if err := cost.NormalizePolicy(&policy); err != nil {
			return p, err
		}
		p.Input = policy
		p.Doc.Spec = map[string]any{"taskRunBudgetMicros": policy.TaskRunBudgetMicros, "warningPercent": policy.WarningPercent}
	case "Budget":
		var in cost.Definition
		if err := decode(&in); err != nil {
			return p, err
		}
		defs := []cost.Definition{in}
		if err := cost.NormalizeDefinitions(defs); err != nil {
			return p, err
		}
		in = defs[0]
		if d.Name() != in.Scope+"/"+in.ID {
			return p, fmt.Errorf("metadata.name: expected scope/id")
		}
		p.Input = in
		p.Doc.Spec = installation.Map(in)
	case "NetworkPolicy":
		var in environmentpolicy.NetworkSettings
		if err := decode(&in); err != nil {
			return p, err
		}
		if err := in.Validate(); err != nil {
			return p, fmt.Errorf("spec: %w", err)
		}
		p.Input = in
		p.Doc.Spec = installation.Map(in)
	case "MCPConnection":
		var in mcpconfig.Input
		spec["id"] = d.Name()
		if err := decode(&in); err != nil {
			return p, err
		}
		if s.MCP == nil {
			return p, fmt.Errorf("spec: MCP management unavailable")
		}
		if credential != "" {
			in.Credential = &mcpconfig.Credential{Value: credential, Username: values["credentialUsername"]}
		}
		if v := values["oauthClient"]; v != "" {
			in.OAuthClientSecret = &mcpconfig.Credential{Value: v}
		}
		if len(envUpdates) > 0 {
			env, err := s.MCP.MergeEnvironment(ctx, d.Name(), envUpdates)
			if err != nil {
				return p, err
			}
			in.ProcessEnvironment = &env
		}
		if err := s.MCP.Preview(ctx, in); err != nil {
			return p, err
		}
		p.Input = in
		in.Credential = nil
		in.OAuthClientSecret = nil
		in.ProcessEnvironment = nil
		p.Doc.Spec = installation.Map(in)
		delete(p.Doc.Spec, "id")
	case "SCMConnection":
		var in scm.Input
		spec["name"] = d.Name()
		if err := decode(&in); err != nil {
			return p, err
		}
		if credential != "" {
			in.Token = &credential
		}
		if old.Doc.Kind == "" && in.Token == nil {
			return p, fmt.Errorf("spec.secretRef: required for a new SCM connection")
		}
		normalized, err := s.SCM.Preview(ctx, in)
		if err != nil {
			return p, err
		}
		p.Input = normalized
		normalized.Token = nil
		p.Doc.Spec = installation.Map(normalized)
		delete(p.Doc.Spec, "name")
	case "IssueConnection":
		var in jiradc.ConnectionInput
		spec["name"] = d.Name()
		if err := decode(&in); err != nil {
			return p, err
		}
		if credential != "" {
			in.Token = &credential
		}
		normalized, err := s.IssueConnections.Preview(ctx, in)
		if err != nil {
			return p, err
		}
		p.Input = normalized
		normalized.Token = nil
		p.Doc.Spec = installation.Map(normalized)
		delete(p.Doc.Spec, "name")
	case "Project":
		var in configProject
		if err := decode(&in); err != nil {
			return p, err
		}
		if in.URL != "" {
			u, err := repositoryimport.ValidateURL(in.URL)
			if err != nil {
				return p, fmt.Errorf("spec.url: expected public HTTPS clone URL without credentials")
			}
			in.URL = u
			if in.Ref == "" {
				in.Ref = "HEAD"
			}
			if !repositoryimport.ValidateRef(in.Ref) {
				return p, fmt.Errorf("spec.ref: invalid Git reference")
			}
			if in.Path != "" {
				return p, fmt.Errorf("spec.path: cannot combine with url")
			}
		} else if in.Path == "" {
			return p, fmt.Errorf("spec.url: URL or existing local path required")
		}
		if old.Doc.Kind != "" {
			for field, value := range map[string]string{"url": in.URL, "ref": in.Ref, "path": in.Path} {
				previous, _ := old.Doc.Spec[field].(string)
				if value != previous {
					return p, fmt.Errorf("spec.%s: project source is immutable; use a new identity", field)
				}
			}
		}
		if err := domain.ValidateChecks(in.Checks); err != nil {
			return p, fmt.Errorf("spec.checks: %w", err)
		}
		if in.ExecutionMode == "" {
			in.ExecutionMode = "workflow"
		}
		if !projectconfig.ValidExecutionPreference(in.ExecutionMode) {
			return p, fmt.Errorf("spec.executionMode: invalid preference")
		}
		if in.ExecutionMode == "local" && !s.Config.NativeProfile {
			return p, fmt.Errorf("spec.executionMode: local requires native Weft")
		}
		if in.Path != "" {
			if err := projectconfig.Validate(projectconfig.Input{Name: in.DisplayName, Path: in.Path}); err != nil {
				return p, fmt.Errorf("spec.path: invalid local project")
			}
		}
		p.Input = in
		p.Doc.Spec = installation.Map(in)
		delete(p.Doc.Spec, "username")
	case "Graph":
		gd := installation.Clone(d)
		delete(gd.Metadata, "revision")
		delete(gd.Metadata, "version")
		delete(gd.Metadata, "createdAt")
		delete(gd.Metadata, "updatedAt")
		raw, _ := json.Marshal(gd)
		g, issues, err := graph.Decode(raw, false)
		if err != nil {
			return p, fmt.Errorf("spec: invalid graph document")
		}
		if len(issues) > 0 {
			return p, unknownFieldsError(issues)
		}
		if err := s.Engine.ResolveAgentBindings(ctx, g); err != nil {
			return p, err
		}
		authored, err := json.Marshal(g)
		if err != nil {
			return p, err
		}
		rep := graph.Validate(g, s.Engine.KnownRuntimes())
		if !rep.Valid {
			return p, fmt.Errorf("spec: %s", graphErrors(rep))
		}
		id := g.Metadata.ID
		g = &domain.Graph{}
		if err := json.Unmarshal(authored, g); err != nil {
			return p, err
		}
		g.Metadata.ID = id
		p.Input = g
		safe := installation.Map(g)
		p.Doc.Spec = safe["spec"].(map[string]any)
		p.Doc.Metadata = safe["metadata"].(map[string]any)
		delete(p.Doc.Metadata, "createdAt")
		delete(p.Doc.Metadata, "updatedAt")
		delete(p.Doc.Metadata, "version")
	case "IssueTrigger":
		var in trigger.Input
		if _, ok := spec["name"]; !ok {
			spec["name"] = d.Name()
		}
		if err := decode(&in); err != nil {
			return p, err
		}
		if in.Authorization == "" {
			return p, fmt.Errorf("spec.authorizationMode: required (ask, workspace or full)")
		}
		if err := trigger.NormalizeInput(&in); err != nil {
			return p, err
		}
		p.Input = in
		p.Doc.Spec = installation.Map(in)
	}
	if (d.Kind == "CostPolicy" || d.Kind == "NetworkPolicy" || d.Kind == "PriceCatalog") && d.Name() != "installation" {
		return p, fmt.Errorf("metadata.name: singleton name must be installation")
	}
	// Compare credentials within their owning managers; never include their bytes or a digest in public state.
	for slot, value := range values {
		if slot == "credentialUsername" {
			continue
		}
		if slot == "credential" && d.Kind == "MCPConnection" {
			if in, ok := p.Input.(mcpconfig.Input); ok && in.CredentialType == "basic" {
				value = values["credentialUsername"] + ":" + value
			}
		}
		same := false
		var err error
		if old.Doc.Kind != "" && old.Secrets[slot] {
			switch d.Kind {
			case "ModelProvider":
				same, err = s.Providers.CredentialMatches(ctx, d.Name(), value)
			case "SCMConnection":
				same, err = s.SCM.CredentialMatches(ctx, d.Name(), value)
			case "IssueConnection":
				same, err = s.IssueConnections.CredentialMatches(ctx, d.Name(), value)
			case "MCPConnection":
				same, err = s.MCP.CredentialMatches(ctx, d.Name(), slot, value)
			}
		}
		if err != nil {
			return p, fmt.Errorf("spec.secretRef: stored credential unavailable")
		}
		if !same {
			p.Secret = "set"
			if old.Secrets[slot] {
				p.Secret = "changed"
			}
		} else if p.Secret == "" {
			p.Secret = "unchanged"
		}
	}
	if len(envUpdates) > 0 {
		previous, err := s.MCP.MergeEnvironment(ctx, d.Name(), nil)
		if err != nil {
			return p, err
		}
		same := true
		for k, v := range envUpdates {
			if previous[k] != v {
				same = false
			}
		}
		if !same {
			p.Secret = "changed"
		} else if p.Secret == "" {
			p.Secret = "unchanged"
		}
	}
	if p.Secret == "" {
		for _, set := range old.Secrets {
			if set {
				p.Secret = "unchanged"
				break
			}
		}
	}
	// Maps have the same JSON number types before comparison.
	p.Doc.Spec = installation.Map(p.Doc.Spec)
	return p, nil
}
func graphErrors(r *graph.Report) string {
	parts := []string{}
	for _, e := range r.Errors {
		parts = append(parts, e.Path+": "+e.Message)
	}
	return strings.Join(parts, "; ")
}
func configComparable(d installation.Document) map[string]any {
	m := installation.Map(d.Spec)
	if d.Kind == "Graph" {
		meta := installation.Map(d.Metadata)
		delete(meta, "revision")
		delete(meta, "createdAt")
		delete(meta, "updatedAt")
		delete(meta, "version")
		m["$metadata"] = meta
		if len(d.Extensions) > 0 {
			m["$extensions"] = d.Extensions
		}
	}
	return m
}
func configEqual(a, b installation.Document) bool {
	return reflect.DeepEqual(configComparable(a), configComparable(b))
}
