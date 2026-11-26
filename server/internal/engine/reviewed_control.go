package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// WorkflowControlScope fences the exact workflow, optional node, saved
// version and lifecycle state an operator reviewed.
type WorkflowControlScope struct {
	WorkflowID      string            `json:"workflowId"`
	NodeID          string            `json:"nodeId,omitempty"`
	Version         int               `json:"version"`
	RunID           string            `json:"runId"`
	CreatedAt       time.Time         `json:"createdAt"`
	DesiredState    domain.GraphState `json:"desiredState"`
	StoppedAndIdle  bool              `json:"stoppedAndIdle,omitempty"`
	NodePaused      bool              `json:"nodePaused,omitempty"`
	NodeStopped     bool              `json:"nodeStopped,omitempty"`
	NodeDisabled    bool              `json:"nodeDisabled,omitempty"`
	ReplicaOverride int               `json:"replicaOverride,omitempty"`
}

type WorkflowControlObservation struct {
	RunID      string `json:"runId"`
	Desired    string `json:"desired"`
	Observed   string `json:"observed"`
	Settled    bool   `json:"settled"`
	Incomplete bool   `json:"incomplete,omitempty"`
}

func (e *Engine) WorkflowControlScope(ctx context.Context, workflow, node string) (WorkflowControlScope, error) {
	g, record, err := e.GetGraph(ctx, workflow, 0)
	if err != nil {
		return WorkflowControlScope{}, err
	}
	scope, _, err := e.workflowControlScope(ctx, g, record, node)
	return scope, err
}

func (e *Engine) workflowControlScope(ctx context.Context, g *domain.Graph, r *domain.GraphRecord, node string) (WorkflowControlScope, nodeControl, error) {
	scope := WorkflowControlScope{
		WorkflowID: g.Metadata.ID, NodeID: node, Version: g.Metadata.Version,
		RunID: r.RunID, CreatedAt: g.Metadata.CreatedAt.UTC(), DesiredState: r.DesiredState,
		StoppedAndIdle: r.State == domain.GraphStopped && (r.DesiredState == domain.GraphStopped || r.DesiredState == ""),
	}
	var c nodeControl
	if r.Version != g.Metadata.Version || g.Metadata.Labels[domain.LabelTask] != "" || strings.HasPrefix(scope.WorkflowID, domain.TaskRunGraphPrefix) {
		return scope, c, ErrConflict
	}
	if node == "" {
		return scope, c, nil
	}
	n := g.Node(node)
	if n == nil {
		return scope, c, ErrNotFound
	}
	scope.NodeDisabled = n.Disabled
	data, err := e.store.KVGet(ctx, controlNS(scope.WorkflowID), "node/"+node)
	if !errors.Is(err, storage.ErrNotFound) {
		if err != nil {
			return scope, c, err
		}
		if err := json.Unmarshal(data, &c); err != nil {
			return scope, c, &invalidNodeControl{node: node, err: err}
		}
	}
	scope.NodePaused, scope.NodeStopped, scope.ReplicaOverride = c.Paused, c.Stopped, c.Replicas
	return scope, c, nil
}

func (s WorkflowControlScope) CanApply(action string) bool {
	running := s.DesiredState == domain.GraphRunning
	paused := s.DesiredState == domain.GraphPaused
	stopped := s.StoppedAndIdle
	if s.NodeID == "" {
		switch action {
		case "start":
			return stopped
		case "pause":
			return running
		case "resume":
			return paused
		case "stop", "kill":
			return running || paused
		default:
			return false
		}
	}
	switch action {
	case "start":
		return !s.NodeDisabled && (stopped || (running && (s.NodeStopped || s.NodePaused)))
	case "pause":
		return running && !s.NodeStopped && !s.NodePaused
	case "resume":
		return running && !s.NodeStopped && s.NodePaused && !s.NodeDisabled
	case "stop", "kill":
		return (running || paused) && !s.NodeStopped
	default:
		return false
	}
}

