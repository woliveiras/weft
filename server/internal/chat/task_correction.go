package chat

import (
	"time"

	"github.com/woliveiras/weft/server/internal/taskconfig"
)

const CorrectTask = "task.edit"

type TaskCorrection struct {
	Task            string                `json:"task"`
	Revision        int64                 `json:"revision"`
	Workflow        string                `json:"workflow"`
	WorkflowVersion int                   `json:"workflowVersion"`
	WorkflowDigest  string                `json:"workflowDigest"`
	Before          *taskconfig.DraftText `json:"before,omitempty"`
	After           *taskconfig.DraftText `json:"after,omitempty"`
	Payload         *TaskPayloadChange    `json:"payload,omitempty"`
}

func validCorrection(text taskconfig.DraftText, partial bool) bool {
	if (!partial || text.Title != "") && !shortLabel(text.Title) {
		return false
	}
	if (!partial || text.Request != "") && (text.Request == "" || !safeText(text.Request, 3000)) {
		return false
	}
	switch text.Outcome {
	case "":
		return partial && (text.Request != "" || text.Title != "")
	case "review", "implement", "understand", "investigate", "freeform":
		return true
	default:
		return false
	}
}

func NewTaskCorrection(owner, session string, draft *taskconfig.Draft, change taskconfig.DraftText, workflowDigest string, now time.Time) (*OperationReview, error) {
	if draft == nil || draft.Revision < 1 || workflowDigest == "" || !validCorrection(change, true) {
		return nil, ErrInvalidPlan
	}
	before := draft.Text()
	if !validCorrection(before, false) {
		return nil, ErrInvalidPlan
	}
	after := before
	if change.Title != "" {
		after.Title = change.Title
	}
	if change.Request != "" {
		after.Request = change.Request
	}
	if change.Outcome != "" {
		after.Outcome = change.Outcome
	}
	if after == before {
		return nil, ErrInvalidPlan
	}
	if _, err := draft.CorrectedInput(after); err != nil {
		return nil, err
	}
	return newReview(owner, session, now, OperationReview{TaskCorrection: &TaskCorrection{Task: draft.ID, Revision: draft.Revision, Workflow: draft.GraphID, WorkflowVersion: draft.GraphVersion, WorkflowDigest: workflowDigest, Before: &before, After: &after}})
}
