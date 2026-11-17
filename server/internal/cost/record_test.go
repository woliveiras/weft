package cost_test

import (
	"testing"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/llm"
)

func TestRecorderPricesGatewayCallsOnly(t *testing.T) {
	st := store(t)
	prices := cost.Prices{Store: st}
	if _, err := prices.Put(ctx, cost.CatalogueInput{Currency: "EUR", Prices: []cost.Price{{Provider: "team", Model: "*", InputMicrosPerMTok: 2_000_000, OutputMicrosPerMTok: 8_000_000}}}, "admin", 0); err != nil {
		t.Fatal(err)
	}
	r := cost.Recorder{Prices: prices}
	call := llm.Call{Source: "gateway", Provider: "team", Type: "openai", Model: "gpt-x", Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100},
		Metadata: map[string]string{"execution": "x1"}}
	rec := r.Price(ctx, call)
	if !rec.Priced || rec.RateSource != "catalogue" || rec.PriceVersion != 1 || rec.Currency != "EUR" || rec.CostMicros != 2000+800 || rec.Execution != "x1" {
		t.Fatalf("priced = %+v", rec)
	}
	call.Estimated = true
	if rec := r.Price(ctx, call); !rec.Estimated || rec.CostMicros != 2800 {
		t.Fatalf("estimated usage is charged like reported usage: %+v", rec)
	}
	if rec := r.Price(ctx, llm.Call{Source: "gateway", Provider: "local", Type: "ollama", Model: "llama3.2", Usage: llm.Usage{InputTokens: 50}}); !rec.Priced || rec.RateSource != "local" || rec.CostMicros != 0 {
		t.Fatalf("local = %+v", rec)
	}
	if rec := r.Price(ctx, llm.Call{Source: "gateway", Provider: "claude", Type: "anthropic", Model: "c", Usage: llm.Usage{InputTokens: 50}}); rec.Priced || rec.RateSource != "unpriced" {
		t.Fatalf("unpriced = %+v", rec)
	}
	// External usage names no provider the gateway served: never priced.
	if rec := r.Price(ctx, llm.Call{Source: "external", Usage: llm.Usage{InputTokens: 50}}); rec.Priced || rec.CostMicros != 0 {
		t.Fatalf("external = %+v", rec)
	}
	// An unreadable catalogue leaves the call unpriced, and says so.
	if _, err := st.PutSetting(ctx, cost.PricesKey, []byte("{"), 0, "x"); err == nil {
		t.Fatal("expected conflict on existing key")
	}
	cur, _ := st.GetSetting(ctx, cost.PricesKey)
	if _, err := st.PutSetting(ctx, cost.PricesKey, []byte("{"), cur.Revision, "x"); err != nil {
		t.Fatal(err)
	}
	if rec := r.Price(ctx, call); rec.Priced || rec.Error == "" {
		t.Fatalf("corrupt catalogue = %+v", rec)
	}
}
