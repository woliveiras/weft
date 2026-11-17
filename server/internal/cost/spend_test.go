package cost_test

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestSpendAggregatesTheMonthByProviderTaskUserProjectAndWorkflow(t *testing.T) {
	st := store(t)
	prices := cost.Prices{Store: st}
	if _, err := prices.Put(ctx, cost.CatalogueInput{Currency: "USD", Prices: []cost.Price{{Provider: "team", Model: "*", InputMicrosPerMTok: 1_000_000, OutputMicrosPerMTok: 1_000_000}}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "task-run-1", Name: "Run", Labels: map[string]string{
		domain.LabelTaskUser: "alice", domain.LabelTaskProject: "prj_1", domain.LabelTaskWorkflow: "factory"}}}
	if _, err := st.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := cost.Recorder{Prices: prices, Bus: bus}
	meta := map[string]string{"graph": "task-run-1", "node": "n", "execution": "x1"}
	rec.Observe(ctx, llm.Call{Source: "gateway", Provider: "team", Type: "openai", Model: "big", Usage: llm.Usage{InputTokens: 100, OutputTokens: 20}, Metadata: meta})
	rec.Observe(ctx, llm.Call{Source: "gateway", Provider: "team", Type: "openai", Model: "big", Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}, Estimated: true, Metadata: meta})
	rec.Observe(ctx, llm.Call{Source: "gateway", Provider: "claude", Type: "anthropic", Model: "c", Usage: llm.Usage{InputTokens: 7}, Metadata: map[string]string{"graph": "plain-graph"}})
	rec.Observe(ctx, llm.Call{Source: "external", Usage: llm.Usage{InputTokens: 3}, Metadata: meta})

	rep, err := cost.Spend(ctx, st, cost.Period(time.Now()), "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Currency != "USD" || rep.Total.Calls != 4 || rep.Total.CostMicros != 120+15 || rep.Total.EstimatedCost != 15 || rep.Total.UnpricedCalls != 2 {
		t.Fatalf("total = %+v", rep.Total)
	}
	if len(rep.ByProvider) < 2 || rep.ByProvider[0].Key != "team" || rep.ByProvider[0].Model != "big" || rep.ByProvider[0].Calls != 2 || rep.ByProvider[0].CostMicros != 135 {
		t.Fatalf("by provider = %+v", rep.ByProvider)
	}
	for name, lines := range map[string][]cost.Line{"user": rep.ByUser, "project": rep.ByProject, "workflow": rep.ByWorkflow} {
		if len(lines) != 1 || lines[0].CostMicros != 135 || lines[0].Calls != 3 {
			t.Fatalf("by %s = %+v", name, lines)
		}
	}
	if rep.ByUser[0].Key != "alice" || rep.ByProject[0].Key != "prj_1" || rep.ByWorkflow[0].Key != "factory" {
		t.Fatalf("keys: %+v %+v %+v", rep.ByUser, rep.ByProject, rep.ByWorkflow)
	}
	if len(rep.ByTask) != 2 || rep.ByTask[0].Key != "task-run-1" {
		t.Fatalf("by task = %+v", rep.ByTask)
	}
	// One graph only.
	one, err := cost.Spend(ctx, st, cost.Period(time.Now()), "task-run-1")
	if err != nil || one.Total.Calls != 3 {
		t.Fatalf("graph spend = %+v %v", one.Total, err)
	}
	// Another month is empty; a bad period is a validation error.
	if prev, err := cost.Spend(ctx, st, "2001-01", ""); err != nil || prev.Total.Calls != 0 {
		t.Fatalf("old month = %+v %v", prev.Total, err)
	}
	var ve *cost.ValidationError
	if _, err := cost.Spend(ctx, st, "November", ""); !errors.As(err, &ve) {
		t.Fatalf("bad period: %v", err)
	}
}

func TestPreviewAndLedgers(t *testing.T) {
	_, b := budgetEnv(t, 100)
	if _, err := b.SetDefinitions(ctx, []cost.Definition{{Scope: cost.User, ID: "alice", LimitMicros: 500}, {Scope: cost.Workflow, ID: "other", LimitMicros: 9}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	views, err := b.Preview(ctx, cost.Target{User: "alice", Workflow: "factory"})
	if err != nil || len(views) != 1 || views[0].Scope != cost.User || views[0].Remaining != 500 || views[0].Currency != "USD" {
		t.Fatalf("preview before spending = %+v %v", views, err)
	}
	r, err := b.Reserve(ctx, cost.Target{Graph: "task-run-1", Execution: "x", User: "alice"}, cat(flat), "team", "openai", request(30))
	if err != nil {
		t.Fatal(err)
	}
	views, _ = b.Preview(ctx, cost.Target{User: "alice"})
	if views[0].Remaining != 500-r.Amount() {
		t.Fatalf("preview after = %+v", views)
	}
	period := cost.Period(time.Now())
	all, err := b.Ledgers(ctx, period)
	if err != nil || len(all) != 1 || all[0].Scope != cost.User {
		t.Fatalf("ledgers (a healthy run ledger is not listed) = %+v %v", all, err)
	}
	if _, err := b.Reserve(ctx, cost.Target{Graph: "task-run-1", Execution: "x"}, cat(flat), "team", "openai", request(100)); !errors.Is(err, cost.ErrBudgetReached) {
		t.Fatalf("expected the run budget to refuse: %v", err)
	}
	all, _ = b.Ledgers(ctx, period)
	if len(all) != 2 {
		t.Fatalf("a reached run ledger needs a person and is listed: %+v", all)
	}
	if none, _ := b.Ledgers(ctx, "2001-01"); len(none) != 1 || none[0].Scope != cost.TaskRun {
		t.Fatalf("other month = %+v", none)
	}
}
