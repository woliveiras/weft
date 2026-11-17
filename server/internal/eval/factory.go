package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/localinstall"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Workflow selects what runs a case: one coding agent, or the Software
// Factory v2 (NAT-10).
type Workflow string

const (
	Single  Workflow = "single"
	Factory Workflow = "factory"
)

// factoryQuiet is how long a factory run may sit with every execution
// finished before it counts as stopped short of the hand-off.
var factoryQuiet = 15 * time.Second

// FactoryGraph adapts Software Factory v2 to an evaluation case: every agent
// uses the measured model, the case's Go test is the declared "test" check,
// shell grants are limited to go and gofmt, and the hand-off needs no human
// approval (the hidden tests decide).
func FactoryGraph(id, provider, model string, maxToolCalls int) (*domain.Graph, error) {
	g, err := localinstall.FactoryV2Graph()
	if err != nil {
		return nil, err
	}
	g.Metadata.ID, g.Metadata.Name, g.Metadata.Labels = id, id, nil
	g.Spec.Checks = []domain.Check{{Name: "test", Command: "go test ./..."}}
	for i := range g.Spec.Nodes {
		n := &g.Spec.Nodes[i]
		n.Model = domain.ModelRef{Provider: provider, Name: model}
		if maxToolCalls > 0 {
			n.Limits.MaxToolCalls = maxToolCalls
		}
		for j := range n.Tools {
			if n.Tools[j].Name == "shell.exec" {
				n.Tools[j].Commands = []string{"go *", "gofmt *"}
			}
		}
	}
	release := g.Channel("release")
	if release == nil {
		return nil, errors.New("software factory v2 has no release channel")
	}
	release.Approval = false
	return g, nil
}

// runFactory runs a case through the factory and fills res with the totals
// of every agent's executions. It returns the workspace directory.
func runFactory(ctx context.Context, a *app.App, workspaces, id string, c Case, provider, model string, maxToolCalls int, timeout time.Duration, res *Result) (string, error) {
	dir := filepath.Join(workspaces, id)
	if err := copyFixture(c, "repo", dir); err != nil {
		return dir, fmt.Errorf("fixture: %w", err)
	}
	g, err := FactoryGraph(id, provider, model, maxToolCalls)
	if err != nil {
		return dir, fmt.Errorf("factory graph: %w", err)
	}
	if _, _, err := a.Engine.SaveGraph(ctx, g, 0, "eval"); err != nil {
		return dir, fmt.Errorf("save graph: %w", err)
	}
	if err := a.Engine.GraphAction(ctx, id, "start", "eval"); err != nil {
		return dir, fmt.Errorf("start: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"request": c.Request})
	if _, err := a.Engine.Inject(ctx, id, "requests", "feature.request", payload, "", nil, "eval"); err != nil {
		return dir, fmt.Errorf("inject: %w", err)
	}
	final, xs, err := waitFactory(ctx, a.Store, id, timeout)
	for _, x := range xs {
		res.ToolCalls += x.ToolCalls
		res.InputTokens += x.Usage.InputTokens
		res.OutputTokens += x.Usage.OutputTokens
		if x.Status == domain.ExecFailed && x.Error != "" && res.Error == "" {
			res.Error = x.NodeID + ": " + x.Error
		}
	}
	if final != nil {
		res.Execution, res.Status = final.ID, string(final.Status)
	} else {
		res.Status = "stopped before the hand-off"
	}
	return dir, err
}

// waitFactory waits for the hand-off agent to finish. It also returns when
// every execution has been finished for factoryQuiet (the workflow stopped
// short, e.g. the rework loop ran out) or at the deadline.
func waitFactory(ctx context.Context, st storage.Store, graph string, timeout time.Duration) (*domain.Execution, []*domain.Execution, error) {
	deadline := time.Now().Add(timeout)
	var xs []*domain.Execution
	lastActive := time.Now()
	for time.Now().Before(deadline) {
		list, err := st.ListExecutions(ctx, storage.ExecutionFilter{GraphID: graph, Limit: 1000})
		if err == nil {
			xs = list
			running := false
			for _, x := range xs {
				if x.NodeID == "integrator" && x.CompletedAt != nil {
					return x, xs, nil
				}
				if x.CompletedAt == nil {
					running = true
				} else if x.CompletedAt.After(lastActive) {
					lastActive = *x.CompletedAt
				}
			}
			if running {
				lastActive = time.Now()
			} else if len(xs) > 0 && time.Since(lastActive) > factoryQuiet {
				return nil, xs, fmt.Errorf("the workflow stopped before the hand-off (%s)", lastNodes(xs))
			}
		}
		select {
		case <-ctx.Done():
			return nil, xs, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil, xs, fmt.Errorf("the workflow did not reach the hand-off within %s (%s)", timeout, lastNodes(xs))
}

// lastNodes names the agents that ran, newest first, for diagnosis.
func lastNodes(xs []*domain.Execution) string {
	var names []string
	for i, x := range xs {
		if i == 6 {
			names = append(names, "…")
			break
		}
		names = append(names, x.NodeID+":"+string(x.Status))
	}
	return strings.Join(names, ", ")
}
