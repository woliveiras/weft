package agentcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
)

func workflowAgentID(workflow, node string) string {
	digest := sha256.Sum256([]byte(workflow + "\x00" + node))
	return fmt.Sprintf("%s%x", workflowAgentPrefix, digest[:16])
}

// RegisterWorkflow snapshots previously unregistered agents from a saved
// workflow. The catalog survives later edits or deletion of the source.
func (m Manager) RegisterWorkflow(ctx context.Context, g *domain.Graph, actor string) error {
	if strings.HasPrefix(g.Metadata.ID, domain.TaskRunGraphPrefix) || g.Metadata.Labels[domain.LabelTask] != "" {
		return nil
	}
	var failures []error
	for i := range g.Spec.Nodes {
		node := &g.Spec.Nodes[i]
		if node.Agent != nil {
			continue
		}
		if err := m.registerNode(ctx, g, node, actor); err != nil {
			failures = append(failures, fmt.Errorf("register %s/%s: %w", g.Metadata.ID, node.ID, err))
		}
	}
	return errors.Join(failures...)
}

func (m Manager) registerNode(ctx context.Context, g *domain.Graph, node *domain.Node, actor string) error {
	id := workflowAgentID(g.Metadata.ID, node.ID)
	_, err := m.Store.GetSetting(ctx, prefix+id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	definition := FromNode(g, node)
	// Source workflow values are local. The new workflow supplies its own
	// values when it chooses this agent.
	for i := range definition.Parameters {
		definition.Parameters[i].Required = definition.Parameters[i].Default != ""
		definition.Parameters[i].Default = ""
	}
	entry := Agent{ID: id, Origin: &Origin{Workflow: g.Metadata.ID, Node: node.ID}, Versions: []Version{{
		Version: 1, Definition: definition, CreatedAt: time.Now().UTC(), CreatedBy: actor,
	}}}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = m.Store.PutSetting(ctx, prefix+id, raw, 0, actor)
	if errors.Is(err, storage.ErrConflict) {
		// Another instance won registration of the same workflow node.
		return nil
	}
	return err
}

// RegisterExistingWorkflows backfills saved workflows and repairs a registration
// interrupted after the graph commit. It never writes back to a graph.
func (m Manager) RegisterExistingWorkflows(ctx context.Context) error {
	workflows, err := m.Store.ListGraphs(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, workflow := range workflows {
		graph.Normalize(workflow)
		if err := m.RegisterWorkflow(ctx, workflow, "workflow"); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
