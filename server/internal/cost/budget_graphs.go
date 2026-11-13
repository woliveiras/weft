package cost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const (
	PauseGraph  = "pause"
	ResumeGraph = "resume"
	HoldGraph   = "hold"
)

// GraphAction is persisted with the ledger so a controller can finish a budget
// transition after losing the graph lease or restarting. RunID scopes it to
// the run that reached the budget. Empty RunID is supported for legacy ledgers.
type GraphAction struct {
	Action string    `json:"action"`
	RunID  string    `json:"runId,omitempty"`
	Actor  string    `json:"actor,omitempty"`
	Raised time.Time `json:"raised,omitempty"`
}

type BudgetGraph struct {
	Scope, ID, Graph string
	GraphAction
}

// MarkPaused registers a pause before attempting the graph transition. The
// limit guards against a refusal arriving after the administrator raised it.
func (b Budgets) MarkPaused(ctx context.Context, target BudgetGraph, limit int64) error {
	return b.update(ctx, target.Scope, target.ID, "pause", func(l *Ledger) (*Ledger, error) {
		if l == nil {
			return nil, fmt.Errorf("%w: budget %s/%s", storage.ErrNotFound, target.Scope, target.ID)
		}
		current := l.GraphActions[target.Graph]
		if l.Limit != limit || current == target.GraphAction {
			return nil, nil
		}
		l.setGraphAction(target.Graph, target.GraphAction)
		return l, nil
	})
}

func (l *Ledger) setGraphAction(graph string, action GraphAction) {
	if l.GraphActions == nil {
		l.GraphActions = make(map[string]GraphAction)
	}
	if !slices.Contains(l.PausedGraphs, graph) {
		l.PausedGraphs = append(l.PausedGraphs, graph)
	}
	l.GraphActions[graph] = action
}

// Raise persists the new limit and schedules resumptions in the same ledger
// write. The caller audits the outcome after attempting the graph actions.
func (b Budgets) Raise(ctx context.Context, scope, id string, limit int64, actor string) (View, int64, []BudgetGraph, error) {
	if limit < 1 || limit > MaxBudgetMicros {
		return View{}, 0, nil, &ValidationError{Errors: []Issue{{"limitMicros", fmt.Sprintf("must be between 1 and %d", MaxBudgetMicros)}}}
	}
	if strings.HasPrefix(actor, "system") || actor == "" {
		return View{}, 0, nil, errors.New("cost: only a person can raise a budget")
	}
	var view View
	var actions []BudgetGraph
	var previous int64
	err := b.update(ctx, scope, id, "raise", func(l *Ledger) (*Ledger, error) {
		if l == nil {
			return nil, fmt.Errorf("%w: budget %s/%s", storage.ErrNotFound, scope, id)
		}
		if limit <= l.Limit {
			return nil, &ValidationError{Errors: []Issue{{"limitMicros", fmt.Sprintf("must be higher than the current limit (%d)", l.Limit)}}}
		}
		now := time.Now().UTC()
		previous = l.Limit
		l.Limit, l.Reached, l.Warned, l.RaisedAt, l.RaisedBy = limit, false, false, &now, actor
		actions = make([]BudgetGraph, 0, len(l.PausedGraphs))
		for _, graph := range l.PausedGraphs {
			action := GraphAction{Action: ResumeGraph, RunID: l.GraphActions[graph].RunID, Actor: actor, Raised: now}
			l.setGraphAction(graph, action)
			actions = append(actions, BudgetGraph{scope, id, graph, action})
		}
		view = l.View()
		return l, nil
	})
	return view, previous, actions, err
}

// PendingGraphActions returns pending actions and active budget holds.
func (b Budgets) PendingGraphActions(ctx context.Context) ([]BudgetGraph, error) {
	rows, err := b.Store.ListSettings(ctx, BudgetPrefix)
	if err != nil {
		return nil, err
	}
	var actions []BudgetGraph
	var failures []error
	for _, row := range rows {
		scope, id, ok := strings.Cut(strings.TrimPrefix(row.Key, BudgetPrefix), "/")
		if !ok {
			continue
		}
		var ledger Ledger
		if err := json.Unmarshal(row.Value, &ledger); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", row.Key, err))
			continue
		}
		for _, graph := range ledger.PausedGraphs {
			action := ledger.GraphActions[graph]
			// A legacy pause has no run ID and no recorded operation.
			if action.Action == "" {
				action.Action = HoldGraph
			}
			actions = append(actions, BudgetGraph{scope, id, graph, action})
		}
	}
	return actions, errors.Join(failures...)
}

func (b Budgets) GraphActionCurrent(ctx context.Context, target BudgetGraph) (bool, error) {
	l, _, err := b.read(ctx, target.Scope, target.ID)
	return l != nil && l.matchesGraphAction(target), err
}

func (l *Ledger) matchesGraphAction(target BudgetGraph) bool {
	current, ok := l.GraphActions[target.Graph]
	if !ok {
		return target.GraphAction == (GraphAction{Action: HoldGraph}) && slices.Contains(l.PausedGraphs, target.Graph)
	}
	return current == target.GraphAction
}

// ResolveGraphAction updates only the intent that was acted on. A concurrent
// raise or a new refusal can replace it while the graph transition is running.
func (b Budgets) ResolveGraphAction(ctx context.Context, target BudgetGraph, held bool) (bool, error) {
	var changed bool
	err := b.update(ctx, target.Scope, target.ID, "graph_transition", func(l *Ledger) (*Ledger, error) {
		changed = l != nil && l.matchesGraphAction(target)
		if !changed {
			return nil, nil
		}
		if held {
			l.setGraphAction(target.Graph, GraphAction{Action: HoldGraph, RunID: target.RunID})
		} else {
			l.PausedGraphs = slices.DeleteFunc(l.PausedGraphs, func(graph string) bool { return graph == target.Graph })
			delete(l.GraphActions, target.Graph)
		}
		return l, nil
	})
	return changed && err == nil, err
}

// AuditRaise records the immediate results of the human's raise. The engine
// audits individual transitions again only when they resolve in the background.
func (b Budgets) AuditRaise(ctx context.Context, scope, id, actor string, previous, limit int64, resumed, pending []string, skipped map[string]string) {
	b.publish(ctx, domain.NewEvent(domain.EvBudgetRaised, graphOf(scope, id), map[string]any{
		"scope": scope, "id": id, "previousMicros": previous, "limitMicros": limit,
		"resumed": resumed, "notResumed": pending, "skipped": skipped,
	}).WithActor(actor))
}
