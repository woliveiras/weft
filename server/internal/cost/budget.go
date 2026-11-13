package cost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

// PolicyKey holds installation cost policy; BudgetPrefix one ledger per
// budget ("cost/budget/run/<graph>" for a task run).
const (
	PolicyKey    = "cost/policy"
	BudgetPrefix = "cost/budget/"
	// MaxBudgetMicros bounds any budget (one billion currency units).
	MaxBudgetMicros = int64(1_000_000_000) * 1_000_000
	// maxEntries bounds a ledger; older settled entries are folded into
	// Compacted (their ids no longer deduplicate, which is safe: settlement
	// only happens once per reservation in practice).
	maxEntries = 256
)

// ErrBudgetReached refuses a call that would exceed a budget.
var ErrBudgetReached = errors.New("monetary budget reached")

// ErrUnpriced refuses an unpriced call while a budget applies and the
// policy says so.
var ErrUnpriced = errors.New("model is unpriced and the price policy refuses unpriced calls under a budget")

// Policy is the installation cost policy.
type Policy struct {
	// TaskRunBudgetMicros is the budget each task run starts with; 0 means
	// task runs have no monetary budget.
	TaskRunBudgetMicros int64 `json:"taskRunBudgetMicros"`
	// WarningPercent of a budget emits budget.warning once (default 80).
	WarningPercent int        `json:"warningPercent"`
	UpdatedAt      *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy      string     `json:"updatedBy,omitempty"`
}

// Entry is one reservation in a ledger.
type Entry struct {
	Execution string `json:"execution,omitempty"`
	Amount    int64  `json:"amount"`
	Settled   bool   `json:"settled"`
	Charged   int64  `json:"charged"`
	Estimated bool   `json:"estimated"`
}

// Ledger is a budget's stored state.
type Ledger struct {
	Scope     string           `json:"scope"`
	ID        string           `json:"id"`
	Currency  string           `json:"currency"`
	Limit     int64            `json:"limit"`
	Compacted int64            `json:"compacted,omitempty"`
	Entries   map[string]Entry `json:"entries"`
	// Warned and Reached record that the events were emitted for the
	// current limit; raising the limit clears them.
	Warned  bool `json:"warned,omitempty"`
	Reached bool `json:"reached,omitempty"`
	// InterruptPercent (> 100) cancels a running call whose settlement
	// takes the budget past it; 0 means no interrupt level.
	InterruptPercent int `json:"interruptPercent,omitempty"`
	WarningPercent   int `json:"warningPercent,omitempty"`
	// PausedGraphs includes runs waiting for a pause or resume to apply.
	PausedGraphs []string               `json:"pausedGraphs,omitempty"`
	GraphActions map[string]GraphAction `json:"graphActions,omitempty"`
	RaisedAt     *time.Time             `json:"raisedAt,omitempty"`
	RaisedBy     string                 `json:"raisedBy,omitempty"`
}

// View summarises a ledger.
type View struct {
	Scope      string   `json:"scope"`
	ID         string   `json:"id"`
	Currency   string   `json:"currency"`
	Limit      int64    `json:"limitMicros"`
	Charged    int64    `json:"chargedMicros"`
	Estimated  int64    `json:"estimatedMicros"`
	Reserved   int64    `json:"reservedMicros"`
	Remaining  int64    `json:"remainingMicros"`
	Reached    bool     `json:"reached"`
	Paused     bool     `json:"paused"`
	RaisedBy   string   `json:"raisedBy,omitempty"`
	NotResumed []string `json:"notResumed,omitempty"`
}

// Warning is the warning threshold in micro-units.
func (l Ledger) warningAt() int64 {
	p := l.WarningPercent
	if p == 0 {
		p = 80
	}
	return l.Limit * int64(p) / 100
}

// View computes the ledger's totals.
func (l Ledger) View() View {
	v := View{Scope: l.Scope, ID: l.ID, Currency: l.Currency, Limit: l.Limit, Charged: l.Compacted, Reached: l.Reached, Paused: len(l.PausedGraphs) > 0, RaisedBy: l.RaisedBy}
	for _, e := range l.Entries {
		if e.Settled {
			v.Charged += e.Charged
			if e.Estimated {
				v.Estimated += e.Charged
			}
		} else {
			v.Reserved += e.Amount
		}
	}
	for _, graph := range l.PausedGraphs {
		if l.GraphActions[graph].Action == ResumeGraph {
			v.NotResumed = append(v.NotResumed, graph)
		}
	}
	v.Remaining = max(v.Limit-v.Charged-v.Reserved, 0)
	return v
}

