package cost

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

const (
	PauseGraph  = "pause"
	ResumeGraph = "resume"
	HoldGraph   = "hold"
)

// GraphAction is persisted with the ledger so a controller can finish a budget
// transition after losing the graph lease or restarting. RunID scopes it to
// the run that reached the budget. Unbound legacy actions are retired without applying them.
type GraphAction struct {
	ID     string    `json:"id,omitempty"`
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
	if ExpiredLedger(scope, id, Period(time.Now())) {
		return View{}, 0, nil, &ValidationError{Errors: []Issue{{"id", "the monthly budget period has ended"}}}
	}
	var view View
	var actions []BudgetGraph
	var previous int64
	var definition *applicable
	applyRaise := func(l *Ledger) (*Ledger, error) {
		if l == nil {
			return nil, fmt.Errorf("%w: budget %s/%s", storage.ErrNotFound, scope, id)
		}
		if definition != nil {
			definition.applyLimit(l)
		}
		if limit <= l.Limit {
			return nil, &ValidationError{Errors: []Issue{{"limitMicros", fmt.Sprintf("must be higher than the current limit (%d)", l.Limit)}}}
		}
		now := time.Now().UTC()
		previous = l.Limit
		used := l.used()
		l.Limit, l.RaisedLimit, l.Reached, l.RaisedAt, l.RaisedBy = limit, limit, used >= limit, &now, actor
		l.Warned = used >= l.warningAt()
		actions = nil
		for _, graph := range l.PausedGraphs {
			if l.Reached {
				if action := l.GraphActions[graph]; action.Action == ResumeGraph {
					l.setGraphAction(graph, GraphAction{Action: HoldGraph, RunID: action.RunID})
				}
				continue
			}
			action := GraphAction{ID: domain.NewID("budget-resume"), Action: ResumeGraph, RunID: l.GraphActions[graph].RunID, Actor: actor, Raised: now}
			l.setGraphAction(graph, action)
			actions = append(actions, BudgetGraph{scope, id, graph, action})
		}
		view = l.View()
		return l, nil
	}
	if scope == TaskRun {
		err := b.update(ctx, scope, id, "raise", applyRaise)
		return view, previous, actions, err
	}
	for range 100 {
		defs, revision, err := b.Definitions(ctx)
		if err != nil {
			return View{}, 0, nil, err
		}
		definition = nil
		for _, d := range defs {
			if d.Scope == scope && MonthlyID(d.ID, Period(time.Now())) == id {
				definition = &applicable{scope: scope, limit: d.LimitMicros}
				break
			}
		}
		err = b.update(ctx, scope, id, "raise", applyRaise, storage.SettingRevision{Key: DefinitionsKey, Revision: revision})
		if !errors.Is(err, storage.ErrConflict) {
			return view, previous, actions, err
		}
	}
	return View{}, 0, nil, fmt.Errorf("%w: budgets changed during raise", storage.ErrConflict)
}

// PendingGraphActions projects graph state without decoding reservation entries.
func (b Budgets) PendingGraphActions(ctx context.Context) ([]BudgetGraph, int, error) {
	rows, err := b.Store.ListSettings(ctx, BudgetPrefix)
	if err != nil {
		return nil, 0, err
	}
	var actions []BudgetGraph
	var invalid int
	for _, row := range rows {
		scope, id, ok := strings.Cut(strings.TrimPrefix(row.Key, BudgetPrefix), "/")
		if !ok {
			continue
		}
		var ledger struct {
			Scope        string
			ID           string
			Limit        int64
			PausedGraphs []string
			GraphActions map[string]GraphAction
		}
		if err := json.Unmarshal(row.Value, &ledger); err != nil || ledger.Scope != scope || ledger.ID != id ||
			ledger.Limit < 0 || ledger.Limit > MaxBudgetMicros || !validGraphActions(ledger.GraphActions) {
			invalid++
			b.auditInvalidLedger(ctx, scope, id, row.Revision)
			continue
		}
		for _, graph := range ledger.PausedGraphs {
			action := ledger.GraphActions[graph]
			if action.Action == "" {
				action.Action = HoldGraph
			}
			actions = append(actions, BudgetGraph{scope, id, graph, action})
		}
	}
	return actions, invalid, nil
}

// ResumeBlocker checks the current applicable ledgers. For a scheduled
// action it also reports when the source no longer applies. Re-read the
// revisions before using the result to detect concurrent changes.
func (b Budgets) ResumeBlocker(ctx context.Context, t Target, source BudgetGraph, now time.Time) (BudgetGraph, string, error) {
	blocker, reason, _, err := b.CheckGraphBudgets(ctx, t, source, now)
	return blocker, reason, err
}

