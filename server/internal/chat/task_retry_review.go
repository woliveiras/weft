package chat

import (
	"context"
	"time"
)

const RetryTask = "task.retry"

type TaskRetry struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Revision        int64  `json:"revision"`
	WorkflowID      string `json:"workflowId"`
	WorkflowVersion int    `json:"workflowVersion"`
	RepositoryCount int    `json:"repositoryCount"`
}

func NewTaskRetryReview(owner, session string, retry TaskRetry, now time.Time) (*OperationReview, error) {
	if retry.ID == "" || retry.Revision < 1 || retry.WorkflowID == "" || retry.WorkflowVersion < 1 {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{TaskRetry: &retry})
}

func (s Sessions) FinishTaskRetry(ctx context.Context, owner, sessionID, reviewID, taskID string, revision int64) (*Session, error) {
	return s.finishReview(ctx, owner, sessionID, reviewID, ReviewCreated, int(revision), taskID, nil)
}
