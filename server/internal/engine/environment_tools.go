package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
)

func environmentDefinition(g *domain.Graph) (string, error) {
	// EnvironmentSpec contains only JSON-serializable fields.
	data, err := json.Marshal(g.Spec.Environment)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Tool calls in a turn are serialized. A newly approved environment replaces
// the old container before commands continue; the workspace remains the same.
func (t *turnEnv) ensureEnvironment(ctx context.Context) error {
	e := t.gr.e
	if e.opt.EnvironmentProposals == nil {
		return nil
	}
	g, err := e.store.GetGraph(ctx, t.principal.GraphID, 0)
	if err != nil {
		return err
	}
	if err := e.opt.EnvironmentProposals.Check(ctx, g); err != nil {
		return err
	}
	if g.Spec.Environment == nil || t.n.Isolation.Mode != domain.IsolationDocker || t.n.Isolation.Image != "" {
		return nil
	}
	definition, err := environmentDefinition(g)
	if err != nil {
		return fmt.Errorf("environment identity: %w", err)
	}
	if t.r.sandboxDefinition == definition {
		return nil
	}
	if e.opt.Sandboxes == nil || e.opt.Workspaces == nil {
		return fmt.Errorf("environment sandbox unavailable")
	}
	if e.opt.Environments != nil {
		if err := e.opt.Environments.Check(ctx, g); err != nil {
			return err
		}
	}
	if t.r.sandbox != nil {
		if err := t.r.sandbox.Close(); err != nil {
			return fmt.Errorf("replace environment sandbox: %w", err)
		}
	}
	sb, err := e.opt.Sandboxes(ctx, SandboxSpec{Graph: g, Node: t.n, Replica: t.r.index, Workdir: t.principal.Workdir, HostWorkdir: e.opt.Workspaces.HostPath(t.principal.Workdir)})
	if err != nil {
		return fmt.Errorf("prepare approved sandbox: %w", err)
	}
	t.r.sandbox = sb
	t.r.sandboxDefinition = definition
	t.principal.Sandbox = sb
	return nil
}
