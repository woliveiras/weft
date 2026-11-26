package chat

import (
	"context"
	"time"
)

const CancelTask = "task.cancel"

type TaskCancellation struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Revision         int64  `json:"revision"`
	ExecutionGraphID string `json:"executionGraphId"`
	ExecutionVersion int    `json:"executionVersion"`
	RunID            string `json:"runId"`
	PendingApprovals int    `json:"pendingApprovals"`
}

func NewTaskCancellationReview(owner, session string, request TaskCancellation, now time.Time) (*OperationReview, error) {
	if request.ID == "" || request.Revision < 1 || request.ExecutionGraphID == "" || request.ExecutionVersion < 1 || request.RunID == "" {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{TaskCancel: &request})
}

func (s Sessions) FinishTaskCancel(ctx context.Context, owner, sessionID, reviewID string, task TaskSummary) (*Session, error) {
	return s.finishTrackedTaskAction(ctx, owner, sessionID, reviewID, task)
}