// CheckGraphBudgets returns the setting revisions that a transition must
// still observe at commit time.
func (b Budgets) CheckGraphBudgets(ctx context.Context, t Target, source BudgetGraph, now time.Time) (BudgetGraph, string, []storage.SettingRevision, error) {
	for range 3 {
		blocker, reason, guards, err := b.resumeBlocker(ctx, t, source, now)
		if !errors.Is(err, storage.ErrConflict) {
			return blocker, reason, guards, err
		}
	}
	return BudgetGraph{}, "", nil, fmt.Errorf("%w: budgets changed during transition; retry the action", storage.ErrConflict)
}

func (b Budgets) resumeBlocker(ctx context.Context, t Target, source BudgetGraph, now time.Time) (BudgetGraph, string, []storage.SettingRevision, error) {
	apply, guards, err := b.applicableSnapshot(ctx, t, now)
	if err != nil {
		return BudgetGraph{}, "", nil, err
	}
	// Prefer the source when it still applies so two reached budgets
	// do not transfer holds back and forth.
	sourceApplies := false
	for i, a := range apply {
		if a.scope == source.Scope && a.id == source.ID {
			sourceApplies = true
			apply[0], apply[i] = apply[i], apply[0]
			break
		}
	}
	var blocker BudgetGraph
	reason := ""
	if source.Scope != "" && !sourceApplies && !ExpiredLedger(source.Scope, source.ID, Period(now)) {
		reason = "not_applicable"
	}
	for _, a := range apply {
		if reason != "" {
			break
		}
		l, revision, err := b.read(ctx, a.scope, a.id)
		if err != nil {
			return BudgetGraph{}, "", nil, err
		}
		guards = append(guards, storage.SettingRevision{Key: ledgerKey(a.scope, a.id), Revision: revision})
		if l == nil {
			continue
		}
		a.applyLimit(l)
		if l.Reached || l.used() >= l.Limit {
			blocker = BudgetGraph{Scope: a.scope, ID: a.id, Graph: t.Graph, GraphAction: GraphAction{Action: HoldGraph, RunID: source.RunID}}
			reason = "budget_reached"
		}
	}
	for _, guard := range guards {
		if err := b.checkSettingRevision(ctx, guard.Key, guard.Revision); err != nil {
			return BudgetGraph{}, "", nil, err
		}
	}
	return blocker, reason, guards, nil
}

func (b Budgets) checkSettingRevision(ctx context.Context, key string, revision int64) error {
	row, err := b.Store.GetSetting(ctx, key)
	if errors.Is(err, storage.ErrNotFound) && revision == 0 {
		return nil
	}
	if errors.Is(err, storage.ErrNotFound) || (err == nil && row.Revision != revision) {
		return storage.ErrConflict
	}
	return err
}

// HoldForResumeBlocker records the new hold before the prior intent is retired.
// The guarded write commits against the same snapshot as the budget check.
func (b Budgets) HoldForResumeBlocker(ctx context.Context, target BudgetGraph, now time.Time, guards []storage.SettingRevision) error {
	defs, _, err := b.Definitions(ctx)
	if err != nil {
		return err
	}
	return b.update(ctx, target.Scope, target.ID, "hold", func(l *Ledger) (*Ledger, error) {
		if l == nil {
			return nil, storage.ErrConflict
		}
		for _, d := range defs {
			if d.Scope == target.Scope && MonthlyID(d.ID, Period(now)) == target.ID {
				(applicable{scope: d.Scope, limit: d.LimitMicros}).applyLimit(l)
				break
			}
		}
		if !l.Reached && l.used() < l.Limit {
			return nil, storage.ErrConflict
		}
		l.Reached = true
		l.setGraphAction(target.Graph, target.GraphAction)
		return l, nil
	}, guards...)
}

// ExpiredLedger reports whether a monthly ledger belongs to a closed period.
func ExpiredLedger(scope, id, period string) bool {
	_, month, ok := strings.Cut(id, "@")
	return scope != TaskRun && ok && month < period
}

