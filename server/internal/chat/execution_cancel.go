package chat

import (
	"context"
	"github.com/woliveiras/weft/server/internal/executioncontrol"
	"github.com/woliveiras/weft/server/internal/storage"
	"time"
)

const CancelExecution = "execution.cancel"

func NewExecutionCancelReview(owner, session string, scope executioncontrol.CancelScope, now time.Time) (*OperationReview, error) {
	if !validReferenceID(scope.Graph) || !validReferenceID(scope.Run) || !validReferenceID(scope.Selected) || !validReferenceID(scope.Root) ||
		(scope.Mode != "tree" && scope.Mode != "child") || (scope.Mode == "child" && scope.Selected == scope.Root) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{ExecutionCancel: &scope})
}
func (s Sessions) RecordExecutionCancel(ctx context.Context, owner, sessionID, operation, state string) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			m := &session.Messages[i]
			r := m.Review
			if r == nil || r.ID != operation || r.ExecutionCancel == nil {
				continue
			}
			if r.State == state || (r.State != ReviewApplying && r.State != ReviewAccepted && r.State != ReviewUnknown) {
				return false, nil
			}
			r.State = state
			if state != ReviewAccepted {
				m.Unread = true
				session.LastActivity = time.Now().UTC()
			}
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}
