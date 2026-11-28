package taskauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

const prefix = "tasks/authorization/"

type Record struct {
	Source          string `json:"source,omitempty"`
	WorkflowID      string `json:"workflowId,omitempty"`
	WorkflowVersion int    `json:"workflowVersion,omitempty"`
	ExecutionScope  string `json:"executionScope,omitempty"`
	TaskID          string `json:"taskId"`
	Mode            Mode   `json:"mode"`
	Scope           string `json:"scope"`
	Actor           string `json:"actor"`
	PolicyVersion   int    `json:"policyVersion"`
	Revision        int64  `json:"revision"`
	Operation       string `json:"operation,omitempty"`
}
type AgentScope struct {
	ID        string               `json:"id"`
	Isolation domain.IsolationMode `json:"isolation"`
	Grants    []domain.ToolGrant   `json:"grants"`
}
type Preference struct {
	Mode     Mode  `json:"mode"`
	Revision int64 `json:"revision"`
}
type Review struct {
	WorkflowProfile Mode          `json:"workflowProfile,omitempty"`
	WorkflowVersion int           `json:"workflowVersion,omitempty"`
	ProjectID       string        `json:"projectId,omitempty"`
	Preference      *Preference   `json:"preference,omitempty"`
	Capabilities    []AgentScope  `json:"capabilities"`
	Record          *Record       `json:"record,omitempty"`
	Scope           string        `json:"scope"`
	Agents          []domain.Node `json:"-"`
}
type Manager struct {
	Store storage.Store
	Tools *tools.Registry
}

func (m Manager) Review(ctx context.Context, id string) (*Review, error) {
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
	if err != nil {
		return nil, err
	}
	version := 0
	if len(d.Repositories) > 0 && d.ExecutionGraphID != "" {
		version = d.GraphVersion
	}
	g, err := m.Store.GetGraph(ctx, d.GraphID, version)
	if err != nil {
		return nil, err
	}
	if g.Metadata.Version != d.GraphVersion {
		return nil, storage.ErrConflict
	}
	b, err := json.Marshal(struct {
		Input              taskconfig.Input
		Graph              *domain.Graph
		Policy             int
		RepositoryRevision int64 `json:",omitempty"`
	}{d.Input, g, PolicyVersion, d.RepositoryRevision})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	graph.Normalize(g)
	r := &Review{ProjectID: d.ProjectID, Scope: hex.EncodeToString(sum[:]), Agents: g.Spec.Nodes}
	if g.Spec.Task != nil && g.Spec.Task.AuthorizationProfile != "" {
		r.WorkflowProfile = g.Spec.Task.AuthorizationProfile
		r.WorkflowVersion = g.Metadata.Version
		if !r.WorkflowProfile.IsProfile() {
			return nil, ErrInvalid
		}
	}
	for _, n := range g.Spec.Nodes {
		if !n.Disabled {
			r.Capabilities = append(r.Capabilities, AgentScope{ID: n.ID, Isolation: n.Isolation.Mode, Grants: n.Tools})
		}
	}
	if d.ProjectID != "" {
		pref, e := m.Store.GetSetting(ctx, preferenceKey(d.ProjectID))
		if e != nil && !errors.Is(e, storage.ErrNotFound) {
			return nil, e
		}
		if e == nil {
			var p Preference
			if json.Unmarshal(pref.Value, &p) != nil || !Valid(p.Mode) {
				return nil, ErrInvalid
			}
			p.Revision = pref.Revision
			r.Preference = &p
		}
	}
	r.Record, err = m.record(ctx, id)
	return r, err
}

// UsesWorkflowGrants selects the delivery contract for the execution copy.
func (m Manager) UsesWorkflowGrants(ctx context.Context, id string, workflow *domain.Graph) (bool, error) {
	if workflow.Spec.Task != nil && workflow.Spec.Task.AuthorizationProfile.IsProfile() {
		return true, nil
	}
	rec, err := m.record(ctx, id)
	return rec != nil && rec.Mode.IsProfile(), err
}

