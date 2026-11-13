package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (e *Engine) monetaryBudgets() cost.Budgets {
	return cost.Budgets{Store: e.store, Bus: e.bus}
}

func (e *Engine) RaiseBudget(ctx context.Context, scope, id string, limit int64, actor string) (cost.View, error) {
	budgets := e.monetaryBudgets()
	view, previous, actions, err := budgets.Raise(ctx, scope, id, limit, actor)
	if err != nil {
		return view, err
	}

	// The raise is already committed. Finish the bounded attempt and audit
	// even if the client disconnects during the request.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	var resumed []string
	skipped := make(map[string]string)
	for _, target := range actions {
		if work.Err() != nil {
			break
		}
		outcome, err := e.waitBudgetGraphTransition(work, target)
		if err != nil {
			if !errors.Is(err, ErrGraphTransitionBusy) && work.Err() == nil {
				e.log.Warn("budget resume deferred", "graph", target.Graph, "scope", scope, "budget", id, "err", err)
			}
			continue
		}
		switch outcome {
		case "resumed":
			resumed = append(resumed, target.Graph)
		case "":
			// A concurrent update superseded this action.
		default:
			skipped[target.Graph] = outcome
		}
	}
	audit, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	latest, err := budgets.Ledger(audit, scope, id)
	if err != nil {
		// The limit still changed. Use the recorded attempt results when
		// the store cannot return the current view.
		e.log.Warn("reload raised budget", "scope", scope, "budget", id, "err", err)
		pending := view.NotResumed[:0]
		for _, graph := range view.NotResumed {
			if !slices.Contains(resumed, graph) && skipped[graph] == "" {
				pending = append(pending, graph)
			}
		}
		view.NotResumed, view.Paused = pending, len(pending) != 0
	} else if latest != nil {
		view = *latest
	}
	budgets.AuditRaise(audit, scope, id, actor, previous, limit, resumed, view.NotResumed, skipped)
	return view, nil
}

func (e *Engine) waitBudgetGraphTransition(ctx context.Context, target cost.BudgetGraph) (string, error) {
	var outcome string
	err := e.waitGraphTransition(ctx, target.Graph, func(ctx context.Context) error {
		var err error
		outcome, err = e.applyBudgetGraphTransition(ctx, target)
		return err
	})
	return outcome, err
}

// applyBudgetGraphTransition runs under the graph transition lease. The
// ledger write uses compare-and-set because budget changes and reservations
// are independent of that lease.
func (e *Engine) applyBudgetGraphTransition(ctx context.Context, target cost.BudgetGraph) (string, error) {
	budgets := e.monetaryBudgets()
	current, err := budgets.GraphActionCurrent(ctx, target)
	if err != nil || !current {
		return "", err
	}
	rec, err := e.store.GetGraphRecord(ctx, target.Graph)
	outcome, err := budgetGraphOutcome(target, rec, err)
	if err != nil {
		return "", err
	}
	switch outcome {
	case "pause":
		if err := e.graphAction(ctx, target.Graph, "pause", target.Actor); err != nil {
			return "", err
		}
		outcome = "paused"
	case "resume":
		blocked, err := e.heldByAnotherBudget(ctx, target)
		if err != nil {
			return "", err
		}
		if blocked {
			outcome = "another_budget"
			break
		}
		if err := e.checkDeferredDeletion(ctx, target.Graph); err != nil {
			return "", err
		}
		if err := e.graphAction(ctx, target.Graph, "resume", target.Actor); err != nil {
			return "", err
		}
		outcome = "resumed"
	case "":
		return "", nil
	}
	if _, err := budgets.ResolveGraphAction(ctx, target, outcome == "paused"); err != nil {
		return "", err
	}
	return outcome, nil
}

func budgetGraphOutcome(target cost.BudgetGraph, rec *domain.GraphRecord, err error) (string, error) {
	if errors.Is(err, storage.ErrNotFound) {
		return "deleted", nil
	}
	if err != nil {
		return "", err
	}
	if target.RunID != "" && target.RunID != rec.RunID {
		return "run_changed", nil
	}
	if rec.DesiredState == domain.GraphStopped || rec.DesiredState == domain.GraphStopping || rec.DesiredState == "" {
		return "stopped", nil
	}
	switch target.Action {
	case cost.PauseGraph:
		if rec.DesiredState == domain.GraphPaused {
			return "paused", nil
		}
		return "pause", nil
	case cost.ResumeGraph:
		if rec.DesiredState == domain.GraphRunning {
			return "already_running", nil
		}
		return "resume", nil
	case cost.HoldGraph:
		if rec.DesiredState == domain.GraphRunning {
			return "already_running", nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("unknown budget graph action %q", target.Action)
	}
}

func (e *Engine) heldByAnotherBudget(ctx context.Context, target cost.BudgetGraph) (bool, error) {
	actions, err := e.monetaryBudgets().PendingGraphActions(ctx)
	for _, other := range actions {
		if other.Graph == target.Graph && (other.Scope != target.Scope || other.ID != target.ID) &&
			(other.RunID == "" || target.RunID == "" || other.RunID == target.RunID) &&
			(other.Action == cost.HoldGraph || other.Action == cost.PauseGraph) {
			return true, nil
		}
	}
	return false, err
}

func (e *Engine) reconcileBudgetGraphs(ctx context.Context) {
	now := time.Now()
	if now.Sub(e.monetaryBudgetSweepAt) < 5*time.Second {
		return
	}
	e.monetaryBudgetSweepAt = now
	work, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	actions, err := e.monetaryBudgets().PendingGraphActions(work)
	var failures []error
	failures = append(failures, err)
	for _, target := range actions {
		if work.Err() != nil {
			break
		}
		// Give the raise request its four-second window before competing
		// with it for the lease.
		if target.Action == cost.HoldGraph || (target.Action == cost.ResumeGraph && now.Sub(target.Raised) < 4*time.Second) {
			continue
		}
		err := e.WithGraphTransition(work, target.Graph, func(ctx context.Context) error {
			outcome, err := e.applyBudgetGraphTransition(ctx, target)
			if err != nil || outcome == "" {
				return err
			}
			e.auditBudgetTransition(ctx, target, outcome)
			return nil
		})
		if err != nil && !errors.Is(err, ErrGraphTransitionBusy) {
			failures = append(failures, fmt.Errorf("graph %s: %w", target.Graph, err))
		}
	}
	if err := errors.Join(failures...); err != nil && ctx.Err() == nil {
		e.log.Warn("reconcile budget graph transitions", "err", err)
	}
}

func (e *Engine) auditBudgetTransition(ctx context.Context, target cost.BudgetGraph, outcome string) {
	e.bus.Publish(ctx, domain.NewEvent(domain.EvBudgetGraphTransition, target.Graph, map[string]any{
		"scope": target.Scope, "id": target.ID, "action": target.Action, "outcome": outcome,
	}).WithActor("system:budget"))
}
