package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/tools"
)

// Prefix keys one setting per trigger.
const Prefix = "triggers/"

// States.
const (
	Active   = "active"
	Paused   = "paused"
	Finished = "finished"
)

// Labels written back to issues.
const (
	LabelInProgress = "weft:in-progress"
	LabelReview     = "weft:review"
	LabelFailed     = "weft:failed"
)

// Consent is what the person who created or resumed the trigger agreed to
// let it start without them. Runs happen only while it still matches.
type Consent struct {
	Actor           string        `json:"actor"`
	WorkflowVersion int           `json:"workflowVersion"`
	ProjectID       string        `json:"projectId,omitempty"`
	ExecutionMode   string        `json:"executionMode,omitempty"`
	Authorization   taskauth.Mode `json:"authorizationMode"`
	PolicyVersion   int           `json:"policyVersion"`
	RecordedAt      time.Time     `json:"recordedAt"`
}

// Skip is an issue the last poll did not start, with the reason.
type Skip struct {
	Key    string `json:"key"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

// Trigger starts a workflow's tasks from labelled issues.
type Trigger struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Workflow        string        `json:"workflow"`
	NodeID          string        `json:"nodeId"`
	ResultNodeID    string        `json:"resultNodeId,omitempty"`
	Source          SourceConfig  `json:"source"`
	Label           string        `json:"label"`
	Owner           string        `json:"owner,omitempty"` // tracker login or account id
	IntervalMinutes int           `json:"intervalMinutes"`
	SCMConnection   string        `json:"scmConnection,omitempty"`
	ProjectID       string        `json:"projectId,omitempty"`
	ExecutionMode   string        `json:"executionMode,omitempty"`
	Authorization   taskauth.Mode `json:"authorizationMode"`
	Consent         Consent       `json:"consent"`

	State        string `json:"state"`
	PausedReason string `json:"pausedReason,omitempty"`

	LastRunAt    *time.Time        `json:"lastRunAt,omitempty"`
	NextRunAt    *time.Time        `json:"nextRunAt,omitempty"`
	CurrentTask  string            `json:"currentTask,omitempty"`
	CurrentIssue *Issue            `json:"currentIssue,omitempty"`
	Processed    map[string]string `json:"processed,omitempty"` // issue key → task id
	Skipped      []Skip            `json:"skipped,omitempty"`
	Notes        []string          `json:"notes,omitempty"`
	LastError    string            `json:"lastError,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
}

// Input creates or updates a trigger.
type Input struct {
	Name            string        `json:"name"`
	Workflow        string        `json:"workflow"`
	NodeID          string        `json:"nodeId"`
	ResultNodeID    string        `json:"resultNodeId,omitempty"`
	Source          SourceConfig  `json:"source"`
	Label           string        `json:"label"`
	Owner           string        `json:"owner,omitempty"`
	IntervalMinutes int           `json:"intervalMinutes"`
	SCMConnection   string        `json:"scmConnection,omitempty"`
	ProjectID       string        `json:"projectId,omitempty"`
	ExecutionMode   string        `json:"executionMode,omitempty"`
	Authorization   taskauth.Mode `json:"authorizationMode"`
}

// FieldError lists problems with an input.
type FieldError struct {
	Errors []struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (e *FieldError) Error() string {
	parts := []string{}
	for _, i := range e.Errors {
		parts = append(parts, i.Path+": "+i.Message)
	}
	return "invalid trigger: " + strings.Join(parts, "; ")
}

func (e *FieldError) add(path, f string, a ...any) {
	e.Errors = append(e.Errors, struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	}{path, fmt.Sprintf(f, a...)})
}

var idRe = regexp.MustCompile(`^[a-z0-9]{1,40}$`)

func validate(in *Input) error {
	fe := &FieldError{}
	in.Name, in.Label, in.Owner = strings.TrimSpace(in.Name), strings.TrimSpace(in.Label), strings.TrimSpace(in.Owner)
	if in.Name == "" || len(in.Name) > 100 {
		fe.add("name", "1-100 characters")
	}
	if in.Workflow == "" || strings.HasPrefix(in.Workflow, domain.TaskRunGraphPrefix) || len(in.Workflow) > 128 {
		fe.add("workflow", "select a saved workflow")
	}
	if in.NodeID == "" || len(in.NodeID) > 128 {
		fe.add("nodeId", "select the entry agent")
	}
	if _, err := in.Source.Open(); err != nil {
		fe.add("source", "%s", err.Error())
	}
	if !ValidLabel(in.Label) || strings.HasPrefix(strings.ToLower(in.Label), "weft:") {
		fe.add("label", "use 1-50 letters, digits, spaces or _.:/- (not a weft: status label)")
	}
	if len(in.Owner) > 128 || strings.ContainsAny(in.Owner, " \r\n\"") {
		fe.add("owner", "a tracker login or account id")
	}
	if in.IntervalMinutes == 0 {
		in.IntervalMinutes = 15
	}
	if in.IntervalMinutes < 5 || in.IntervalMinutes > 1440 {
		fe.add("intervalMinutes", "between 5 and 1440")
	}
	if in.Authorization == "" {
		in.Authorization = taskauth.Ask
	}
	if !taskauth.Valid(in.Authorization) {
		fe.add("authorizationMode", "ask, workspace or full")
	}
	switch in.ExecutionMode {
	case "", "workflow", "local", "sandbox":
	default:
		fe.add("executionMode", "workflow, local or sandbox")
	}
	if len(fe.Errors) > 0 {
		return fe
	}
	return nil
}

