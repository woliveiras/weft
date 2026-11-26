package chat

import (
	"time"

	"github.com/woliveiras/weft/server/internal/trigger"
)

const ControlTrigger = "trigger.control"

type TriggerControlDraft struct{ Target, Action string }

func (d TriggerControlDraft) valid() bool {
	return shortLabel(d.Target) && (d.Action == "pause" || d.Action == "resume")
}

func NewTriggerControlReview(owner, session string, scope trigger.IntakeScope, now time.Time) (*OperationReview, error) {
	if !shortLabel(scope.ID) || !shortLabel(scope.Name) || !shortLabel(scope.Actor) ||
		!shortLabel(scope.Connection) || !shortLabel(scope.Workflow) ||
		(scope.Action != "pause" && scope.Action != "resume") || scope.Configuration == "" ||
		(scope.Action == "resume" && (scope.ConsentScope == "" || scope.WorkflowVersion < 1)) || !safeText(scope.Query, 500) {
		return nil, ErrInvalidPlan
	}
	for _, value := range []string{scope.Node, scope.ResultNode, scope.SourceKind, scope.SourceProject, scope.Label, scope.Owner,
		scope.Project, scope.ExecutionMode, scope.Authorization, scope.SCM, scope.CurrentTask, scope.Claiming} {
		if value != "" && !shortLabel(value) {
			return nil, ErrInvalidPlan
		}
	}
	for _, operation := range scope.Operations {
		if !shortLabel(operation) {
			return nil, ErrInvalidPlan
		}
	}
	return newReview(owner, session, now, OperationReview{TriggerControl: &scope})
}