func (l Ledger) used() int64 { v := l.View(); return v.Charged + v.Reserved }

// Budgets manages policy and ledgers.
type Budgets struct {
	Store storage.Store
	Bus   *events.Bus // optional
}

// Policy returns the installation policy and its revision.
func (b Budgets) Policy(ctx context.Context) (Policy, int64, error) {
	st, err := b.Store.GetSetting(ctx, PolicyKey)
	if errors.Is(err, storage.ErrNotFound) {
		return Policy{WarningPercent: 80}, 0, nil
	}
	if err != nil {
		return Policy{}, 0, fmt.Errorf("cost: read policy: %w", err)
	}
	var p Policy
	if err := json.Unmarshal(st.Value, &p); err != nil || p.TaskRunBudgetMicros < 0 || p.TaskRunBudgetMicros > MaxBudgetMicros || p.WarningPercent < 1 || p.WarningPercent > 100 {
		return Policy{}, 0, errors.New("cost: the stored cost policy is corrupt")
	}
	return p, st.Revision, nil
}

// SetPolicy validates and stores the policy. Existing ledgers keep their
// limits; the new default applies to task runs that start afterwards.
func (b Budgets) SetPolicy(ctx context.Context, p Policy, actor string, ifRevision int64) (Policy, error) {
	ve := &ValidationError{}
	if p.TaskRunBudgetMicros < 0 || p.TaskRunBudgetMicros > MaxBudgetMicros {
		ve.Errors = append(ve.Errors, Issue{"taskRunBudgetMicros", fmt.Sprintf("must be between 0 (no budget) and %d", MaxBudgetMicros)})
	}
	if p.WarningPercent == 0 {
		p.WarningPercent = 80
	}
	if p.WarningPercent < 1 || p.WarningPercent > 100 {
		ve.Errors = append(ve.Errors, Issue{"warningPercent", "must be between 1 and 100"})
	}
	if len(ve.Errors) > 0 {
		return Policy{}, ve
	}
	_, rev, err := b.Policy(ctx)
	if err != nil {
		return Policy{}, err
	}
	if ifRevision > 0 && ifRevision != rev {
		return Policy{}, fmt.Errorf("%w: cost policy changed (revision %d, you edited %d); reload", storage.ErrConflict, rev, ifRevision)
	}
	now := time.Now().UTC()
	p.UpdatedAt, p.UpdatedBy = &now, actor
	raw, _ := json.Marshal(p)
	if _, err := b.Store.PutSetting(ctx, PolicyKey, raw, rev, actor); err != nil {
		return Policy{}, fmt.Errorf("cost: store policy: %w", err)
	}
	b.publish(ctx, domain.NewEvent(domain.EvBudgetChanged, "", map[string]any{"scope": "policy", "taskRunBudgetMicros": p.TaskRunBudgetMicros, "warningPercent": p.WarningPercent}).WithActor(actor))
	return p, nil
}

func ledgerKey(scope, id string) string { return BudgetPrefix + scope + "/" + id }