// Manager stores triggers.
type Manager struct {
	Store storage.Store
	Bus   *events.Bus // optional
	Now   func() time.Time
	Tools *tools.Registry
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m Manager) read(ctx context.Context, id string) (*Trigger, int64, error) {
	st, err := m.Store.GetSetting(ctx, Prefix+id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, 0, fmt.Errorf("%w: trigger %q", storage.ErrNotFound, id)
		}
		return nil, 0, err
	}
	var t Trigger
	if err := json.Unmarshal(st.Value, &t); err != nil {
		return nil, 0, errors.New("trigger: a stored trigger is corrupt")
	}
	return &t, st.Revision, nil
}

// Get returns a trigger.
func (m Manager) Get(ctx context.Context, id string) (*Trigger, error) {
	t, _, err := m.read(ctx, id)
	return t, err
}

// List returns every trigger, optionally for one workflow.
func (m Manager) List(ctx context.Context, workflow string) ([]*Trigger, error) {
	rows, err := m.Store.ListSettings(ctx, Prefix)
	if err != nil {
		return nil, err
	}
	out := []*Trigger{}
	for _, r := range rows {
		var t Trigger
		if json.Unmarshal(r.Value, &t) != nil {
			continue
		}
		if workflow == "" || t.Workflow == workflow {
			out = append(out, &t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Update applies fn with compare-and-set.
func (m Manager) Update(ctx context.Context, id, actor string, fn func(t *Trigger) error) (*Trigger, error) {
	for i := 0; i < 10; i++ {
		t, rev, err := m.read(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := fn(t); err != nil {
			return nil, err
		}
		t.UpdatedAt, t.UpdatedBy = m.now(), actor
		raw, _ := json.Marshal(t)
		if _, err = m.Store.PutSetting(ctx, Prefix+id, raw, rev, actor); errors.Is(err, storage.ErrConflict) {
			continue
		} else if err != nil {
			return nil, err
		}
		return t, nil
	}
	return nil, fmt.Errorf("%w: trigger %q is being changed concurrently", storage.ErrConflict, id)
}

// authorizationProblem is shared by consent writes and the runner's preflight.
func (m Manager) authorizationProblem(mode taskauth.Mode, g *domain.Graph) string {
	if !taskauth.Valid(mode) {
		return "ask, workspace or full"
	}
	if mode == taskauth.Workspace {
		graph.Normalize(g)
		if err := taskauth.ValidateMode(mode, g.Spec.Nodes, m.Tools); err != nil {
			return err.Error()
		}
	}
	return ""
}

// consent records what the actor agrees to now.
func (m Manager) consent(ctx context.Context, in Input, actor string) (Consent, error) {
	g, err := m.Store.GetGraph(ctx, in.Workflow, 0)
	if errors.Is(err, storage.ErrNotFound) {
		fe := &FieldError{}
		fe.add("workflow", "the workflow does not exist")
		return Consent{}, fe
	}
	if err != nil {
		return Consent{}, err
	}
	fe := &FieldError{}
	if g.Node(in.NodeID) == nil {
		fe.add("nodeId", "the entry agent is not in the workflow")
	}
	if in.ResultNodeID != "" && g.Node(in.ResultNodeID) == nil {
		fe.add("resultNodeId", "the result agent is not in the workflow")
	}
	if len(fe.Errors) > 0 {
		return Consent{}, fe
	}
	if problem := m.authorizationProblem(in.Authorization, g); problem != "" {
		fe := &FieldError{}
		fe.add("authorizationMode", "%s", problem)
		return Consent{}, fe
	}
	return Consent{Actor: actor, WorkflowVersion: g.Metadata.Version, ProjectID: in.ProjectID, ExecutionMode: in.ExecutionMode,
		Authorization: in.Authorization, PolicyVersion: taskauth.PolicyVersion, RecordedAt: m.now()}, nil
}

// Create stores a new active trigger with the creator's consent.
func (m Manager) Create(ctx context.Context, in Input, actor string) (*Trigger, error) {
	if err := validate(&in); err != nil {
		return nil, err
	}
	c, err := m.consent(ctx, in, actor)
	if err != nil {
		return nil, err
	}
	now := m.now()
	t := &Trigger{ID: domain.NewID("trg"), State: Active, Consent: c, Processed: map[string]string{}, CreatedAt: now, UpdatedAt: now, UpdatedBy: actor}
	apply(t, in)
	if !idRe.MatchString(strings.TrimPrefix(t.ID, "trg_")) {
		return nil, errors.New("trigger: unexpected id")
	}
	raw, _ := json.Marshal(t)
	if _, err := m.Store.PutSetting(ctx, Prefix+t.ID, raw, 0, actor); err != nil {
		return nil, err
	}
	m.audit(ctx, t, "created", actor)
	return t, nil
}

func apply(t *Trigger, in Input) {
	t.Name, t.Workflow, t.NodeID, t.ResultNodeID, t.Source, t.Label, t.Owner = in.Name, in.Workflow, in.NodeID, in.ResultNodeID, in.Source, in.Label, in.Owner
	t.IntervalMinutes, t.SCMConnection, t.ProjectID, t.ExecutionMode, t.Authorization = in.IntervalMinutes, in.SCMConnection, in.ProjectID, in.ExecutionMode, in.Authorization
}

// Edit changes a trigger. A change to what the consent covers (workflow,
// entry or result agent, project, environment, authorization, source or
// delivery) pauses it until a person resumes it and records new consent.
func (m Manager) Edit(ctx context.Context, id string, in Input, actor string) (*Trigger, error) {
	if err := validate(&in); err != nil {
		return nil, err
	}
	t, err := m.Update(ctx, id, actor, func(t *Trigger) error {
		if _, err := m.consent(ctx, in, actor); err != nil {
			return err
		}
		covered := t.Workflow != in.Workflow || t.NodeID != in.NodeID || t.ResultNodeID != in.ResultNodeID || t.ProjectID != in.ProjectID ||
			t.ExecutionMode != in.ExecutionMode || t.Authorization != in.Authorization || t.Source != in.Source || t.SCMConnection != in.SCMConnection
		apply(t, in)
		if covered && t.State == Active {
			t.State, t.PausedReason = Paused, "The trigger changed what was consented to. Review it and resume to record new consent."
		}
		return nil
	})
	if err == nil {
		m.audit(ctx, t, "updated", actor)
	}
	return t, err
}

// Pause stops a trigger from starting tasks.
func (m Manager) Pause(ctx context.Context, id, reason, actor string) (*Trigger, error) {
	t, err := m.Update(ctx, id, actor, func(t *Trigger) error {
		if t.State == Finished {
			return fmt.Errorf("%w: the trigger is finished", storage.ErrConflict)
		}
		t.State, t.PausedReason = Paused, reason
		return nil
	})
	if err == nil {
		m.audit(ctx, t, "paused", actor)
	}
	return t, err
}

// Resume records the actor's consent for the trigger as it is now and
// makes it active. A task still running from before is kept.
func (m Manager) Resume(ctx context.Context, id, actor string) (*Trigger, error) {
	t, err := m.Update(ctx, id, actor, func(t *Trigger) error {
		in := Input{Name: t.Name, Workflow: t.Workflow, NodeID: t.NodeID, ResultNodeID: t.ResultNodeID, Source: t.Source, Label: t.Label,
			Owner: t.Owner, IntervalMinutes: t.IntervalMinutes, SCMConnection: t.SCMConnection, ProjectID: t.ProjectID, ExecutionMode: t.ExecutionMode, Authorization: t.Authorization}
		c, err := m.consent(ctx, in, actor)
		if err != nil {
			return err
		}
		t.Consent, t.State, t.PausedReason, t.LastError, t.NextRunAt = c, Active, "", "", nil
		return nil
	})
	if err == nil {
		m.audit(ctx, t, "resumed", actor)
	}
	return t, err
}

// Delete removes a trigger.
func (m Manager) Delete(ctx context.Context, id, actor string) error {
	t, rev, err := m.read(ctx, id)
	if err != nil {
		return err
	}
	if err := m.Store.DeleteSetting(ctx, Prefix+id, rev); err != nil {
		return err
	}
	m.audit(ctx, t, "deleted", actor)
	return nil
}

// ConsentProblem says why the consent no longer covers a run, or "".
func (m Manager) ConsentProblem(ctx context.Context, t *Trigger) (string, error) {
	g, err := m.Store.GetGraph(ctx, t.Workflow, 0)
	if errors.Is(err, storage.ErrNotFound) {
		return "The workflow was deleted.", nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case g.Metadata.Version != t.Consent.WorkflowVersion:
		return fmt.Sprintf("The workflow changed (version %d, consented to %d). Review it and resume to record new consent.", g.Metadata.Version, t.Consent.WorkflowVersion), nil
	case t.Consent.PolicyVersion != taskauth.PolicyVersion:
		return "The task authorization policy changed. Review the trigger and resume to record new consent.", nil
	case t.Consent.ProjectID != t.ProjectID || t.Consent.ExecutionMode != t.ExecutionMode || t.Consent.Authorization != t.Authorization:
		return "The trigger changed what was consented to. Review it and resume to record new consent.", nil
	}
	return m.authorizationProblem(t.Authorization, g), nil
}

func (m Manager) audit(ctx context.Context, t *Trigger, action, actor string) {
	if m.Bus != nil && t != nil {
		m.Bus.Publish(context.WithoutCancel(ctx), domain.NewEvent(domain.EvTriggerChanged, "", map[string]any{"trigger": t.ID, "action": action, "state": t.State, "workflow": t.Workflow}).WithActor(actor))
	}
}
