package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (e *Engine) monetaryBudgets() cost.Budgets {
	return cost.Budgets{Store: e.store, Bus: e.bus}
}

// BudgetResumeError identifies the ledger preventing a manual resume.
type BudgetResumeError struct {
	Scope string
	ID    string
}

func (e *BudgetResumeError) Error() string {
	return fmt.Sprintf("Resume blocked by %s budget %s. Ask an administrator to raise it in Models → Costs.", e.Scope, e.ID)
}

func (e *BudgetResumeError) Unwrap() error { return ErrConflict }

func (e *Engine) checkManualBudgetResume(ctx context.Context, g *domain.Graph, runID, actor, action, node string) error {
	target := graphBudgetTarget(g)
	if !strings.HasPrefix(target.Graph, domain.TaskRunGraphPrefix) {
		// Standalone graphs are not subject to task spending policy. Honor
		// an explicit run ledger if one exists for this graph.
		_, err := e.store.GetSetting(ctx, cost.BudgetPrefix+cost.TaskRun+"/"+target.Graph)
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	now := time.Now()
	blocker, reason, err := e.monetaryBudgets().ResumeBlocker(ctx, target, cost.BudgetGraph{
		GraphAction: cost.GraphAction{RunID: runID},
	}, now)
	if err != nil || reason == "" {
		return err
	}
	data := map[string]any{
		"scope": blocker.Scope, "id": blocker.ID, "action": cost.ResumeGraph, "outcome": reason,
		"blockingScope": blocker.Scope, "blockingId": blocker.ID, "requestedAction": action,
	}
	event := domain.NewEvent(domain.EvBudgetGraphTransition, target.Graph, data).WithActor(actor)
	if node != "" {
		event.WithNode(node, -1)
	}
	e.bus.Publish(ctx, event)
	return &BudgetResumeError{Scope: blocker.Scope, ID: blocker.ID}
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
		if len(actions) != 0 {
			view.Paused = len(view.NotResumed) != 0 || slices.ContainsFunc(actions, func(target cost.BudgetGraph) bool {
				return skipped[target.Graph] == "budget_reached" && view.Reached
			})
		}
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
	switch {
	case rec != nil && target.RunID == "" && outcome != "stopped":
		outcome = "legacy_unbound"
	case outcome == "" || outcome == "resume" || outcome == "pause" || outcome == "paused" || outcome == "already_running":
		var retain bool
		outcome, retain, err = e.checkBudgetGraphTransition(ctx, target, rec, outcome, expired, now)
		if err != nil || retain {
			return outcome, err
		}
	}
	switch outcome {
	case "pause":
		actor := target.Actor
		if actor == "" {
			actor = "system:budget"
		}
		if err := e.recordGraphAction(ctx, rec, "pause", actor, ""); err != nil {
			return "", err
		}
		outcome = "paused"
	case "resume":
		if err := e.recordGraphAction(ctx, rec, "resume", target.Actor, target.GraphAction.ID); err != nil {
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

// checkBudgetGraphTransition checks a live run under the graph lease before
// changing its desired state or transferring a hold to a different ledger.
func (e *Engine) checkBudgetGraphTransition(ctx context.Context, target cost.BudgetGraph, rec *domain.GraphRecord, outcome string, expired bool, now time.Time) (string, bool, error) {
	budgets := e.monetaryBudgets()
	if expired && rec.DesiredState != domain.GraphPaused && target.Action != cost.ResumeGraph {
		return "period_ended", false, nil
	}
	g, err := e.store.GetGraph(ctx, target.Graph, rec.Version)
	if err != nil {
		return "", false, err
	}
	graphTarget := graphBudgetTarget(g)
	if rec.DesiredState == domain.GraphPaused {
		finished, err := e.runFinished(ctx, g, rec.RunID)
		if err != nil {
			return "", false, err
		}
		if finished {
			return "run_finished", false, nil
		}
		if expired && target.Action != cost.ResumeGraph {
			scheduled, err := budgets.RefreshGraphBudget(ctx, target, graphTarget, now)
			if !scheduled {
				return "", true, err
			}
			return "period_ended", true, nil
		}
	}
	if target.Action == cost.ResumeGraph {
		if err := e.checkDeferredDeletion(ctx, target.Graph); err != nil {
			return "", false, err
		}
	}
	blocker, reason, err := budgets.ResumeBlocker(ctx, graphTarget, target, now)
	if err != nil {
		return "", false, err
	}
	switch reason {
	case "not_applicable":
		return reason, false, nil
	case "":
		if outcome == "pause" {
			return "limit_available", false, nil
		}
	}
	sameLedger := target.Scope == blocker.Scope && target.ID == blocker.ID
	if target.Action != cost.ResumeGraph {
		if sameLedger || rec.DesiredState == domain.GraphPaused {
			scheduled, err := budgets.RefreshGraphBudget(ctx, target, graphTarget, now)
			if err != nil {
				return "", true, err
			}
			if scheduled {
				return "limit_available", true, nil
			}
			if sameLedger {
				return outcome, false, nil
			}
			if reason == "" {
				return "", true, nil
			}
		}
	}
	if reason == "" {
		return outcome, false, nil
	}
	if err := budgets.HoldForResumeBlocker(ctx, blocker, now); err != nil {
		return "", false, err
	}
	if rec.DesiredState == domain.GraphRunning {
		if err := e.recordGraphAction(ctx, rec, "pause", "system:budget", ""); err != nil {
			return "", false, err
		}
	}
	if err := e.auditBudgetTransition(ctx, target, reason, blocker); err != nil {
		return "", false, err
	}
	if sameLedger {
		return reason, true, nil
	}
	return reason, false, nil
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
			return "pause", nil
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
	failures, conflicts := 0, 0
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
		var invalidControl *invalidNodeControl
		switch {
		case errors.As(transitionErr, &invalidControl):
			e.warnInvalidNodeControl(target.Graph, target.RunID, invalidControl)
		case errors.Is(transitionErr, cost.ErrInvalidLedger):
			invalid++
		case errors.Is(transitionErr, storage.ErrConflict):
			conflicts++
			observability.BudgetOperations.WithLabelValues("all", "reconcile_graph", "conflict").Inc()
		case transitionErr != nil && !errors.Is(transitionErr, ErrGraphTransitionBusy):
			failures++
		}
	}
	if conflicts > 0 && ctx.Err() == nil && budgetWarningDue(&e.budgetConflictWarningAt, now) {
		e.log.Warn("budget graph transition conflicts", "conflicted_actions", conflicts)
	}
	if err == nil {
		err = work.Err()
	}
	failed := err != nil || failures > 0
	if ctx.Err() == nil && (failed || (invalid > 0 && budgetWarningDue(&e.invalidLedgerWarningAt, now))) {
		e.log.Warn("reconcile budget graph transitions", "scan_failed", err != nil,
			"timed_out", errors.Is(err, context.DeadlineExceeded), "failed_actions", failures, "invalid_ledgers", invalid)
	}
	return failed
}

// Share each graph's warning limit across status polling and budget sweeps.
func (e *Engine) warnInvalidNodeControl(graph, run string, err *invalidNodeControl) {
	if e.invalidControlWarnings.allow(graph, time.Now()) {
		e.log.Warn("check paused run completion", "graph", graph, "run", run, "err", err)
	}
}

const maxGraphWarnings = 256

type graphWarningLimiter struct {
	mu     sync.Mutex
	warned map[string]time.Time
}

func (l *graphWarningLimiter) allow(graph string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.warned == nil {
		l.warned = make(map[string]time.Time)
	}
	if last, ok := l.warned[graph]; ok && now.Sub(last) < time.Minute {
		return false
	}
	if len(l.warned) >= maxGraphWarnings {
		for id, last := range l.warned {
			if now.Sub(last) >= time.Minute {
				delete(l.warned, id)
			}
		}
		if len(l.warned) >= maxGraphWarnings {
			return false
		}
	}
	l.warned[graph] = now
	return true
}

func budgetWarningDue(last *atomic.Int64, now time.Time) bool {
	previous := last.Load()
	return now.Sub(time.Unix(0, previous)) >= time.Minute && last.CompareAndSwap(previous, now.UnixNano())
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
