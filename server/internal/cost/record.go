package cost

import (
	"context"
	"log/slog"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/observability"
)

// Record is one priced model call, published as a cost.recorded event. It
// never holds prompts, completions or credentials.
type Record struct {
	Source       string    `json:"source"` // gateway | external
	Provider     string    `json:"provider,omitempty"`
	Type         string    `json:"type,omitempty"`
	Model        string    `json:"model,omitempty"`
	Execution    string    `json:"execution,omitempty"`
	Usage        llm.Usage `json:"usage"`
	Estimated    bool      `json:"estimated"`
	Priced       bool      `json:"priced"`
	RateSource   string    `json:"rateSource"`
	PriceVersion int       `json:"priceVersion"`
	CostMicros   int64     `json:"costMicros"`
	Currency     string    `json:"currency"`
	Error        string    `json:"error,omitempty"`
}

// Recorder prices calls the LLM gateway reports (llm.Registry.SetObserver)
// and publishes them.
type Recorder struct {
	Prices Prices
	Bus    *events.Bus
	Log    *slog.Logger
}

// Price computes the record for a call with the current catalogue. A
// catalogue that cannot be read leaves the call unpriced, said so in Error,
// rather than failing a call that already happened.
func (r Recorder) Price(ctx context.Context, c llm.Call) Record {
	rec := Record{Source: c.Source, Provider: c.Provider, Type: c.Type, Model: c.Model, Execution: c.Metadata["execution"],
		Usage: c.Usage, Estimated: c.Estimated, RateSource: "unpriced"}
	cat, _, err := r.Prices.Get(ctx)
	if err != nil {
		rec.Error = "price catalogue unavailable"
		cat = Default()
		if r.Log != nil {
			r.Log.Error("cost record left unpriced: price catalogue unavailable", "provider", c.Provider, "model", c.Model, "err", err)
		}
	}
	rec.Currency, rec.PriceVersion = cat.Currency, cat.Version
	if err == nil && c.Source == "gateway" {
		rate := cat.Lookup(c.Provider, c.Type, c.Model)
		rec.RateSource, rec.Priced = rate.Source, rate.Priced()
		if rec.Priced {
			rec.CostMicros = rate.Cost(c.Usage)
		}
	}
	return rec
}

// Observe records one call: it prices it, publishes cost.recorded on the
// call's graph/node/execution, and counts it in bounded metrics.
func (r Recorder) Observe(ctx context.Context, c llm.Call) {
	rec := r.Price(ctx, c)
	priced := "unpriced"
	switch {
	case rec.Estimated:
		priced = "estimated"
	case rec.Priced:
		priced = "priced"
	}
	observability.CostCalls.WithLabelValues(c.Provider, c.Model, priced).Inc()
	observability.CostMicros.WithLabelValues(c.Provider, c.Model).Add(float64(rec.CostMicros))
	if r.Bus == nil {
		return
	}
	ev := domain.NewEvent(domain.EvCostRecorded, c.Metadata["graph"], rec)
	if node := c.Metadata["node"]; node != "" {
		ev = ev.WithNode(node, 0)
	}
	if rec.Execution != "" {
		ev = ev.WithExecution(rec.Execution)
	}
	r.Bus.Publish(context.WithoutCancel(ctx), ev.WithActor("system"))
}
