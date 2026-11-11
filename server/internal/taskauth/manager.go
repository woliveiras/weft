package taskauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/tools"
)

const prefix = "tasks/authorization/"

type Record struct {
	ExecutionScope string `json:"executionScope,omitempty"`
	TaskID         string `json:"taskId"`
	Mode           Mode   `json:"mode"`
	Scope          string `json:"scope"`
	Actor          string `json:"actor"`
	PolicyVersion  int    `json:"policyVersion"`
	Revision       int64  `json:"revision"`
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
	ProjectID    string        `json:"projectId,omitempty"`
	Preference   *Preference   `json:"preference,omitempty"`
	Capabilities []AgentScope  `json:"capabilities"`
	Record       *Record       `json:"record,omitempty"`
	Scope        string        `json:"scope"`
	Agents       []domain.Node `json:"-"`
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
	g, err := m.Store.GetGraph(ctx, d.GraphID, 0)
	if err != nil {
		return nil, err
	}
	if g.Metadata.Version != d.GraphVersion {
		return nil, storage.ErrConflict
	}
	b, err := json.Marshal(struct {
		Input  taskconfig.Input
		Graph  *domain.Graph
		Policy int
	}{d.Input, g, PolicyVersion})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	graph.Normalize(g)
	r := &Review{ProjectID: d.ProjectID, Scope: hex.EncodeToString(sum[:]), Agents: g.Spec.Nodes}
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
	setting, err := m.Store.GetSetting(ctx, prefix+id)
	if errors.Is(err, storage.ErrNotFound) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var rec Record
	if json.Unmarshal(setting.Value, &rec) != nil || rec.TaskID != id || !Valid(rec.Mode) || rec.PolicyVersion != PolicyVersion {
		return nil, ErrInvalid
	}
	rec.Revision = setting.Revision
	r.Record = &rec
	return r, nil
}
func (m Manager) Save(ctx context.Context, id string, mode Mode, scope string, revision int64, actor string, confirm bool) (*Record, error) {
	if !Valid(mode) || actor == "" || !confirm {
		return nil, ErrInvalid
	}
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
	if err != nil {
		return nil, err
	}
	running := d.Status == "running" || d.Status == "starting"
	if d.Status != "draft" && d.Status != "environment_pending" && !running {
		return nil, storage.ErrConflict
	}
	review, err := m.Review(ctx, id)
	if err != nil {
		return nil, err
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
	rec := &Record{TaskID: id, Mode: mode, Scope: scope, Actor: actor, PolicyVersion: PolicyVersion}
	if running {
		rec.ExecutionScope = review.Record.ExecutionScope
	}
	if err = m.Store.AppendEvent(ctx, domain.NewEvent("task.authorization_requested", d.GraphID, map[string]any{"taskId": id, "mode": mode, "scope": scope, "expectedRevision": revision}).WithActor(actor)); err != nil {
		return nil, err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	rec.Revision, err = m.Store.PutSetting(ctx, prefix+id, b, revision, actor)
	return rec, err
}

// Ensure is called before task side effects. Absent consent uses Ask, never Full.
func (m Manager) Ensure(ctx context.Context, id, actor string) error {
	review, err := m.Review(ctx, id)
	if err != nil {
		return err
	}
	if review.Record == nil {
		_, err = m.Save(ctx, id, Ask, review.Scope, 0, actor, true)
		return err
	}
	if review.Record.Scope != review.Scope {
		return fmt.Errorf("%w: task authorization scope changed; review and confirm again", ErrInvalid)
	}
	return nil
}

// Check is called at gateway admission and again immediately before invocation.
func (m Manager) Check(ctx context.Context, p *tools.Principal, t tools.Tool, mandatory bool) (bool, string, error) {
	if !strings.HasPrefix(p.GraphID, "task-run-") {
		return mandatory, "", nil
	}
	id := "tsk_" + strings.TrimPrefix(p.GraphID, "task-run-")
	d, err := (taskconfig.Manager{Store: m.Store}).Get(ctx, id)
	if err != nil {
		return false, "", err
	}
	if d.ExecutionGraphID != p.GraphID || (d.Status != "starting" && d.Status != "running") {
		return false, "", ErrDenied
	}
	g, err := m.Store.GetGraph(ctx, p.GraphID, 0)
	if err != nil {
		return false, "", err
	}
	if g.Metadata.Labels["weft.task"] != id || g.Metadata.Version != p.GraphVersion {
		return false, "", ErrDenied
	}
	review, err := m.Review(ctx, id)
	if err != nil {
		return false, "", err
	}
	if review.Record == nil {
		return mandatory, "legacy", nil
	} // Existing installation tasks keep their grants.
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
	required, err := Approval(rec.Mode, t.Capability(), kind, mandatory)
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