func (b Budgets) read(ctx context.Context, scope, id string) (*Ledger, int64, error) {
	st, err := b.Store.GetSetting(ctx, ledgerKey(scope, id))
	if errors.Is(err, storage.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var l Ledger
	if err := json.Unmarshal(st.Value, &l); err != nil || l.Limit < 0 || l.Limit > MaxBudgetMicros || l.Entries == nil || len(l.Entries) > maxEntries {
		return nil, 0, errors.New("cost: a stored budget ledger is corrupt")
	}
	for id, e := range l.Entries {
		if id == "" || e.Amount < 0 || e.Charged < 0 || (!e.Settled && e.Charged != 0) {
			return nil, 0, errors.New("cost: a stored budget reservation is corrupt")
		}
	}
	return &l, st.Revision, nil
}

// Ledger returns a budget's view, or nil when it does not exist.
func (b Budgets) Ledger(ctx context.Context, scope, id string) (*View, error) {
	l, _, err := b.read(ctx, scope, id)
	if err != nil || l == nil {
		return nil, err
	}
	v := l.View()
	return &v, nil
}

// update applies change with compare-and-set so concurrent reservations
// across instances can never overshoot a budget. change returns nil for
// "no write".
func (b Budgets) update(ctx context.Context, scope, id, op string, change func(*Ledger) (*Ledger, error)) (retErr error) {
	started := time.Now()
	defer func() {
		outcome := "ok"
		switch {
		case errors.Is(retErr, ErrBudgetReached):
			outcome = "reached"
		case retErr != nil:
			outcome = "error"
		}
		observability.BudgetOperations.WithLabelValues(scope, op, outcome).Inc()
		observability.BudgetSeconds.WithLabelValues(op).Observe(time.Since(started).Seconds())
	}()
	for i := 0; i < 100; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		l, rev, err := b.read(ctx, scope, id)
		if err != nil {
			return err
		}
		next, err := change(l)
		if err != nil || next == nil {
			return err
		}
		if len(next.Entries) > maxEntries {
			compact(next)
		}
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		_, err = b.Store.PutSetting(ctx, ledgerKey(scope, id), raw, rev, "system:cost-budget")
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("%w: budget contention", storage.ErrConflict)
}

// compact folds settled entries into the compacted total until the ledger
// fits; unsettled reservations are always kept.
func compact(l *Ledger) {
	for id, e := range l.Entries {
		if len(l.Entries) <= maxEntries/2 {
			return
		}
		if e.Settled {
			l.Compacted += e.Charged
			delete(l.Entries, id)
		}
	}
}

// Reservation is money held against every budget that applies to one call.
type Reservation struct {
	b      Budgets
	parts  []part
	rate   Rate
	amount int64
}

type part struct{ scope, id, rid string }

// Rate is the rate the reservation was priced with; settlement uses it so
// a price change applies only to later calls.
func (r *Reservation) Rate() Rate { return r.rate }

// Amount reserved, in micro-units.
func (r *Reservation) Amount() int64 { return r.amount }

// Scopes.
const (
	TaskRun      = "run" // one task run (its execution graph id)
	Installation = "installation"
	User         = "user"
	Project      = "project"
	Workflow     = "workflow"
)

// Target identifies who and what a call is for.
type Target struct {
	Graph     string // execution graph (a task run)
	Execution string
	User      string
	Project   string
	Workflow  string
}

// Period is the calendar month (UTC) monthly budgets are kept for.
func Period(t time.Time) string { return t.UTC().Format("2006-01") }

// MonthlyID is the ledger id of a monthly budget.
func MonthlyID(id, period string) string { return id + "@" + period }

type applicable struct {
	scope, id string
	limit     int64
	warning   int
	interrupt int
	raised    bool
}

// applicable lists the budgets that apply to a target now.
func (b Budgets) applicable(ctx context.Context, t Target, now time.Time) ([]applicable, error) {
	policy, _, err := b.Policy(ctx)
	if err != nil {
		return nil, err
	}
	var out []applicable
	if t.Graph != "" {
		existing, _, err := b.read(ctx, TaskRun, t.Graph)
		if err != nil {
			return nil, err
		}
		if existing != nil || policy.TaskRunBudgetMicros > 0 {
			out = append(out, applicable{scope: TaskRun, id: t.Graph, limit: policy.TaskRunBudgetMicros, warning: policy.WarningPercent})
		}
	}
	defs, _, err := b.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	period := Period(now)
	for _, d := range defs {
		match := ""
		switch d.Scope {
		case Installation:
			match = "*"
		case User:
			match = t.User
		case Project:
			match = t.Project
		case Workflow:
			match = t.Workflow
		}
		if match == "" || match != d.ID {
			continue
		}
		warning := d.WarningPercent
		if warning == 0 {
			warning = policy.WarningPercent
		}
		out = append(out, applicable{scope: d.Scope, id: MonthlyID(d.ID, period), limit: d.LimitMicros, warning: warning, interrupt: d.InterruptPercent})
	}
	return out, nil
}

// Reserve holds the maximum cost of req against every budget that applies
// to the target. It returns nil when none applies; the catalogue is read
// only when one does. If any budget refuses, the parts already reserved are
// released and nothing is held. The request's output allowance is made
// finite so the reservation is a real ceiling.
func (b Budgets) Reserve(ctx context.Context, t Target, catalogue func(context.Context) (Catalogue, error), provider, typ string, req *llm.ChatRequest) (*Reservation, error) {
	apply, err := b.applicable(ctx, t, time.Now())
	if err != nil || len(apply) == 0 {
		return nil, err
	}
	cat, err := catalogue(ctx)
	if err != nil {
		// Fail closed: a budget applies and the call cannot be priced.
		return nil, err
	}
	rate := cat.Lookup(provider, typ, req.Model)
	if !rate.Priced() && cat.Unpriced == UnpricedRefuse {
		b.publish(ctx, domain.NewEvent(domain.EvBudgetReservationFailed, t.Graph, map[string]any{"scope": apply[0].scope, "id": apply[0].id, "reason": "unpriced", "provider": provider, "model": req.Model}).WithExecution(t.Execution))
		return nil, fmt.Errorf("%w: %s/%s", ErrUnpriced, provider, req.Model)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = llm.DefaultEstimatedOutput
	}
	var amount int64
	if rate.Priced() {
		amount = rate.Cost(llm.Usage{InputTokens: llm.EstimateTokens(req), OutputTokens: req.MaxTokens})
	}
	r := &Reservation{b: b, rate: rate, amount: amount}
	rid := domain.NewID("cost")
	for _, a := range apply {
		if err := b.reservePart(ctx, a, t, cat.Currency, rid, amount); err != nil {
			return nil, errors.Join(err, r.Release(ctx))
		}
		r.parts = append(r.parts, part{scope: a.scope, id: a.id, rid: rid})
	}
	return r, nil
}

// ReserveTaskRun reserves against the task run's budget only.
func (b Budgets) ReserveTaskRun(ctx context.Context, graph, execution string, catalogue func(context.Context) (Catalogue, error), provider, typ string, req *llm.ChatRequest) (*Reservation, error) {
	return b.Reserve(ctx, Target{Graph: graph, Execution: execution}, catalogue, provider, typ, req)
}

func (b Budgets) reservePart(ctx context.Context, a applicable, t Target, currency, rid string, amount int64) error {
	var warn, refused, firstReach bool
	var view View
	err := b.update(ctx, a.scope, a.id, "reserve", func(l *Ledger) (*Ledger, error) {
		if l == nil {
			l = &Ledger{Scope: a.scope, ID: a.id, Currency: currency, Limit: a.limit, Entries: map[string]Entry{}}
		}
		// A definition change applies to the current period unless a
		// person raised this ledger above it.
		if a.scope != TaskRun && (l.RaisedBy == "" || a.limit > l.Limit) && l.Limit != a.limit {
			l.Limit, l.Reached = a.limit, false
		}
		l.WarningPercent, l.InterruptPercent = a.warning, a.interrupt
		warn, refused, firstReach = false, false, false
		// Free calls (local or allowed unpriced models) pass even at the limit.
		if amount > 0 && (l.Reached || l.used()+amount > l.Limit) {
			refused, firstReach = true, !l.Reached
			l.Reached = true
			view = l.View()
			if firstReach {
				return l, nil // persist Reached; the call is refused below
			}
			return nil, nil
		}
		l.Entries[rid] = Entry{Execution: t.Execution, Amount: amount}
		if !l.Warned && l.Limit > 0 && l.used() >= l.warningAt() {
			l.Warned, warn = true, true
		}
		view = l.View()
		return l, nil
	})
	if err != nil {
		return err
	}
	if warn {
		b.publish(ctx, domain.NewEvent(domain.EvBudgetWarning, t.Graph, view).WithExecution(t.Execution))
	}
	if refused {
		if firstReach {
			b.publish(ctx, domain.NewEvent(domain.EvBudgetReached, t.Graph, view).WithExecution(t.Execution))
		}
		return &ReachedError{Scope: a.scope, ID: a.id, View: view}
	}
	return nil
}

// ReachedError says which budget refused a call.
type ReachedError struct {
	Scope, ID string
	View      View
}

func (e *ReachedError) Error() string {
	return fmt.Sprintf("%s: %s budget %s has %d of %d %s micro-units left; an administrator can raise it",
		ErrBudgetReached, e.Scope, e.ID, e.View.Remaining, e.View.Limit, e.View.Currency)
}

func (e *ReachedError) Unwrap() error { return ErrBudgetReached }

// ErrInterrupted cancels a run whose spend passed a budget's interrupt level.
var ErrInterrupted = errors.New("monetary budget interrupt level passed")

// Settle charges the reported usage (or keeps the whole reservation as an
// estimated charge when usage is unknown) and releases the rest. It is
// idempotent per reservation. It returns an *InterruptError when the charge
// takes a budget past its interrupt level.
func (r *Reservation) Settle(ctx context.Context, resp *llm.ChatResponse) error {
	if r == nil {
		return nil
	}
	charged, estimated := r.amount, true
	if resp != nil && (resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0) {
		charged, estimated = 0, false
		if r.rate.Priced() {
			charged = r.rate.Cost(resp.Usage)
		}
	}
	return r.settle(ctx, charged, estimated)
}

// Release returns the whole reservation: the call was never dispatched.
func (r *Reservation) Release(ctx context.Context) error {
	if r == nil {
		return nil
	}
	return r.settle(ctx, 0, false)
}

// InterruptError names the budget whose interrupt level a charge passed.
type InterruptError struct {
	Scope, ID string
	Limit     int64
}

func (e *InterruptError) Error() string {
	return fmt.Sprintf("%s: %s budget %s", ErrInterrupted, e.Scope, e.ID)
}

func (e *InterruptError) Unwrap() error { return ErrInterrupted }

func (r *Reservation) settle(ctx context.Context, charged int64, estimated bool) error {
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	var errs []error
	var interrupt error
	for _, p := range r.parts {
		var interruptLimit int64
		passed := false
		errs = append(errs, r.b.update(cleanup, p.scope, p.id, "settle", func(l *Ledger) (*Ledger, error) {
			if l == nil {
				return nil, errors.New("cost: settling a reservation of a missing budget")
			}
			e, ok := l.Entries[p.rid]
			if !ok || e.Settled {
				return nil, nil
			}
			e.Settled, e.Charged, e.Estimated = true, charged, estimated
			l.Entries[p.rid] = e
			interruptLimit = l.Limit
			passed = l.InterruptPercent > 100 && charged > 0 && l.View().Charged*100 >= l.Limit*int64(l.InterruptPercent)
			return l, nil
		}))
		if passed && interrupt == nil {
			interrupt = &InterruptError{Scope: p.scope, ID: p.id, Limit: interruptLimit}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return interrupt
}

// SettleExecution closes reservations left open by an execution that ended
// (crash or restart between dispatch and settlement): they are charged in
// full as estimated, never refunded.
func (b Budgets) SettleExecution(ctx context.Context, scope, id, execution string) error {
	return b.update(ctx, scope, id, "reconcile", func(l *Ledger) (*Ledger, error) {
		if l == nil {
			return nil, nil
		}
		changed := false
		for rid, e := range l.Entries {
			if !e.Settled && e.Execution == execution {
				e.Settled, e.Charged, e.Estimated = true, e.Amount, true
				l.Entries[rid] = e
				changed = true
			}
		}
		if !changed {
			return nil, nil
		}
		return l, nil
	})
}

func graphOf(scope, id string) string {
	if scope == TaskRun {
		return id
	}
	return ""
}

func (b Budgets) publish(ctx context.Context, ev *domain.Event) {
	if b.Bus != nil {
		b.Bus.Publish(context.WithoutCancel(ctx), ev)
	}
}

// Reconcile settles reservations left open by executions that have ended
// (for example after a restart between dispatch and settlement).
func (b Budgets) Reconcile(ctx context.Context, ended func(ctx context.Context, execution string) bool) error {
	rows, err := b.Store.ListSettings(ctx, BudgetPrefix)
	if err != nil {
		return err
	}
	var errs []error
	for _, row := range rows {
		scope, id, ok := strings.Cut(strings.TrimPrefix(row.Key, BudgetPrefix), "/")
		if !ok {
			continue
		}
		l, _, err := b.read(ctx, scope, id)
		if err != nil || l == nil {
			errs = append(errs, err)
			continue
		}
		done := map[string]bool{}
		for _, e := range l.Entries {
			if !e.Settled && e.Execution != "" && !done[e.Execution] && ended(ctx, e.Execution) {
				done[e.Execution] = true
				errs = append(errs, b.SettleExecution(ctx, scope, id, e.Execution))
			}
		}
	}
	return errors.Join(errs...)
}