// ApplyReviewedWorkflowControl rechecks the reviewed scope under the native
// lifecycle lease before applying any effect. Both paths preserve the normal
// budget, preflight, review and cancellation checks.
func (e *Engine) ApplyReviewedWorkflowControl(ctx context.Context, expected WorkflowControlScope, action, actor, operation string, before func(context.Context) error) error {
	if operation == "" {
		return ErrInvalid
	}
	return e.withGraphActionTransition(ctx, expected.WorkflowID, action, func(ctx context.Context) error {
		g, record, err := e.GetGraph(ctx, expected.WorkflowID, 0)
		if err != nil {
			return err
		}
		now, c, err := e.workflowControlScope(ctx, g, record, expected.NodeID)
		if err != nil {
			return err
		}
		if now != expected || !expected.CanApply(action) {
			return ErrConflict
		}
		if action == "start" || action == "resume" {
			if err := e.checkDeferredDeletion(ctx, expected.WorkflowID); err != nil {
				return err
			}
		}
		if before != nil {
			if err := before(ctx); err != nil {
				return err
			}
		}
		if expected.NodeID != "" {
			return e.applyNodeAction(ctx, g, record, g.Node(expected.NodeID), c, action, actor, operation)
		}
		return e.applyGraphAction(ctx, g, record, action, actor, false, operation)
	})
}

// ObserveReviewedWorkflowControl requires the native operation receipt. A
// matching state alone cannot establish who requested a transition.
func (e *Engine) ObserveReviewedWorkflowControl(ctx context.Context, target WorkflowControlScope, action, operation string) (WorkflowControlObservation, bool, error) {
	g, record, err := e.GetGraph(ctx, target.WorkflowID, 0)
	if err != nil {
		return WorkflowControlObservation{}, false, err
	}
	current, c, err := e.workflowControlScope(ctx, g, record, target.NodeID)
	if err != nil {
		return WorkflowControlObservation{}, false, err
	}
	if current.CreatedAt != target.CreatedAt || current.Version != target.Version {
		return WorkflowControlObservation{}, false, nil
	}
	desired := string(domain.GraphRunning)
	switch action {
	case "pause":
		desired = string(domain.GraphPaused)
	case "stop", "kill":
		desired = string(domain.GraphStopped)
	}
	if current.NodeID == "" {
		if record.ControlOperation != operation {
			return WorkflowControlObservation{}, false, nil
		}
		return WorkflowControlObservation{
			RunID: record.RunID, Desired: desired, Observed: string(record.State),
			Settled:    string(record.DesiredState) == desired && string(record.State) == desired,
			Incomplete: string(record.DesiredState) != desired || record.State == domain.GraphFailed,
		}, true, nil
	}
	if c.Operation != operation {
		return WorkflowControlObservation{}, false, nil
	}
	status, err := e.Status(ctx, target.WorkflowID)
	if err != nil {
		return WorkflowControlObservation{}, false, err
	}
	if status.Version != current.Version || status.RunID != current.RunID {
		return WorkflowControlObservation{}, false, nil
	}
	for _, node := range status.Nodes {
		if node.NodeID != current.NodeID {
			continue
		}
		if node.Paused != c.Paused || node.Stopped != c.Stopped {
			return WorkflowControlObservation{}, false, nil
		}
		out := WorkflowControlObservation{RunID: status.RunID, Desired: desired, Observed: string(node.State)}
		switch desired {
		case "stopped":
			out.Incomplete = !c.Stopped
			out.Settled = node.State == domain.ReplicaStopped || node.State == domain.ReplicaCompleted
		case "paused":
			out.Incomplete = !c.Paused || status.DesiredState != domain.GraphRunning
			out.Settled = node.State == domain.ReplicaPaused || node.State == domain.ReplicaStopped
		default:
			out.Incomplete = c.Paused || c.Stopped || status.DesiredState != domain.GraphRunning || node.State == domain.ReplicaFailed
			out.Settled = node.State == domain.ReplicaIdle || node.State == domain.ReplicaRunning ||
				node.State == domain.ReplicaAwaitingApproval || node.State == domain.ReplicaCompleted
		}
		out.Settled = out.Settled && !out.Incomplete
		return out, true, nil
	}
	return WorkflowControlObservation{}, false, ErrNotFound
}
