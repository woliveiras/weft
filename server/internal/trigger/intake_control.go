package trigger

import (
	"context"

	"github.com/woliveiras/weft/server/internal/storage"
)

// IntakeScope binds the saved trigger and the native consent applicable to
// the next intake. Transient polling counters cannot invalidate a decision.
type IntakeScope struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Action          string   `json:"action"`
	Actor           string   `json:"actor"`
	Configuration   string   `json:"configuration"`
	ConsentScope    string   `json:"consentScope,omitempty"`
	Workflow        string   `json:"workflow"`
	WorkflowVersion int      `json:"workflowVersion"`
	Node            string   `json:"node"`
	ResultNode      string   `json:"resultNode,omitempty"`
	SourceKind      string   `json:"sourceKind"`
	Connection      string   `json:"connection"`
	SourceProject   string   `json:"sourceProject"`
	Query           string   `json:"query,omitempty"`
	Label           string   `json:"label"`
	Owner           string   `json:"owner,omitempty"`
	Project         string   `json:"project,omitempty"`
	ExecutionMode   string   `json:"executionMode,omitempty"`
	Authorization   string   `json:"authorization"`
	SCM             string   `json:"scm,omitempty"`
	IntervalMinutes int      `json:"intervalMinutes"`
	CurrentTask     string   `json:"currentTask,omitempty"`
	Claiming        string   `json:"claiming,omitempty"`
	Operations      []string `json:"operations,omitempty"`
}

func allowsIntakeControl(state, action string) bool {
	return state == Active && action == "pause" || state == Paused && action == "resume"
}

func (r IntakeScope) matches(t *Trigger, actor, action string) bool {
	return r.ID == t.ID && r.Actor == actor && r.Action == action && allowsIntakeControl(t.State, action) &&
		r.Configuration == ConfigurationRevision(t) && r.CurrentTask == t.CurrentTask && r.Claiming == t.Claiming
}

func (m Manager) ReviewIntakeControl(ctx context.Context, t *Trigger, action, actor string) (IntakeScope, error) {
	if !allowsIntakeControl(t.State, action) {
		return IntakeScope{}, storage.ErrConflict
	}
	consent := t.Consent
	if action == "resume" {
		var err error
		consent, err = m.consentWithMappings(ctx, triggerInput(t), actor, t.CurrentTask == "")
		if err != nil {
			return IntakeScope{}, err
		}
	}
	target := t.Source.Repository
	if target == "" {
		target = t.Source.ProjectID
	}
	if target == "" {
		target = t.Source.CloudID
	}
	return IntakeScope{ID: t.ID, Name: t.Name, Action: action, Actor: actor,
		Configuration: ConfigurationRevision(t), ConsentScope: consent.Scope, Workflow: t.Workflow, WorkflowVersion: consent.WorkflowVersion,
		Node: t.NodeID, ResultNode: t.ResultNodeID, SourceKind: t.Source.Kind, Connection: t.Source.ConnectionID, SourceProject: target,
		Query: t.Source.Query, Label: t.Label, Owner: t.Owner, IntervalMinutes: t.IntervalMinutes, Project: t.ProjectID,
		ExecutionMode: t.ExecutionMode, Authorization: string(t.Authorization), SCM: t.SCMConnection,
		CurrentTask: t.CurrentTask, Claiming: t.Claiming, Operations: consent.Operations}, nil
}

func (m Manager) ApplyReviewedIntake(ctx context.Context, review IntakeScope, actor, operation string) (*Trigger, error) {
	if operation == "" || review.Configuration == "" || actor != review.Actor {
		return nil, storage.ErrConflict
	}
	switch review.Action {
	case "pause":
		return m.pause(ctx, review.ID, "Paused by "+actor+".", actor, &review, operation)
	case "resume":
		return m.resume(ctx, review.ID, actor, &review, operation)
	default:
		return nil, storage.ErrConflict
	}
}