func (b Budgets) visibleView(ctx context.Context, l *Ledger, period string) (View, error) {
	v := l.View()
	if ExpiredLedger(l.Scope, l.ID, period) {
		v.Paused, v.NotResumed = false, nil
		return v, nil
	}
	// A committed graph resume remains observable even while its ledger
	// action awaits finalization.
	pending := v.NotResumed[:0]
	applied := 0
	for _, graph := range v.NotResumed {
		action := l.GraphActions[graph]
		if action.ID != "" {
			rec, err := b.Store.GetGraphRecord(ctx, graph)
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				return View{}, err
			}
			if rec != nil && rec.RunID == action.RunID && rec.BudgetResumeID == action.ID {
				applied++
				continue
			}
		}
		pending = append(pending, graph)
	}
	v.NotResumed = pending
	v.Paused = len(l.PausedGraphs) > applied
	return v, nil
}

func (b Budgets) auditInvalidLedger(ctx context.Context, scope, id string, revision int64) {
	if b.Bus == nil {
		return
	}
	ev := domain.NewEvent(domain.EvBudgetLedgerInvalid, graphOf(scope, id), map[string]any{
		"scope": scope, "id": id, "revision": revision,
	}).WithActor("system:budget")
	// The store's event ID constraint deduplicates discovery across controllers.
	ev.ID = fmt.Sprintf("evt_budget_ledger_invalid_%x", sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", scope, id, revision))))
	if err := b.Bus.PublishOnce(ctx, ev); err != nil {
		observability.BudgetOperations.WithLabelValues("unknown", "invalid_audit", "error").Inc()
	}
}

func (b Budgets) GraphActionCurrent(ctx context.Context, target BudgetGraph) (bool, error) {
	_, current, err := b.GraphActionRevision(ctx, target)
	return current, err
}

func (b Budgets) GraphActionRevision(ctx context.Context, target BudgetGraph) (storage.SettingRevision, bool, error) {
	l, revision, err := b.read(ctx, target.Scope, target.ID)
	return storage.SettingRevision{Key: ledgerKey(target.Scope, target.ID), Revision: revision}, l != nil && l.matchesGraphAction(target), err
}

func (l *Ledger) matchesGraphAction(target BudgetGraph) bool {
	current, ok := l.GraphActions[target.Graph]
	if !ok {
		return target.GraphAction == (GraphAction{Action: HoldGraph}) && slices.Contains(l.PausedGraphs, target.Graph)
	}
	return current == target.GraphAction
}

// ScheduleGraphResume converts a hold into a durable resume attempt after
// rollover or when the configured limit leaves enough capacity.
func (b Budgets) ScheduleGraphResume(ctx context.Context, target BudgetGraph, now time.Time, guards ...storage.SettingRevision) error {
	op := "schedule_resume"
	expired := ExpiredLedger(target.Scope, target.ID, Period(now))
	if expired {
		op = "rollover"
	}
	var defs []Definition
	if !expired {
		var revision int64
		var err error
		defs, revision, err = b.Definitions(ctx)
		if err != nil {
			return err
		}
		guards = append(guards, storage.SettingRevision{Key: DefinitionsKey, Revision: revision})
	}
	return b.update(ctx, target.Scope, target.ID, op, func(l *Ledger) (*Ledger, error) {
		if l == nil || !l.matchesGraphAction(target) {
			return nil, nil
		}
		if !expired {
			for _, d := range defs {
				if d.Scope == target.Scope && MonthlyID(d.ID, Period(now)) == target.ID {
					(applicable{scope: d.Scope, limit: d.LimitMicros}).applyLimit(l)
					if d.WarningPercent != 0 {
						l.WarningPercent = d.WarningPercent
					}
					l.Warned = l.used() >= l.warningAt()
					break
				}
			}
			if l.Reached || l.used() >= l.Limit {
				return nil, storage.ErrConflict
			}

		}
		l.setGraphAction(target.Graph, GraphAction{
			ID: domain.NewID("budget-resume"), Action: ResumeGraph, RunID: target.RunID, Actor: "system:budget",
		})
		return l, nil
	}, guards...)
}

// ResolveGraphAction updates only the intent that was acted on. A concurrent
// raise or a new refusal can replace it while the graph transition is running.
func (b Budgets) ResolveGraphAction(ctx context.Context, target BudgetGraph, held bool, guards ...storage.SettingRevision) (bool, error) {
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
	}, guards...)
	return changed && err == nil, err
}

// AuditRaise records the immediate results of the human's raise.
func (b Budgets) AuditRaise(ctx context.Context, scope, id, actor string, previous, limit int64, resumed, pending []string, skipped map[string]string) {
	b.publish(ctx, domain.NewEvent(domain.EvBudgetRaised, graphOf(scope, id), map[string]any{
		"scope": scope, "id": id, "previousMicros": previous, "limitMicros": limit,
		"resumed": resumed, "notResumed": pending, "skipped": skipped,
	}).WithActor(actor))
}
