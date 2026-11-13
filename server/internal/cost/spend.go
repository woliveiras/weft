package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Line is spend aggregated for one key (a provider/model, task run, user,
// project or workflow).
type Line struct {
	Key           string `json:"key"`
	Model         string `json:"model,omitempty"`
	Calls         int    `json:"calls"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
	CostMicros    int64  `json:"costMicros"`
	EstimatedCost int64  `json:"estimatedMicros"`
	UnpricedCalls int    `json:"unpricedCalls"`
}

// Report is spend for one calendar month (UTC), from cost.recorded events.
type Report struct {
	Period     string `json:"period"`
	Currency   string `json:"currency"`
	Total      Line   `json:"total"`
	ByProvider []Line `json:"byProvider"`
	ByTask     []Line `json:"byTask"`
	ByUser     []Line `json:"byUser"`
	ByProject  []Line `json:"byProject"`
	ByWorkflow []Line `json:"byWorkflow"`
	// Truncated: the event history held more records than were read.
	Truncated bool `json:"truncated,omitempty"`
}

// maxRecords bounds the events one report reads.
const maxRecords = 100_000

func (l *Line) add(r Record) {
	l.Calls++
	l.InputTokens += int64(r.Usage.InputTokens)
	l.OutputTokens += int64(r.Usage.OutputTokens)
	l.CostMicros += r.CostMicros
	if r.Estimated {
		l.EstimatedCost += r.CostMicros
	}
	if !r.Priced {
		l.UnpricedCalls++
	}
}

// Spend aggregates the cost records of a month, optionally for one graph.
// Records follow the event history retention.
func Spend(ctx context.Context, store storage.Store, period, graph string) (Report, error) {
	start, err := time.Parse("2006-01", period)
	if err != nil {
		return Report{}, &ValidationError{Errors: []Issue{{"period", "use YYYY-MM"}}}
	}
	end := start.AddDate(0, 1, 0)
	cat, _, err := (Prices{Store: store}).Get(ctx)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Period: period, Currency: cat.Currency}
	groups := map[string]map[string]*Line{}
	bump := func(dim, key, model string, r Record) {
		if key == "" {
			return
		}
		if groups[dim] == nil {
			groups[dim] = map[string]*Line{}
		}
		id := key + "\x00" + model
		l := groups[dim][id]
		if l == nil {
			l = &Line{Key: key, Model: model}
			groups[dim][id] = l
		}
		l.add(r)
	}
	labels := map[string]map[string]string{}
	labelsOf := func(graph string) map[string]string {
		if l, ok := labels[graph]; ok {
			return l
		}
		g, err := store.GetGraph(ctx, graph, 0)
		if err != nil {
			labels[graph] = nil
			return nil
		}
		labels[graph] = g.Metadata.Labels
		return g.Metadata.Labels
	}
	var after int64
	read := 0
	for {
		evs, err := store.ListEvents(ctx, storage.EventFilter{GraphID: graph, Types: []domain.EventType{domain.EvCostRecorded}, AfterSeq: after, Limit: 5000, Ascending: true})
		if err != nil {
			return Report{}, fmt.Errorf("cost: read cost records: %w", err)
		}
		for _, ev := range evs {
			after = ev.Seq
			if ev.Time.Before(start) || !ev.Time.Before(end) {
				continue
			}
			var r Record
			if json.Unmarshal(ev.Data, &r) != nil {
				continue
			}
			if r.Currency != "" && r.Currency != rep.Currency && r.Priced {
				// A record priced in an earlier currency is kept out of the
				// totals rather than mixed in.
				continue
			}
			rep.Total.add(r)
			bump("provider", r.Provider, r.Model, r)
			if ev.GraphID != "" {
				bump("task", ev.GraphID, "", r)
				l := labelsOf(ev.GraphID)
				bump("user", l[domain.LabelTaskUser], "", r)
				bump("project", l[domain.LabelTaskProject], "", r)
				bump("workflow", l[domain.LabelTaskWorkflow], "", r)
			}
		}
		read += len(evs)
		if len(evs) < 5000 {
			break
		}
		if read >= maxRecords {
			rep.Truncated = true
			break
		}
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
	}
	sorted := func(dim string) []Line {
		out := make([]Line, 0, len(groups[dim]))
		for _, l := range groups[dim] {
			out = append(out, *l)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].CostMicros != out[j].CostMicros {
				return out[i].CostMicros > out[j].CostMicros
			}
			return out[i].Key+out[i].Model < out[j].Key+out[j].Model
		})
		if len(out) > 100 {
			out = out[:100]
		}
		return out
	}
	rep.ByProvider, rep.ByTask, rep.ByUser, rep.ByProject, rep.ByWorkflow = sorted("provider"), sorted("task"), sorted("user"), sorted("project"), sorted("workflow")
	rep.Total.Key = "total"
	return rep, nil
}

// Preview lists the budgets a new run for the target would be held to, with
// what remains this month (or the full limit before the first call).
func (b Budgets) Preview(ctx context.Context, t Target) ([]View, error) {
	apply, err := b.applicable(ctx, t, time.Now())
	if err != nil {
		return nil, err
	}
	cat, _, err := (Prices{Store: b.Store}).Get(ctx)
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, a := range apply {
		l, _, err := b.read(ctx, a.scope, a.id)
		if err != nil {
			return nil, err
		}
		if l == nil {
			out = append(out, View{Scope: a.scope, ID: a.id, Currency: cat.Currency, Limit: a.limit, Remaining: a.limit})
			continue
		}
		out = append(out, l.View())
	}
	return out, nil
}

// Ledgers lists monthly ledgers from the requested period and reached or
// paused task-run ledgers. Invalid rows stay visible for diagnosis.
func (b Budgets) Ledgers(ctx context.Context, period string) ([]View, error) {
	rows, err := b.Store.ListSettings(ctx, BudgetPrefix)
	if err != nil {
		return nil, err
	}
	out := []View{}
	current := Period(time.Now())
	for _, row := range rows {
		scope, id, ok := strings.Cut(strings.TrimPrefix(row.Key, BudgetPrefix), "/")
		if !ok {
			continue
		}
		if scope != TaskRun && !strings.HasSuffix(id, "@"+period) {
			continue
		}
		l, err := decodeLedger(row.Value, scope, id)
		if err != nil {
			b.auditInvalidLedger(ctx, scope, id, row.Revision)
			out = append(out, invalidLedgerView(scope, id, row.Revision))
			continue
		}
		if scope != TaskRun || l.Reached || len(l.PausedGraphs) != 0 {
			view, err := b.visibleView(ctx, l, current)
			if err != nil {
				return nil, err
			}
			out = append(out, view)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope+out[i].ID < out[j].Scope+out[j].ID })
	return out, nil
}
