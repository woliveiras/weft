package chat

import (
	"context"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskauth"
)

type TaskAdmission struct {
	ID          string                `json:"id"`
	Title       string                `json:"title"`
	WorkflowID  string                `json:"workflowId"`
	Version     int                   `json:"workflowVersion"`
	Revision    int64                 `json:"taskRevision"`
	Consent     taskauth.StartConsent `json:"consent"`
	AgentGrants []AgentStartGrant     `json:"agentGrants"`
}

// FinishTaskStart records admission separately from the observed task outcome.
// This can recover a lost response from the task's durable dispatch receipt.
func (s Sessions) FinishTaskStart(ctx context.Context, owner, sessionID, reviewID string, task TaskSummary) (*Session, error) {
	return s.finishTrackedTaskAction(ctx, owner, sessionID, reviewID, task)
}

func (s Sessions) finishTrackedTaskAction(ctx context.Context, owner, sessionID, reviewID string, task TaskSummary) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			message := &session.Messages[i]
			review := message.Review
			if review == nil || review.ID != reviewID ||
				!((review.TaskStart != nil && review.TaskStart.ID == task.ID) || (review.TaskCancel != nil && review.TaskCancel.ID == task.ID)) {
				continue
			}
			if review.State != ReviewApplying && review.State != ReviewUnknown {
				return false, nil
			}
			now := time.Now().UTC()
			review.State, review.ResultID = ReviewAccepted, task.ID
			message.Unread, session.LastActivity = true, now
			for _, existing := range session.Watches {
				if existing.Task.ID == task.ID && !existing.Complete {
					return true, nil
				}
			}
			if task.Terminal() {
				message.Task = &task
			} else {
				session.Watches = append(session.Watches, Watch{RequestID: reviewID, Task: task, Observed: now})
			}
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}

type AgentStartGrant struct {
	ID        string   `json:"id"`
	Isolation string   `json:"isolation"`
	Tools     []string `json:"tools,omitempty"`
}

func NewTaskStartReview(owner, session string, start TaskAdmission, now time.Time) (*OperationReview, error) {
	if start.ID == "" || start.WorkflowID == "" || start.Version < 1 || start.Revision < 1 ||
		start.Consent.Scope == "" || !taskauth.Valid(start.Consent.Mode) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{TaskStart: &start})
}
