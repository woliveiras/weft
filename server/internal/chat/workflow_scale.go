package chat

import (
	"context"
	"strconv"
	"time"

	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/storage"
)

const ScaleWorkflowNode = "workflow.scale"

type WorkflowScaleDraft struct{ Workflow, Node, Size, Mode string }

func (d WorkflowScaleDraft) valid() bool {
	return shortLabel(d.Workflow) && shortLabel(d.Node) && (d.Mode == "fixed" || d.Mode == "autoscale") && d.count() >= 0
}

func (d WorkflowScaleDraft) count() int {
	n, err := strconv.Atoi(d.Size)
	if err != nil || n < 0 || n > 256 || strconv.Itoa(n) != d.Size {
		return -1
	}
	return n
}

func (d WorkflowScaleDraft) SizeFor(scope engine.WorkflowScaleScope) (int, bool) {
	n := d.count()
	return n, (d.Mode == "autoscale") == scope.Autoscale && scope.Allows(n)
}

type WorkflowScale struct {
	Scope engine.WorkflowScaleScope `json:"scope"`
	Size  int                       `json:"size"`
}

func NewWorkflowScaleReview(owner, session string, scale WorkflowScale, now time.Time) (*OperationReview, error) {
	if !shortLabel(scale.Scope.WorkflowID) || !shortLabel(scale.Scope.NodeID) || scale.Scope.Version < 1 || !scale.Scope.Allows(scale.Size) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{WorkflowScale: &scale})
}

func (s Sessions) RecordWorkflowScale(ctx context.Context, owner, sessionID, id, next string, result engine.WorkflowScaleObservation) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			message := &session.Messages[i]
			review := message.Review
			if review == nil || review.ID != id || review.WorkflowScale == nil {
				continue
			}
			if review.State != ReviewApplying && review.State != ReviewAccepted && review.State != ReviewUnknown {
				return false, nil
			}
			if review.State == next {
				if review.ScaleResult == nil && result.Version == 0 || review.ScaleResult != nil && *review.ScaleResult == result {
					return false, nil
				}
			}
			review.State = next
			if result.Version > 0 {
				review.ScaleResult = &result
			}
			if next != ReviewAccepted {
				message.Unread, session.LastActivity = true, time.Now().UTC()
			}
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}
