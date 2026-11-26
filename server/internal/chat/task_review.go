package chat

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

type TaskDraft struct {
	Title           string
	Workflow        string
	WorkflowVersion string
	Request         string
	Outcome         string
}

func (d TaskDraft) validate() error {
	if !shortLabel(d.Title) || !shortLabel(d.Workflow) || strings.TrimSpace(d.Request) == "" || !safeText(d.Request, 3000) {
		return ErrInvalidPlan
	}
	switch d.Outcome {
	case "understand", "implement", "investigate", "review", "freeform":
	default:
		return ErrInvalidPlan
	}
	if d.WorkflowVersion != "" {
		version, err := strconv.Atoi(d.WorkflowVersion)
		if err != nil || version < 1 || strconv.Itoa(version) != d.WorkflowVersion {
			return ErrInvalidPlan
		}
	}
	return nil
}

// TaskCreation records the fully bound input. The control plane derives its
// task ID from the operation and never dispatches it from this review.
type TaskCreation struct {
	Input            taskconfig.Input `json:"input"`
	WorkflowRevision string           `json:"workflowRevision"`
}

func (d TaskDraft) Input() taskconfig.Input {
	return taskconfig.Input{Title: d.Title, Request: d.Request, Outcome: d.Outcome}
}

func (d TaskDraft) MatchWorkflowVersion(version int) bool {
	return d.WorkflowVersion == "" || d.WorkflowVersion == strconv.Itoa(version)
}

func NewTaskReview(owner, session string, input taskconfig.Input, workflowRevision string, now time.Time) (*OperationReview, error) {
	if err := taskconfig.Validate(input); err != nil || workflowRevision == "" {
		return nil, fmt.Errorf("%w: invalid draft bindings", ErrInvalidPlan)
	}
	return newReview(owner, session, now, OperationReview{TaskDraft: &TaskCreation{Input: input, WorkflowRevision: workflowRevision}})
}
