package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
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
	source, current, err := budgets.GraphActionRevision(ctx, target)
	if err != nil || !current {
		return "", err
	}
	rec, err := e.store.GetGraphRecord(ctx, target.Graph)
	outcome, err := budgetGraphOutcome(target, rec, err)
	if err != nil {
		return "", err
	}
	var guards []storage.SettingRevision
	now := time.Now()
	expired := cost.ExpiredLedger(target.Scope, target.ID, cost.Period(now))
	switch {
	case rec != nil && target.RunID == "" && outcome != "stopped":
		outcome = "legacy_unbound"
	case outcome == "" || outcome == "resume" || outcome == "pause" || outcome == "paused" || outcome == "already_running":
		var retain bool
		outcome, retain, guards, err = e.checkBudgetGraphTransition(ctx, target, rec, outcome, expired, now, source)
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
		if err := e.recordGraphAction(ctx, rec, "pause", actor, "", guards...); err != nil {
			return "", err
		}
		outcome = "paused"
	case "resume":
		if err := e.recordGraphAction(ctx, rec, "resume", target.Actor, target.GraphAction.ID, guards...); err != nil {
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
	if outcome != "not_applicable" && outcome != "limit_available" {
		guards = nil
	}
	changed, err := budgets.ResolveGraphAction(ctx, target, outcome == "paused", guards...)
	if !changed && err == nil && (outcome == "not_applicable" || outcome == "limit_available") {
		return "", nil
	}
	return outcome, err
}

// checkBudgetGraphTransition checks a live run under the graph lease before
// changing its desired state or transferring a hold to a different ledger.
func (e *Engine) checkBudgetGraphTransition(ctx context.Context, target cost.BudgetGraph, rec *domain.GraphRecord, outcome string, expired bool, now time.Time, source storage.SettingRevision) (string, bool, []storage.SettingRevision, error) {
	budgets := e.monetaryBudgets()
	if expired && rec.DesiredState != domain.GraphPaused && target.Action != cost.ResumeGraph {
		return "period_ended", false, nil, nil
	}
	g, err := e.store.GetGraph(ctx, target.Graph, rec.Version)
	if err != nil {
		return "", false, nil, err
	}
	if rec.DesiredState == domain.GraphPaused {
		finished, err := e.runFinished(ctx, g, rec.RunID)
		if err != nil {
			return "", false, nil, err
		}
		if finished {
			return "run_finished", false, nil, nil
		}
		if expired && target.Action != cost.ResumeGraph {
			return "period_ended", true, nil, budgets.ScheduleGraphResume(ctx, target, now, source)
		}
	}
	if target.Action == cost.ResumeGraph {
		if err := e.checkDeferredDeletion(ctx, target.Graph); err != nil {
			return "", false, nil, err
		}
	}
	blocker, reason, checked, err := budgets.CheckGraphBudgets(ctx, graphBudgetTarget(g), target, now)
	if err != nil {
		return "", false, nil, err
	}
	guards := append(checked, source)
	switch reason {
	case "not_applicable":
		return reason, false, guards, nil
	case "":
		if outcome == "pause" {
			return "limit_available", false, guards, nil
		}
	}
	sameLedger := target.Scope == blocker.Scope && target.ID == blocker.ID
	if target.Action != cost.ResumeGraph {
		if sameLedger {
			return outcome, false, guards, nil
		}
		if rec.DesiredState == domain.GraphPaused {
			return "limit_available", true, nil, budgets.ScheduleGraphResume(ctx, target, now, guards...)
		}
	}
	if reason == "" {
		return outcome, false, guards, nil
	}
	if err := budgets.HoldForResumeBlocker(ctx, blocker, now, guards); err != nil {
		return "", false, nil, err
	}
	if rec.DesiredState == domain.GraphRunning {
		// The guarded transfer changed the blocker ledger by one revision.
		key := cost.BudgetPrefix + blocker.Scope + "/" + blocker.ID
		for i := range guards {
			if guards[i].Key == key {
				guards[i].Revision++
			}
		}
		if err := e.recordGraphAction(ctx, rec, "pause", "system:budget", "", guards...); err != nil {
			return "", false, nil, err
		}
	}
	if err := e.auditBudgetTransition(ctx, target, reason, blocker); err != nil {
		return "", false, nil, err
	}
	if sameLedger {
		return reason, true, nil, nil
	}
	return reason, false, nil, nil
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
		var invalidControl *invalidNodeControl
		switch {
		case errors.As(transitionErr, &invalidControl):
			e.warnInvalidNodeControl(target.Graph, target.RunID, invalidControl)
		case errors.Is(transitionErr, cost.ErrInvalidLedger):
			invalid++
		case transitionErr != nil && !errors.Is(transitionErr, ErrGraphTransitionBusy) && !errors.Is(transitionErr, storage.ErrConflict):
			failures++
		}
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

// Share one warning per minute across status polling and budget sweeps.
func (e *Engine) warnInvalidNodeControl(graph, run string, err *invalidNodeControl) {
	if budgetWarningDue(&e.invalidControlWarningAt, time.Now()) {
		e.log.Warn("check paused run completion", "graph", graph, "run", run, "err", err)
	}
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
