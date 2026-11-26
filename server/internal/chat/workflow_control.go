package chat

import (
	"context"
	"time"

	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
)

const ControlWorkflow = "workflow.control"

type WorkflowControlDraft struct {
	Workflow string
	Node     string
	Action   string
}

func (d WorkflowControlDraft) valid() bool {
	if !shortLabel(d.Workflow) || (d.Node != "" && !shortLabel(d.Node)) {
		return false
	}
	switch d.Action {
	case "start", "pause", "resume", "stop", "kill":
		return true
	default:
		return false
	}
}

type WorkflowControl struct {
	Scope  engine.WorkflowControlScope `json:"scope"`
	Action string                      `json:"action"`
}

func NewWorkflowControlReview(owner, session string, control WorkflowControl, now time.Time) (*OperationReview, error) {
	if control.Scope.WorkflowID == "" || control.Scope.Version < 1 || !control.Scope.CanApply(control.Action) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{WorkflowControl: &control})
}

// RecordWorkflowControl updates a review from native desired and observed
// state. Overlapping observers cannot replace a newer snapshot.
func (s Sessions) RecordWorkflowControl(ctx context.Context, owner, sessionID string, prior *OperationReview, next string, result *engine.WorkflowControlObservation) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			message := &session.Messages[i]
			review := message.Review
			if review == nil || review.ID != prior.ID || review.WorkflowControl == nil {
				continue
			}
			observedFirst := prior.State == ReviewApplying && next == ReviewIncomplete &&
				(review.State == ReviewAccepted || review.State == ReviewApplied)
			stale := review.State != prior.State || !equalControlOutcome(review.ControlResult, prior.ControlResult)
			if (stale && !observedFirst) || (review.State == next && equalControlOutcome(review.ControlResult, result)) {
				return false, nil
			}
			changedState := review.State != next
			review.State, review.ControlResult = next, result
			if changedState {
				now := time.Now().UTC()
				message.Unread, session.LastActivity = true, now
			}
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}

func equalControlOutcome(a, b *engine.WorkflowControlObservation) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
