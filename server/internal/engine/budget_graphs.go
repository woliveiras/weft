package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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
		}
		switch outcome {
		case "resumed":
			resumed = append(resumed, target.Graph)
		case "":
			// A concurrent update superseded this action.
		default:
			skipped[target.Graph] = outcome
			if err == nil && outcome != "budget_reached" {
				_ = e.auditBudgetTransition(work, target, outcome)
			}
		}
	}
	audit, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	latest, err := budgets.Ledger(audit, scope, id)
	if err != nil {
		// The limit still changed. Use the recorded attempt results when
		// the store cannot return the current view.
		e.log.Warn("reload raised budget", "scope", scope, "budget", id, "err", err)
		view.NotResumed = slices.DeleteFunc(view.NotResumed, func(graph string) bool {
			return slices.Contains(resumed, graph) || skipped[graph] != ""
		})
		view.Paused = len(view.NotResumed) != 0 || slices.ContainsFunc(actions, func(target cost.BudgetGraph) bool {
			return skipped[target.Graph] == "budget_reached" && view.Reached
		})
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
	now := time.Now()
	expired := cost.ExpiredLedger(target.Scope, target.ID, cost.Period(now))
	if rec != nil && target.RunID == "" && outcome != "stopped" {
		outcome = "legacy_unbound"
	} else if outcome == "" || outcome == "resume" || outcome == "pause" || outcome == "paused" {
		if rec.DesiredState == domain.GraphPaused {
			g, err := e.store.GetGraph(ctx, target.Graph, rec.Version)
			if err != nil {
				return "", err
			}
			finished, err := e.runFinished(ctx, g, rec.RunID)
			if err != nil {
				return "", err
			}
			if finished {
				outcome = "run_finished"
			} else if expired && target.Action != cost.ResumeGraph {
				if err := budgets.ScheduleRollover(ctx, target); err != nil {
					return "", err
				}
				return "period_ended", nil
			} else if outcome == "resume" {
				labels := g.Metadata.Labels
				t := cost.Target{Graph: target.Graph, User: labels[domain.LabelTaskUser],
					Project: labels[domain.LabelTaskProject], Workflow: labels[domain.LabelTaskWorkflow]}
				blocker, reason, err := budgets.ResumeBlocker(ctx, t, target, now)
				if err != nil {
					return "", err
				}
				if reason == "budget_reached" {
					if err := budgets.HoldForResumeBlocker(ctx, blocker, now); err != nil {
						return "", err
					}
					_ = e.auditBudgetTransition(ctx, target, reason, blocker)
					if target.Scope == blocker.Scope && target.ID == blocker.ID {
						return reason, nil // the same ledger now owns the hold
					}
				}
				if reason != "" {
					outcome = reason
				}
			}
		} else if expired {
			outcome = "period_ended"
		}
	}
	switch outcome {
	case "pause":
		if err := e.graphAction(ctx, target.Graph, "pause", target.Actor); err != nil {
			return "", err
		}
		outcome = "paused"
	case "resume":
		if err := e.checkDeferredDeletion(ctx, target.Graph); err != nil {
			return "", err
		}
		if err := e.recordGraphAction(ctx, target.Graph, "resume", target.Actor, target.GraphAction.ID); err != nil {
			// A write can commit while its caller loses the reply.
			verify, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			applied, readErr := e.store.GetGraphRecord(verify, target.Graph)
			if readErr != nil || applied.RunID != target.RunID || target.GraphAction.ID == "" ||
				applied.BudgetResumeID != target.GraphAction.ID {
				return "", errors.Join(err, readErr)
			}
		}
		outcome = "resumed"
	case "":
		return "", nil
	}
	if outcome == "resumed" {
		// The command has committed. Finalize under the same lease even if
		// its request expired. The graph receipt covers process loss as well.
		finalize, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := e.auditBudgetTransition(finalize, target, outcome); err != nil {
			return outcome, err
		}
		_, err := budgets.ResolveGraphAction(finalize, target, false)
		return outcome, err
	}
	_, err = budgets.ResolveGraphAction(ctx, target, outcome == "paused")
	return outcome, err
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
	if target.Action == cost.ResumeGraph && target.GraphAction.ID != "" && target.GraphAction.ID == rec.BudgetResumeID {
		return "resumed", nil
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

func (e *Engine) budgetGraphLoop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	delay := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if e.reconcileBudgetGraphs(ctx) {
			delay = min(2*delay, time.Minute)
		} else {
			delay = 5 * time.Second
		}
		timer.Reset(delay)
	}
}

// reconcileBudgetGraphs returns whether a sweep found an error and should back off.
func (e *Engine) reconcileBudgetGraphs(ctx context.Context) bool {
	now := time.Now()
	work, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	actions, invalid, err := e.monetaryBudgets().PendingGraphActions(work)
	failures := 0
	for _, target := range actions {
		if work.Err() != nil {
			break
		}
		// Give the raise request its four-second window before competing
		// with it for the lease.
		if target.Action == cost.ResumeGraph && now.Sub(target.Raised) < 4*time.Second {
			continue
		}
		transitionErr := e.WithGraphTransition(work, target.Graph, func(ctx context.Context) error {
			outcome, err := e.applyBudgetGraphTransition(ctx, target)
			if err == nil && outcome != "" && outcome != "budget_reached" && outcome != "resumed" {
				return e.auditBudgetTransition(ctx, target, outcome)
			}
			return err
		})
		if transitionErr != nil && !errors.Is(transitionErr, ErrGraphTransitionBusy) {
			failures++
		}
	}
	if err == nil {
		err = work.Err()
	}
	failed := err != nil || failures > 0 || invalid > 0
	if failed && ctx.Err() == nil {
		e.log.Warn("reconcile budget graph transitions", "scan_failed", err != nil,
			"timed_out", errors.Is(err, context.DeadlineExceeded), "failed_actions", failures, "invalid_ledgers", invalid)
	}
	return failed
}

func (e *Engine) auditBudgetTransition(ctx context.Context, target cost.BudgetGraph, outcome string, blockers ...cost.BudgetGraph) error {
	data := map[string]any{"scope": target.Scope, "id": target.ID, "action": target.Action, "outcome": outcome}
	if len(blockers) != 0 {
		data["blockingScope"], data["blockingId"] = blockers[0].Scope, blockers[0].ID
	}
	if outcome == "resumed" && target.GraphAction.ID != "" {
		ev := domain.NewEvent(domain.EvBudgetGraphTransition, target.Graph, data).WithActor("system:budget")
		ev.ID = "evt_" + strings.ReplaceAll(target.GraphAction.ID, "-", "_")
		return e.bus.PublishOnce(ctx, ev)
	}
	e.bus.Publish(ctx, domain.NewEvent(domain.EvBudgetGraphTransition, target.Graph, data).WithActor("system:budget"))
	return nil
}
