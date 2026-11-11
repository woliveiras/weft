package cost_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/llm"
	_ "github.com/woliveiras/weft/server/internal/llm/providers"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

var ctx = context.Background()

func ptr(v int64) *int64 { return &v }

func store(t *testing.T) storage.Store {
	t.Helper()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestRateCostRoundsUpAndChargesCacheAtItsRate(t *testing.T) {
	r := cost.Rate{Source: "catalogue", Price: cost.Price{InputMicrosPerMTok: 3_000_000, OutputMicrosPerMTok: 15_000_000, CacheReadMicrosPerMTok: ptr(300_000)}}
	// 1000 input of which 400 cached reads; cache writes default to the input rate.
	got := r.Cost(llm.Usage{InputTokens: 1000, OutputTokens: 200, CacheReadTokens: 400, CacheWriteTokens: 100})
	// (500*3 + 200*15 + 400*0.3 + 100*3) micro-units per token-million => 1500+3000+120+300 = 4920 micros / ... per token
	want := int64((500*3_000_000 + 200*15_000_000 + 400*300_000 + 100*3_000_000 + 999_999) / 1_000_000)
	if got != want {
		t.Fatalf("cost = %d, want %d", got, want)
	}
	if one := r.Cost(llm.Usage{OutputTokens: 1}); one != 15 {
		t.Fatalf("one output token = %d micros, want 15", one)
	}
	if tiny := (cost.Rate{Price: cost.Price{InputMicrosPerMTok: 1}}).Cost(llm.Usage{InputTokens: 1}); tiny != 1 {
		t.Fatalf("fractions round up: %d", tiny)
	}
	// Inconsistent usage (cache larger than input) never goes negative.
	if neg := r.Cost(llm.Usage{InputTokens: 10, CacheReadTokens: 50}); neg < 0 {
		t.Fatalf("negative cost %d", neg)
	}
}

func TestLookupPrefersTheMostSpecificPrice(t *testing.T) {
	c := cost.Catalogue{Version: 3, Prices: []cost.Price{
		{Type: "openai", Model: "*", InputMicrosPerMTok: 1},
		{Type: "openai", Model: "gpt-x", InputMicrosPerMTok: 2},
		{Provider: "team", Model: "*", InputMicrosPerMTok: 3},
		{Provider: "team", Model: "gpt-x", InputMicrosPerMTok: 4},
	}}
	for _, tc := range []struct {
		provider, typ, model string
		want                 int64
		source               string
	}{
		{"team", "openai", "gpt-x", 4, "catalogue"},
		{"team", "openai", "gpt-y", 3, "catalogue"},
		{"other", "openai", "gpt-x", 2, "catalogue"},
		{"other", "openai", "gpt-y", 1, "catalogue"},
		{"local", "ollama", "llama3.2", 0, "local"},
		{"claude", "anthropic", "claude-x", 0, "unpriced"},
	} {
		r := c.Lookup(tc.provider, tc.typ, tc.model)
		if r.InputMicrosPerMTok != tc.want || r.Source != tc.source || r.Version != 3 {
			t.Errorf("%s/%s/%s = %+v", tc.provider, tc.typ, tc.model, r)
		}
	}
	if (cost.Rate{Source: "unpriced"}).Priced() || !(cost.Rate{Source: "local"}).Priced() {
		t.Fatal("Priced")
	}
	// An explicit price overrides the zero default for a local plugin.
	c.Prices = append(c.Prices, cost.Price{Type: "ollama", Model: "*", OutputMicrosPerMTok: 7})
	if r := c.Lookup("local", "ollama", "llama3.2"); r.Source != "catalogue" || r.OutputMicrosPerMTok != 7 {
		t.Fatalf("explicit local price = %+v", r)
	}
}

func TestPricesAreVersionedAndValidated(t *testing.T) {
	st := store(t)
	m := cost.Prices{Store: st}
	c, rev, err := m.Get(ctx)
	if err != nil || rev != 0 || c.Currency != "USD" || c.Unpriced != cost.UnpricedAllow || c.Version != 0 {
		t.Fatalf("default = %+v rev %d %v", c, rev, err)
	}
	_, err = m.Put(ctx, cost.CatalogueInput{Currency: "dollars", Unpriced: "maybe", Prices: []cost.Price{
		{Model: "x"},
		{Provider: "p", Type: "openai", Model: "x"},
		{Type: "nope", Model: "x"},
		{Type: "openai", Model: "", InputMicrosPerMTok: -1},
		{Type: "openai", Model: "*", OutputMicrosPerMTok: cost.MaxMicrosPerMTok + 1},
		{Type: "openai", Model: "*"},
	}}, "admin", 0)
	var ve *cost.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want validation error, got %v", err)
	}
	paths := map[string]bool{}
	for _, i := range ve.Errors {
		paths[i.Path] = true
	}
	for _, p := range []string{"currency", "unpriced", "prices[0]", "prices[1]", "prices[2].type", "prices[3].model", "prices[3].inputMicrosPerMTok", "prices[4].outputMicrosPerMTok", "prices[5]"} {
		if !paths[p] {
			t.Errorf("missing issue %s in %v", p, ve.Errors)
		}
	}
	if _, rev, _ := m.Get(ctx); rev != 0 {
		t.Fatal("an invalid catalogue was stored")
	}

	v1, err := m.Put(ctx, cost.CatalogueInput{Currency: "eur", Prices: []cost.Price{{Type: "OpenAI", Model: "*", InputMicrosPerMTok: 5}}}, "alice", 0)
	if err != nil || v1.Version != 1 || v1.Currency != "EUR" || v1.Unpriced != cost.UnpricedAllow || v1.Prices[0].Type != "openai" || v1.UpdatedBy != "alice" {
		t.Fatalf("v1 = %+v %v", v1, err)
	}
	_, rev1, _ := m.Get(ctx)
	if _, err := m.Put(ctx, cost.CatalogueInput{Currency: "EUR", Unpriced: cost.UnpricedRefuse}, "bob", rev1+5); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	v2, err := m.Put(ctx, cost.CatalogueInput{Currency: "EUR", Unpriced: cost.UnpricedRefuse}, "bob", rev1)
	if err != nil || v2.Version != 2 || len(v2.Prices) != 0 {
		t.Fatalf("v2 = %+v %v", v2, err)
	}
	// Earlier versions stay readable so recorded costs remain explainable.
	old, err := m.Version(ctx, 1)
	if err != nil || old.Prices[0].InputMicrosPerMTok != 5 || old.UpdatedBy != "alice" {
		t.Fatalf("version 1 = %+v %v", old, err)
	}
	if _, err := m.Version(ctx, 9); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
}

func TestCorruptCatalogueFailsLoudly(t *testing.T) {
	st := store(t)
	if _, err := st.PutSetting(ctx, cost.PricesKey, []byte("{"), 0, "x"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (cost.Prices{Store: st}).Get(ctx); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("err = %v", err)
	}
}
