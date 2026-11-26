package engine

import (
	"context"
	"errors"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// CancelReviewedTaskRun finishes the durable cancellation of one admitted
// attempt. The task lifecycle can call it again after a controller restart.
func (e *Engine) CancelReviewedTaskRun(ctx context.Context, graphID, taskID, runID, actor, operation string) error {
	return e.withGraphActionTransition(ctx, graphID, "kill", func(ctx context.Context) error {
		workflow, record, err := e.GetGraph(ctx, graphID, 0)
		if err != nil {
			return err
		}
		if graphID != domain.TaskExecutionID(taskID) || workflow.Metadata.Labels[domain.LabelTask] != taskID ||
			runID == "" || record.RunID != runID || operation == "" {
			return ErrConflict
		}
		if record.DesiredState != domain.GraphStopped || (record.State != domain.GraphStopped && record.ControlOperation != operation) {
			if err := e.applyGraphAction(ctx, workflow, record, "kill", actor, false, operation); err != nil {
				return err
			}
		}
		pending, err := e.store.ListApprovals(ctx, storage.ApprovalFilter{GraphID: graphID, Status: domain.ApprovalPending, Limit: 1000})
		if err != nil {
			return err
		}
		if len(pending) > 0 && e.opt.Approvals == nil {
			return errors.New("cannot settle pending approvals without the approval service")
		}
		for _, request := range pending {
			if _, err := e.opt.Approvals.CancelPending(ctx, request.ID); err != nil {
				return err
			}
		}
		return nil
	})
}
