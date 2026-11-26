package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woliveiras/weft/server/internal/storage"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
)

func (e *Engine) readNodeControl(ctx context.Context, graph, node string) (nodeControl, error) {
	var ctl nodeControl
	raw, err := e.store.KVGet(ctx, controlNS(graph), "node/"+node)
	if errors.Is(err, storage.ErrNotFound) {
		return ctl, nil
	}
	if err != nil {
		return ctl, err
	}
	if err = json.Unmarshal(raw, &ctl); err != nil {
		return ctl, &invalidNodeControl{node: node, err: err}
	}
	return ctl, nil
}

// GuidanceTarget checks the currently running workflow and its effective replica
// range. Task-owned copies keep their separate task controls.
func (e *Engine) GuidanceTarget(ctx context.Context, graph, node string, replica int) (*domain.GraphRecord, error) {
	if strings.HasPrefix(graph, domain.TaskRunGraphPrefix) || replica < -1 {
		return nil, ErrInvalid
	}
	g, rec, err := e.GetGraph(ctx, graph, 0)
	if err != nil {
		return nil, err
	}
	n := g.Node(node)
	if n == nil {
		return nil, ErrNotFound
	}
	if rec.DesiredState != domain.GraphRunning || rec.RunID == "" || n.Disabled {
		return nil, ErrConflict
	}
	ctl, err := e.readNodeControl(ctx, graph, node)
	if err != nil {
		return nil, err
	}
	if ctl.Stopped || ctl.Paused {
		return nil, ErrConflict
	}
	count := n.Cluster.Replicas
	if ctl.Replicas > 0 {
		count = ctl.Replicas
	}
	if n.Cluster.Mode == domain.ClusterAutoscale {
		count = n.Cluster.MaxReplicas
	}
	if replica >= count {
		return nil, ErrConflict
	}
	return rec, nil
}

func (e *Engine) SendReviewedGuidance(ctx context.Context, message *domain.OperatorMessage, version int) error {
	if message == nil || message.ID == "" || message.RunID == "" || strings.TrimSpace(message.Text) == "" {
		return ErrInvalid
	}
	return e.WithGraphTransition(ctx, message.GraphID, func(ctx context.Context) error {
		rec, err := e.GuidanceTarget(ctx, message.GraphID, message.NodeID, message.Replica)
		if err != nil {
			return err
		}
		if rec.RunID != message.RunID || rec.Version != version {
			return ErrConflict
		}
		if err := e.store.PutOperatorMessageOnce(ctx, message); err != nil {
			return err
		}
		stored, err := e.store.GetOperatorMessage(ctx, message.ID)
		if err != nil {
			return err
		}
		if stored.GraphID != message.GraphID || stored.NodeID != message.NodeID || stored.RunID != message.RunID || stored.Replica != message.Replica || stored.Text != message.Text {
			return fmt.Errorf("%w: operator message identity collision", ErrConflict)
		}
		e.bus.Publish(ctx, domain.NewEvent(domain.EvOperatorMsg, message.GraphID, map[string]any{"id": message.ID, "runId": message.RunID}).WithNode(message.NodeID, message.Replica).WithActor(message.Author))
		e.wakeOperatorMessage(message.GraphID, message.NodeID, message.Replica)
		return nil
	})
}
