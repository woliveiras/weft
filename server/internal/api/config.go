package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/installation"
	"github.com/woliveiras/weft/server/internal/jiracloud"
	"github.com/woliveiras/weft/server/internal/jiradc"
	"github.com/woliveiras/weft/server/internal/llmconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
	"github.com/woliveiras/weft/server/internal/reviewroute"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/trigger"
)

func decodeConfig(w http.ResponseWriter, r *http.Request) (installation.Request, []installation.Document, error) {
	var in installation.Request
	r.Body = http.MaxBytesReader(w, r.Body, installation.MaxBytes*2)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF {
		return in, nil, badRequest("invalid configuration request")
	}
	ds, err := installation.Parse(in.Files)
	if err != nil {
		return in, nil, badRequest("%s", err)
	}
	keys := map[string]bool{}
	for _, d := range ds {
		keys[d.Key()] = true
	}
	for _, key := range in.Force {
		if !keys[key] {
			return in, nil, badRequest("force must name an object in the request")
		}
	}
	return in, ds, nil
}
func configLookup(snapshot map[string]configObject, d installation.Document) (configObject, error) {
	if old, ok := snapshot[d.Key()]; ok {
		return old, nil
	}
	if d.Kind == "IssueTrigger" {
		var found configObject
		for _, old := range snapshot {
			if old.Doc.Kind == "IssueTrigger" && old.Doc.Spec["name"] == d.Name() {
				if found.Doc.Kind != "" {
					return configObject{}, fmt.Errorf("metadata.name: ambiguous trigger name; use exported ID")
				}
				found = old
				found.Doc.Metadata = installation.Map(old.Doc.Metadata)
				found.Doc.Metadata["name"] = d.Name()
			}
		}
		return found, nil
	}
	if d.Kind == "Project" {
		var found configObject
		for _, old := range snapshot {
			if old.Doc.Kind != "Project" {
				continue
			}
			url, _ := d.Spec["url"].(string)
			ref, _ := d.Spec["ref"].(string)
			if ref == "" {
				ref = "HEAD"
			}
			match := url != "" && strings.TrimSuffix(url, ".git") == strings.TrimSuffix(fmt.Sprint(old.Doc.Spec["url"]), ".git") && ref == old.Doc.Spec["ref"]
			if path, ok := d.Spec["path"].(string); ok && path != "" {
				match = path == old.Doc.Spec["path"]
			}
			if match {
				if found.Doc.Kind != "" {
					return configObject{}, fmt.Errorf("spec.url: ambiguous existing projects")
				}
				found = old
				found.Doc.Metadata["name"] = d.Name()
			}
		}
		return found, nil
	}
	return configObject{}, nil
}
func (s *Server) buildConfigPlan(ctx context.Context, in installation.Request, ds []installation.Document) (installation.Plan, []configPrepared, map[string]configObject, error) {
	snapshot, err := s.configSnapshot(ctx)
	if err != nil {
		return installation.Plan{}, nil, nil, err
	}
	plan := installation.Plan{Objects: []installation.ObjectPlan{}, OK: true}
	prepared := []configPrepared{}
	plugins := map[string]bool{}
	for _, d := range ds {
		if d.Kind == "InferencePlugin" {
			if enabled, ok := d.Spec["enabled"].(bool); ok {
				plugins[d.Name()] = enabled
			}
		}
	}
	aliases := map[string]string{}
	for _, d := range ds {
		if d.Kind == "Project" {
			old, e := configLookup(snapshot, d)
			if e == nil && old.NativeID != "" {
				aliases[d.Name()] = old.NativeID
			}
		}
	}
	for _, d := range ds {
		if d.Kind == "IssueTrigger" {
			if alias, _ := d.Spec["projectId"].(string); aliases[alias] != "" {
				d = installation.Clone(d)
				d.Spec["projectId"] = aliases[alias]
			}
		}
		old, lookupErr := configLookup(snapshot, d)
		revision := "absent"
		if old.Doc.Kind != "" {
			revision = old.Doc.Revision()
		}
		item := installation.ObjectPlan{Key: d.Key(), Revision: revision, Changes: []installation.Change{}, Status: old.Status}
		p, err := s.prepareConfig(ctx, d, old, plugins)
		if lookupErr != nil {
			err = lookupErr
		}
		if err != nil {
			item.Action = "error"
			item.Error = secrets.Redact(err.Error(), p.Values...)
			plan.OK = false
		} else {
			item.Action = "create"
			item.Secret = p.Secret
			if old.Doc.Kind != "" {
				item.Action = "update"
				if configEqual(old.Doc, p.Doc) && p.Secret != "set" && p.Secret != "changed" {
					item.Action = "unchanged"
				}
			}
			if d.Kind == "IssueTrigger" && in.RecordConsent && old.ConsentRequired {
				item.Action = "update"
			}
			item.Changes = installation.Diff(configComparable(old.Doc), configComparable(p.Doc))
			if p.Secret == "changed" || p.Secret == "set" {
				item.Changes = append(item.Changes, installation.Change{Path: "spec.secretRef", Before: "unchanged", After: p.Secret})
			}
			baseline := d.Revision()
			if v := in.Revisions[d.Key()]; v != "" {
				baseline = v
			}
			if ((baseline != "" && baseline != revision) || (old.Doc.Kind != "" && !old.DefaultOnly && baseline == "" && item.Action != "unchanged")) && !slices.Contains(in.Force, d.Key()) {
				item.Action = "conflict"
				item.Error = "object changed or has no known revision; use object-specific force"
				plan.OK = false
			}
			if d.Kind == "IssueTrigger" {
				v := p.Input.(trigger.Input)
				item.Consent = fmt.Sprintf("Authorize trigger %s to poll %s with label %s every %d minutes, start workflow %s at %s with authorization %s and hand off through %s. Readiness enabled: %t.", d.Name(), v.Source.ConnectionID, v.Label, v.IntervalMinutes, v.Workflow, v.NodeID, v.Authorization, v.SCMConnection, !v.ReadinessDisabled)
				if !in.RecordConsent && item.Action != "unchanged" {
					item.Warning = "consent required; trigger will be paused"
				}
			}
			if (d.Kind == "IssueWorkflowMapping" || d.Kind == "JiraCloudWorkflowMapping") && item.Action != "unchanged" {
				item.Warning = "Apply may revalidate the reviewed sample with read-only Jira discovery, unless exactly disabling the saved definition; no transition is executed"
			}
			if d.Kind == "PullRequestReviewRoute" && item.Action != "unchanged" {
				route := p.Input.(reviewroute.Input)
				item.Consent = fmt.Sprintf("Route new pull requests from SCM connection %s at revision %s to workflow %s version %d, channel %s, agent %s. Routing never starts or resumes the workflow or grants tools.", route.Connection, route.ConnectionRevision, route.GraphID, route.GraphVersion, route.ChannelID, route.NodeID)
				if route.Enabled {
					item.Warning = "Effective apply records a new activation boundary; earlier pull requests are not replayed"
					if !in.RecordConsent {
						item.Action = "error"
						item.Error = "recordConsent (--record-consent) is required to activate or change an enabled pull request review route"
						plan.OK = false
					}
				}
			}
			if d.Kind == "Project" && item.Action == "create" {
				item.Warning = "Confirm repository download into a managed directory"
			}
			if d.Kind == "Graph" && item.Action != "unchanged" {
				g := p.Input.(*domain.Graph)
				if g.Spec.Environment != nil {
					item.Warning = "environment definition requires a build; apply does not build"
				}
			}
		}
		prepared = append(prepared, p)
		plan.Objects = append(plan.Objects, item)
	}
	s.validateConfigDependencies(ctx, ds, prepared, snapshot, &plan)
	return plan, prepared, snapshot, nil
}
func (s *Server) configPlan(w http.ResponseWriter, r *http.Request) error {
	in, ds, err := decodeConfig(w, r)
	if err != nil {
		return err
	}
	plan, _, _, err := s.buildConfigPlan(r.Context(), in, ds)
	if err != nil {
		return err
	}
	writeJSON(w, 200, plan)
	return nil
}
func (s *Server) configExport(w http.ResponseWriter, r *http.Request) error {
	snapshot, err := s.configSnapshot(r.Context())
	if err != nil {
		return err
	}
	ds := []installation.Document{}
	for _, o := range snapshot {
		ds = append(ds, exportedObject(o))
	}
	raw, err := installation.Export(ds)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(200)
	_, err = io.WriteString(w, raw)
	return err
}
func (s *Server) configApply(w http.ResponseWriter, r *http.Request) error {
	in, ds, err := decodeConfig(w, r)
	if err != nil {
		return err
	}
	if !in.Confirmed {
		return badRequest("review the plan and explicitly confirm apply")
	}
	plan, _, _, err := s.buildConfigPlan(r.Context(), in, ds)
	if err != nil {
		return err
	}
	// Revalidate the entire reviewed snapshot before the first mutation, even with force.
	for i := range plan.Objects {
		item := &plan.Objects[i]
		if in.Expected[item.Key] != item.Revision {
			item.Action = "conflict"
			item.Error = "reviewed plan is stale or missing; plan again"
			plan.OK = false
		}
	}
	if !plan.OK {
		for i := range plan.Objects {
			plan.Objects[i].Result = "not-applied"
		}
		writeJSON(w, 200, plan)
		return nil
	}
	actor := identityFrom(r.Context()).Name
	failed := false
	aliases := map[string]string{}
	for i, d := range ds {
		item := &plan.Objects[i]
		if failed {
			item.Result = "not-applied"
			continue
		}
		current, err := s.configSnapshot(r.Context())
		if err != nil {
			item.Result = "failed"
			item.Error = "configuration state unavailable"
			failed = true
			continue
		}
		old, err := configLookup(current, d)
		if err != nil {
			item.Result = "failed"
			item.Error = err.Error()
			failed = true
			continue
		}
		revision := "absent"
		if old.Doc.Kind != "" {
			revision = old.Doc.Revision()
		}
		if revision != item.Revision {
			item.Result = "failed"
			item.Error = "object changed after the reviewed plan"
			failed = true
			continue
		}
		if item.Action == "unchanged" {
			item.Result = "unchanged"
			if d.Kind == "Project" {
				aliases[d.Name()] = old.NativeID
			}
			continue
		}
		// Resolve references again at the mutation boundary; the CLI never sees values.
		if d.Kind == "IssueTrigger" {
			if alias, _ := d.Spec["projectId"].(string); aliases[alias] != "" {
				d = installation.Clone(d)
				d.Spec["projectId"] = aliases[alias]
			}
		}
		p, err := s.prepareConfig(r.Context(), d, old, nil)
		if err == nil {
			item.Status, err = s.applyConfigObject(r.Context(), p, old, actor, in.RecordConsent)
		}
		if err != nil {
			item.Result = "failed"
			item.Error = secrets.Redact(err.Error(), p.Values...)
			failed = true
			continue
		}
		item.Result = "applied"
		current, err = s.configSnapshot(r.Context())
		if err != nil {
			item.Result = "failed"
			item.Error = "saved object; revision could not be read; export before retry"
			failed = true
			continue
		}
		next, err := configLookup(current, d)
		if err != nil || next.Doc.Kind == "" {
			item.Result = "failed"
			item.Error = "saved object could not be identified; export before retry"
			failed = true
			continue
		}
		item.Revision = next.Doc.Revision()
		if d.Kind == "Project" {
			aliases[d.Name()] = next.NativeID
		}
	}
	plan.OK = !failed
	writeJSON(w, 200, plan)
	return nil
}
func (s *Server) applyConfigObject(ctx context.Context, p configPrepared, old configObject, actor string, recordConsent bool) (string, error) {
	name := p.Doc.Name()
	switch p.Doc.Kind {
	case "InferencePlugin":
		_, err := s.Providers.SetPluginEnabled(ctx, name, p.Input.(bool), actor, old.NativeRevision)
		return "", err
	case "ModelProvider":
		in := p.Input.(llmconfig.ProviderInput)
		var err error
		if old.Doc.Kind == "" {
			_, err = s.Providers.Create(ctx, in, actor, s.Providers.Meta().Revision)
		} else {
			_, err = s.Providers.Update(ctx, name, in, actor, old.NativeRevision)
		}
		if err == nil {
			currentDefault := s.Providers.Meta().Default
			if p.Default && currentDefault != name {
				_, err = s.Providers.SetDefault(ctx, name, actor, s.Providers.Meta().Revision)
			} else if !p.Default && currentDefault == name {
				_, err = s.Providers.SetDefault(ctx, "", actor, s.Providers.Meta().Revision)
			}
		}
		return "", err
	case "PriceCatalog":
		_, err := s.prices().Put(ctx, p.Input.(cost.CatalogueInput), actor, old.NativeRevision)
		return "", err
	case "CostPolicy":
		_, err := s.budgets().SetPolicy(ctx, p.Input.(cost.Policy), actor, old.NativeRevision)
		return "", err
	case "Budget":
		in := p.Input.(cost.Definition)
		defs, rev, objectRevisions, err := s.budgets().DefinitionsWithRevisions(ctx)
		if err != nil {
			return "", err
		}
		found := false
		for i, d := range defs {
			if d.Scope == in.Scope && d.ID == in.ID {
				if old.Doc.Kind == "" || objectRevisions[d.Scope+"/"+d.ID] != old.NativeObjectRevision || installation.Digest(d) != installation.Digest(old.Doc.Spec) {
					return "", fmt.Errorf("budget changed after plan")
				}
				defs[i] = in
				found = true
			}
		}
		if !found {
			if old.Doc.Kind != "" {
				return "", fmt.Errorf("budget changed after plan")
			}
			defs = append(defs, in)
		}
		_, err = s.budgets().SetDefinitionsAt(ctx, defs, actor, rev)
		return "", err
	case "NetworkPolicy":
		v, err := s.networkSettings().Save(ctx, p.Input.(environmentpolicy.NetworkSettings), old.NativeRevision, actor)
		if err == nil {
			s.Bus.Publish(ctx, domain.NewEvent("installation.network_policy.updated", "", map[string]any{"revision": v.Revision}).WithActor(actor))
			s.Log.Info("installation network policy updated", "revision", v.Revision, "actor", actor)
		}
		return "", err
	case "MCPConnection":
		v, err := s.MCP.Put(ctx, p.Input.(mcpconfig.Input), old.NativeRevision, actor)
		if err != nil {
			return "", err
		}
		if v.Enabled {
			v, err = s.MCP.Refresh(ctx, name, actor)
		}
		if err != nil {
			return "", err
		}
		status := v.Status
		if v.Error != "" {
			status += ": " + v.Error
		}
		return status, nil
	case "SCMConnection":
		_, err := s.SCM.SaveAt(ctx, p.Input.(scm.Input), actor, old.Doc.Kind != "", old.NativeTime)
		if err != nil {
			return "", err
		}
		_, err = s.SCM.Test(ctx, name)
		if err != nil {
			return "unavailable: " + err.Error(), nil
		}
		return "ready", nil
	case "IssueConnection":
		_, err := s.IssueConnections.Put(ctx, p.Input.(jiradc.ConnectionInput), old.NativeRevision, actor)
		return "saved; project permission qualification pending", err
	case "IssueWorkflowMapping":
		_, err := (jiradc.WorkflowMappings{Connections: s.IssueConnections}).Put(ctx, p.Input.(jiradc.MappingInput), old.NativeRevision, actor)
		return "", err
	case "JiraCloudProfile":
		manager, err := s.cloudProfiles()
		if err != nil {
			return "", err
		}
		_, err = manager.Put(ctx, p.Input.(jiracloud.Input), old.NativeRevision, actor)
		return "", err
	case "JiraCloudWorkflowMapping":
		manager, err := s.cloudProfiles()
		if err != nil {
			return "", err
		}
		_, err = (jiradc.WorkflowMappings{Backend: manager.Backend()}).Put(ctx, p.Input.(jiradc.MappingInput), old.NativeRevision, actor)
		return "", err
	case "PullRequestReviewRoute":
		in := p.Input.(reviewroute.Input)
		if in.Enabled && !recordConsent {
			return "", fmt.Errorf("recordConsent is required to activate an enabled pull request review route")
		}
		manager, err := s.reviewRouteManager()
		if err != nil {
			return "", err
		}
		_, err = manager.Put(ctx, in, old.NativeRevision, actor)
		return "", err
	case "Project":
		return s.applyConfigProject(ctx, p, old, actor)
	case "Graph":
		g := p.Input.(*domain.Graph)
		if err := s.validateEnvironmentOrigin(ctx, g); err != nil {
			return "", err
		}
		_, _, err := s.Engine.SaveGraph(ctx, g, int(old.NativeRevision), actor)
		return "", err
	case "IssueTrigger":
		_, err := s.triggers().ApplyConfigurationAt(ctx, old.NativeID, p.Input.(trigger.Input), actor, recordConsent, old.NativeConfig)
		return "", err
	}
	return "", fmt.Errorf("unsupported configuration kind")
}
func (s *Server) applyConfigProject(ctx context.Context, p configPrepared, old configObject, actor string) (string, error) {
	in := p.Input.(configProject)
	id := old.NativeID
	if id == "" {
		if in.URL == "" {
			v, err := (projectconfig.Manager{Store: s.Store}).Create(ctx, projectconfig.Input{Name: in.DisplayName, Path: in.Path}, actor)
			if err != nil {
				return "", err
			}
			id = v.ID
		} else {
			if s.RepositoryImports == nil {
				return "", fmt.Errorf("repository imports unavailable")
			}
			request := repositoryimport.Input{URL: in.URL, Ref: in.Ref}
			if len(p.Values) > 0 {
				request.Authentication = repositoryimport.Authentication{Mode: "token", Username: in.Username, Token: p.Values[0]}
			}
			v, err := s.RepositoryImports.Start(ctx, request, actor)
			if err != nil {
				return "", err
			}
			if v.State == "failed" || v.State == "cancelled" || v.State == "interrupted" {
				request.Retry = true
				request.Revision = v.Revision
				v, err = s.RepositoryImports.Start(ctx, request, actor)
				if err != nil {
					return "", err
				}
			}
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for v.State != "ready" {
				if v.State == "failed" || v.State == "cancelled" || v.State == "interrupted" {
					return "", fmt.Errorf("repository import %s; inspect repository-imports for progress", v.State)
				}
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-ticker.C:
				}
				v, err = s.RepositoryImports.Get(ctx, v.ID)
				if err != nil {
					return "", err
				}
			}
			id = v.ProjectID
		}
	}
	m := projectconfig.Manager{Store: s.Store}
	v, err := m.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if old.NativeRevision > 0 && v.Revision != old.NativeRevision {
		return "", fmt.Errorf("project changed after plan")
	}
	if installation.Digest(v.Checks) != installation.Digest(in.Checks) {
		v, err = m.SetChecks(ctx, id, in.Checks, v.Revision, actor)
		if err != nil {
			return "", err
		}
	}
	if v.ExecutionMode != in.ExecutionMode {
		if in.ExecutionMode == "local" && !s.Config.NativeProfile {
			return "", fmt.Errorf("spec.executionMode: local requires native Weft")
		}
		_, err = m.SetExecutionPreference(ctx, id, in.ExecutionMode, v.Revision, actor)
	}
	return "ready", err
}
