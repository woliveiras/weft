package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/llm"
)

// reserveMoney holds a task run's monetary budget for one model call before
// it is dispatched. It returns nil when no budget applies. Reaching the
// budget refuses the call and pauses the run until a person raises the
// budget or stops it; delegation children share their run's budget.
func (t *turnEnv) reserveMoney(ctx context.Context, provider string, req *llm.ChatRequest) (*cost.Reservation, error) {
	e := t.gr.e
	graph := t.gr.g.Metadata.ID
	if !strings.HasPrefix(graph, domain.TaskRunGraphPrefix) {
		return nil, nil
	}
	typ := ""
	if r, ok := e.opt.Models.(interface{ Type(string) string }); ok {
		typ = r.Type(provider)
	}
	budgets := cost.Budgets{Store: e.store, Bus: e.bus}
	prices := cost.Prices{Store: e.store}
	catalogue := func(ctx context.Context) (cost.Catalogue, error) {
		c, _, err := prices.Get(ctx)
		return c, err
	}
	target := graphBudgetTarget(t.gr.g)
	target.Execution = t.x.ID
	r, err := budgets.Reserve(ctx, target, catalogue, provider, typ, req)
	var reached *cost.ReachedError
	if errors.As(err, &reached) {
		err = errors.Join(err, t.pauseForBudget(ctx, reached.Scope, reached.ID, reached.View.Limit))
	}
	return r, err
}

// graphBudgetTarget attributes a task run's calls to its user, project and
// source workflow (labels set when the run was prepared).
func graphBudgetTarget(g *domain.Graph) cost.Target {
	labels := g.Metadata.Labels
	return cost.Target{Graph: g.Metadata.ID,
		User: labels[domain.LabelTaskUser], Project: labels[domain.LabelTaskProject], Workflow: labels[domain.LabelTaskWorkflow]}
}

// pauseForBudget pauses the run until a person raises the budget.
func (t *turnEnv) pauseForBudget(ctx context.Context, scope, id string, limit int64) error {
	e := t.gr.e
	target := cost.BudgetGraph{
		Scope: scope, ID: id, Graph: t.gr.g.Metadata.ID,
		GraphAction: cost.GraphAction{Action: cost.PauseGraph, RunID: t.x.RunID, Actor: "system:budget"},
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := e.monetaryBudgets().MarkPaused(work, target, limit); err != nil {
		return err
	}
	outcome, err := e.waitBudgetGraphTransition(work, target)
	if errors.Is(err, ErrGraphTransitionBusy) {
		return nil // The reconciler will apply the persisted pause.
	}
	if err != nil {
		return err
	}
	if outcome != "" {
		return e.auditBudgetTransition(work, target, outcome)
	}
	return nil
}

// settleMoney charges a call's reported usage, or its whole reservation when
// usage is unknown. A settlement failure is returned, never hidden. Passing
// a budget's interrupt level ends the call with an error and pauses the run.
func (t *turnEnv) settleMoney(ctx context.Context, r *cost.Reservation, resp *llm.ChatResponse) error {
	err := r.Settle(ctx, resp)
	var interrupt *cost.InterruptError
	if errors.As(err, &interrupt) {
		err = errors.Join(err, t.pauseForBudget(ctx, interrupt.Scope, interrupt.ID, interrupt.Limit))
	}
	return err
}
