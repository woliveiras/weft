package engine

import (
	"context"

	"github.com/woliveiras/weft/server/internal/domain"
)

type WorkflowScaleScope struct {
	WorkflowControlScope
	Autoscale bool `json:"autoscale"`
	Minimum   int  `json:"minimum"`
	Current   int  `json:"current"`
}

func (s WorkflowScaleScope) Allows(next int) bool {
	return next >= s.Minimum && next <= 256 && (next != s.Current || next == 0 && !s.NodeStopped) && s.NodeID != "" &&
		(s.StoppedAndIdle || s.DesiredState == domain.GraphRunning || s.DesiredState == domain.GraphPaused)
}

type WorkflowScaleObservation struct {
	Version int    `json:"version"`
	Active  int    `json:"activeReplicas"`
	State   string `json:"state"`
}

func (e *Engine) WorkflowScaleScope(ctx context.Context, workflow, node string) (WorkflowScaleScope, error) {
	g, record, err := e.GetGraph(ctx, workflow, 0)
	if err != nil {
		return WorkflowScaleScope{}, err
	}
	scope, _, err := e.workflowScaleScope(ctx, g, record, node)
	return scope, err
}

func (e *Engine) workflowScaleScope(ctx context.Context, g *domain.Graph, record *domain.GraphRecord, node string) (WorkflowScaleScope, nodeControl, error) {
	control, c, err := e.workflowControlScope(ctx, g, record, node)
	if err != nil {
		return WorkflowScaleScope{}, c, err
	}
	n := g.Node(node)
	if n == nil {
		return WorkflowScaleScope{}, c, ErrNotFound
	}
	scope := WorkflowScaleScope{WorkflowControlScope: control, Current: n.Cluster.Replicas}
	if n.Cluster.Mode == domain.ClusterAutoscale {
		scope.Autoscale, scope.Minimum, scope.Current = true, n.Cluster.MinReplicas, n.Cluster.MaxReplicas
	} else if c.Replicas > 0 {
		scope.Current = c.Replicas
	} else if c.Stopped {
		scope.Current = 0
	}
	return scope, c, nil
}

func (e *Engine) checkReviewedScale(ctx context.Context, g *domain.Graph, record *domain.GraphRecord, expected WorkflowScaleScope, next int, actor string) (nodeControl, error) {
	now, c, err := e.workflowScaleScope(ctx, g, record, expected.NodeID)
	if err != nil {
		return c, err
	}
	if now != expected || !now.Allows(next) {
		return c, ErrConflict
	}
	if err := e.checkDeferredDeletion(ctx, expected.WorkflowID); err != nil {
		return c, err
	}
	if next > now.Current && now.DesiredState == domain.GraphRunning && !c.Stopped && !c.Paused && !now.NodeDisabled {
		return c, e.checkManualBudgetResume(ctx, g, record.RunID, actor, "scale", expected.NodeID)
	}
	return c, nil
}

func (e *Engine) ApplyReviewedWorkflowScale(ctx context.Context, expected WorkflowScaleScope, next int, actor, operation string) error {
	if operation == "" || !expected.Allows(next) {
		return ErrInvalid
	}
	if expected.Autoscale {
		doc, _, err := e.GetGraphDocument(ctx, expected.WorkflowID, expected.Version)
		if err != nil {
			return err
		}
		node := doc.Node(expected.NodeID)
		if node == nil {
			return ErrNotFound
		}
		node.Cluster.MaxReplicas = next
		_, _, err = e.SaveReviewedGraph(ctx, doc, expected.Version, actor, GraphSaveReview{CreatedAt: expected.CreatedAt, Operation: operation, scale: &expected})
		return err
	}
	return e.WithGraphTransition(ctx, expected.WorkflowID, func(ctx context.Context) error {
		g, record, err := e.GetGraph(ctx, expected.WorkflowID, 0)
		if err != nil {
			return err
		}
		c, err := e.checkReviewedScale(ctx, g, record, expected, next, actor)
		if err != nil {
			return err
		}
		return e.scaleFixed(ctx, g, expected.NodeID, next, c, actor, operation)
	})
}

// ObserveReviewedWorkflowScale reports counts from the reviewed workflow and
// run only after finding the operation in the native graph or node record.
func (e *Engine) ObserveReviewedWorkflowScale(ctx context.Context, expected WorkflowScaleScope, next int, operation string) (WorkflowScaleObservation, bool, error) {
	g, record, err := e.GetGraph(ctx, expected.WorkflowID, 0)
	if err != nil {
		return WorkflowScaleObservation{}, false, err
	}
	now, c, err := e.workflowScaleScope(ctx, g, record, expected.NodeID)
	if err != nil {
		return WorkflowScaleObservation{}, false, err
	}
	if now.CreatedAt != expected.CreatedAt || now.Autoscale != expected.Autoscale {
		return WorkflowScaleObservation{}, false, nil
	}
	if expected.Autoscale {
		if record.ChangeOperation != operation || record.Version != expected.Version+1 || now.Current != next {
			return WorkflowScaleObservation{}, false, nil
		}
	} else if c.Operation != operation || c.Replicas != next || record.Version != expected.Version {
		return WorkflowScaleObservation{}, false, nil
	}
	result := WorkflowScaleObservation{Version: record.Version, State: "converging"}
	if record.RunID != expected.RunID || record.DesiredState != expected.DesiredState || record.State == domain.GraphFailed {
		result.State = "superseded"
		return result, true, nil
	}
	replicas, err := e.store.ListReplicaStatus(ctx, expected.WorkflowID)
	if err != nil {
		return result, false, err
	}
	var activeTooHigh, transitioning, failed bool
	for _, replica := range replicas {
		if replica.NodeID != expected.NodeID || record.StartedAt != nil && replica.UpdatedAt.Before(*record.StartedAt) {
			continue
		}
		switch replica.State {
		case domain.ReplicaCompleted, domain.ReplicaStopped:
			continue
		case domain.ReplicaFailed:
			failed = failed || replica.Replica < next
			continue
		case domain.ReplicaStarting, domain.ReplicaStopping:
			transitioning = true
		}
		result.Active++
		activeTooHigh = activeTooHigh || replica.Replica >= next
	}
	switch {
	case failed:
		result.State = "failed"
	case now.StoppedAndIdle || now.NodeStopped || now.NodeDisabled:
		if result.Active == 0 {
			result.State = "saved"
		}
	case !transitioning && !activeTooHigh && (expected.Autoscale || result.Active == next):
		result.State = "observed"
	}
	return result, true, nil
}