func (m Manager) record(ctx context.Context, id string) (*Record, error) {
	setting, err := m.Store.GetSetting(ctx, prefix+id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec Record
	if json.Unmarshal(setting.Value, &rec) != nil || rec.TaskID != id || !Valid(rec.Mode) || rec.PolicyVersion != PolicyVersion {
		return nil, ErrInvalid
	}
	rec.Revision = setting.Revision
	return &rec, nil
}
func (m Manager) Save(ctx context.Context, id string, mode Mode, scope string, revision int64, actor string, confirm bool) (*Record, error) {
	return m.save(ctx, id, mode, scope, revision, actor, confirm, 0, "", false)
}

func (m Manager) SaveReviewed(ctx context.Context, id string, mode Mode, scope string, revision, draftRevision int64, actor, operation string) (*Record, error) {
	if operation == "" || draftRevision < 1 {
		return nil, ErrInvalid
	}
	return m.save(ctx, id, mode, scope, revision, actor, true, draftRevision, operation, false)
}

func (m Manager) save(ctx context.Context, id string, mode Mode, scope string, revision int64, actor string, confirm bool, draftRevision int64, operation string, inherited bool) (*Record, error) {
	if !Valid(mode) || actor == "" || !confirm {
		return nil, ErrInvalid
	}
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if draftRevision > 0 && d.Revision != draftRevision {
		return nil, storage.ErrConflict
	}
	running := d.Status == "running" || d.Status == "starting"
	if d.Status != "draft" && d.Status != "environment_pending" && !running {
		return nil, storage.ErrConflict
	}
	review, err := m.Review(ctx, id)
	if err != nil {
		return nil, err
	}
	if review.WorkflowProfile != "" && !mode.IsProfile() {
		return nil, ErrInvalid
	}
	if inherited && mode != review.StartConsent().Mode {
		return nil, storage.ErrConflict
	}
	if scope != review.Scope {
		return nil, storage.ErrConflict
	}
	if running && (review.Record == nil || !Reduction(review.Record.Mode, mode) || review.Record.Scope != scope) {
		return nil, storage.ErrConflict
	}
	if err := ValidateMode(mode, review.Agents, m.Tools); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	rec := &Record{TaskID: id, Mode: mode, Scope: scope, Actor: actor, PolicyVersion: PolicyVersion, Operation: operation}
	if mode.IsProfile() {
		rec.Source, rec.WorkflowID, rec.WorkflowVersion = "task", d.GraphID, d.GraphVersion
		if inherited {
			rec.Source = "workflow"
		}
	}
	if running {
		rec.ExecutionScope = review.Record.ExecutionScope
	}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.authorization_requested", d.GraphID, map[string]any{"taskId": id, "mode": mode, "scope": scope, "expectedRevision": revision, "source": rec.Source, "workflowVersion": rec.WorkflowVersion}).WithActor(actor)); err != nil {
		return nil, err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	rec.Revision, err = m.Store.PutSettingIf(ctx, prefix+id, b, revision, actor, []storage.SettingRevision{d.RevisionGuard()})
	return rec, err
}

// Ensure snapshots the workflow default before any task side effect.
func (m Manager) Ensure(ctx context.Context, id, actor string) error {
	review, err := m.Review(ctx, id)
	if err != nil {
		return err
	}
	if review.Record == nil {
		_, err = m.save(ctx, id, review.StartConsent().Mode, review.Scope, 0, actor, true, 0, "", true)
		return err
	}
	if review.Record.Scope != review.Scope {
		return fmt.Errorf("%w: task authorization scope changed; review and confirm again", ErrInvalid)
	}
	return nil
}

// Check is called at gateway admission and again immediately before invocation.
func (m Manager) Check(ctx context.Context, p *tools.Principal, t tools.Tool, mandatory bool, args json.RawMessage) (bool, string, error) {
	if !strings.HasPrefix(p.GraphID, "task-run-") {
		return mandatory, "", nil
	}
	g, err := m.Store.GetGraph(ctx, p.GraphID, 0)
	if err != nil {
		return false, "", err
	}
	id := g.Metadata.Labels[domain.LabelTask]
	if id == "" || domain.TaskExecutionID(id) != p.GraphID || g.Metadata.ID != p.GraphID || g.Metadata.Version != p.GraphVersion {
		return false, "", ErrDenied
	}
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
	if err != nil {
		return false, "", err
	}
	if d.ExecutionGraphID != p.GraphID || (d.Status != "starting" && d.Status != "running") {
		return false, "", ErrDenied
	}
	review, err := m.Review(ctx, id)
	if err != nil {
		return false, "", err
	}
	if review.Record == nil {
		if g.Metadata.Labels[domain.LabelTaskConsent] == "required" || review.WorkflowProfile.IsProfile() {
			return false, "", ErrDenied
		}
		// Historical execution copies created before consent records existed.
		return mandatory, "legacy", nil
	}
	rec := review.Record
	currentScope, scopeErr := executionScope(g)
	if scopeErr != nil {
		return false, "", scopeErr
	}
	if rec.ExecutionScope == "" || rec.ExecutionScope != currentScope {
		return false, "", ErrDenied
	}
	if rec.Scope != review.Scope {
		return false, "", ErrDenied
	}
	kind := "process"
	if p.Sandbox != nil {
		kind = p.Sandbox.Kind()
	}
	if rec.Mode.IsProfile() && len(d.Repositories) > 0 {
		// Multi-repository progress is pinned. Recheck the live policy before a new effect.
		latest, err := m.Store.GetGraph(ctx, d.GraphID, 0)
		if err != nil {
			return false, "", err
		}
		if latest.Metadata.Version != d.GraphVersion {
			return false, "", ErrDenied
		}
	}
	required, err := Approval(rec.Mode, t.Capability(), kind, mandatory)
	if rec.Mode.IsProfile() {
		routine := routineTool(t.Name(), kind)
		if t.Name() == "git.push" || t.Name() == "scm.pull_request.create" {
			if !boundDelivery(d.Repositories, args) {
				return false, "", fmt.Errorf("%w: choose a repository, connection and base branch in task delivery", ErrDenied)
			}
			routine = true
		}
		if rec.Mode == Assisted && !mandatory {
			required = !routine
		}
	}
	return required, fmt.Sprintf("%s:%d:%s", id, rec.Revision, rec.Mode), err
}

func executionScope(g *domain.Graph) (string, error) {
	b, err := json.Marshal(g.Spec)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Bind seals the execution copy after normalization and before any agent starts.
func (m Manager) Bind(ctx context.Context, id string, g *domain.Graph) error {
	review, err := m.Review(ctx, id)
	if err != nil {
		return err
	}
	if review.Record == nil || review.Record.Scope != review.Scope {
		return ErrDenied
	}
	rec := review.Record
	if rec.Mode.IsProfile() {
		// A prepared graph may predate a task override. Reject grants added by
		// legacy delivery preparation, even if the mode changed concurrently.
		for _, node := range g.Spec.Nodes {
			if node.Disabled {
				continue
			}
			i := slices.IndexFunc(review.Agents, func(a domain.Node) bool { return a.ID == node.ID && !a.Disabled })
			if i < 0 || !reflect.DeepEqual(node.Tools, review.Agents[i].Tools) {
				return fmt.Errorf("%w: execution grants differ from the workflow for %s", ErrDenied, node.ID)
			}
		}
	}
	rec.ExecutionScope, err = executionScope(g)
	if err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = m.Store.PutSetting(ctx, prefix+id, b, rec.Revision, rec.Actor)
	return err
}

func preferenceKey(project string) string {
	sum := sha256.Sum256([]byte(project))
	return "tasks/authorization-preferences/" + hex.EncodeToString(sum[:])
}

// Remember stores a suggestion, never consent for another task. CAS prevents lost updates.
func (m Manager) Remember(ctx context.Context, id string, mode Mode, revision int64, actor string) error {
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
	if err != nil {
		return err
	}
	if d.ProjectID == "" || !Valid(mode) {
		return ErrInvalid
	}
	review, err := m.Review(ctx, id)
	if err != nil {
		return err
	}
	if review.Record == nil || review.Record.Mode != mode || review.Record.Scope != review.Scope {
		return ErrDenied
	}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.authorization_preference_requested", d.GraphID, map[string]any{"taskId": id, "mode": mode, "expectedRevision": revision}).WithActor(actor)); err != nil {
		return err
	}
	b, err := json.Marshal(Preference{Mode: mode})
	if err != nil {
		return err
	}
	_, err = m.Store.PutSetting(ctx, preferenceKey(d.ProjectID), b, revision, actor)
	return err
}
