// Package cost turns model usage into money: a versioned price catalogue
// administrators maintain, cost records the LLM gateway writes for every
// call, and monetary budgets enforced before dispatch.
//
// Amounts are integer micro-units of the catalogue currency (1 USD =
// 1_000_000) so sums never drift; prices are micro-units per million tokens.
// Weft ships no vendor prices: operators enter the rates that apply to
// them, and local plugins (Ollama, vLLM, ...) cost nothing unless priced.
package cost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

// PricesKey holds the current catalogue; every version is also kept under
// PricesKey+"/versions/<n>" so cost records can always be explained.
const PricesKey = "cost/prices"

// MaxMicrosPerMTok bounds a price (10 000 currency units per million tokens).
const MaxMicrosPerMTok = 10_000 * 1_000_000

// Unpriced policies.
const (
	UnpricedAllow  = "allow"  // run and record the call as unpriced
	UnpricedRefuse = "refuse" // refuse it while a monetary budget applies
)

// Price is a rate for calls matching Provider (instance name) or Type
// (plugin id) and Model ("*" matches any model). Cache rates default to the
// input rate when omitted.
type Price struct {
	Provider                string `json:"provider,omitempty"`
	Type                    string `json:"type,omitempty"`
	Model                   string `json:"model"`
	InputMicrosPerMTok      int64  `json:"inputMicrosPerMTok"`
	OutputMicrosPerMTok     int64  `json:"outputMicrosPerMTok"`
	CacheReadMicrosPerMTok  *int64 `json:"cacheReadMicrosPerMTok,omitempty"`
	CacheWriteMicrosPerMTok *int64 `json:"cacheWriteMicrosPerMTok,omitempty"`
}

// Catalogue is one version of the price list.
type Catalogue struct {
	Version   int        `json:"version"`
	Currency  string     `json:"currency"`
	Unpriced  string     `json:"unpriced"`
	Prices    []Price    `json:"prices"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy string     `json:"updatedBy,omitempty"`
}

// CatalogueInput is what an administrator submits.
type CatalogueInput struct {
	Currency string  `json:"currency"`
	Unpriced string  `json:"unpriced"`
	Prices   []Price `json:"prices"`
}

// Issue is one field-level validation problem.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationError lists every problem with a catalogue.
type ValidationError struct {
	Errors []Issue `json:"errors"`
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, i := range e.Errors {
		parts = append(parts, i.Path+": "+i.Message)
	}
	return "invalid price catalogue: " + strings.Join(parts, "; ")
}

// Rate is the price that applies to one call.
type Rate struct {
	Price
	// Source says why: "catalogue", "local" (zero default for a local
	// plugin) or "unpriced".
	Source  string `json:"source"`
	Version int    `json:"version"`
}

// Priced reports whether the rate yields a cost.
func (r Rate) Priced() bool { return r.Source != "unpriced" }

// Cost is the cost of usage at this rate, rounded up to a whole micro-unit.
// InputTokens include the cached parts, which are charged at their own rates.
func (r Rate) Cost(u llm.Usage) int64 {
	cacheRead, cacheWrite := int64(u.CacheReadTokens), int64(u.CacheWriteTokens)
	input := max(int64(u.InputTokens)-cacheRead-cacheWrite, 0)
	readRate, writeRate := r.InputMicrosPerMTok, r.InputMicrosPerMTok
	if r.CacheReadMicrosPerMTok != nil {
		readRate = *r.CacheReadMicrosPerMTok
	}
	if r.CacheWriteMicrosPerMTok != nil {
		writeRate = *r.CacheWriteMicrosPerMTok
	}
	total := input*r.InputMicrosPerMTok + int64(u.OutputTokens)*r.OutputMicrosPerMTok + cacheRead*readRate + cacheWrite*writeRate
	return (total + 999_999) / 1_000_000
}

// Lookup finds the rate for a call: the most specific catalogue entry
// (provider and model, provider, type and model, type), else a zero price
// for local plugins, else unpriced.
func (c Catalogue) Lookup(provider, typ, model string) Rate {
	best, score := -1, 0
	for i, p := range c.Prices {
		s := 0
		switch {
		case p.Provider != "" && p.Provider == provider:
			s = 4
		case p.Provider == "" && p.Type != "" && strings.EqualFold(p.Type, typ):
			s = 2
		default:
			continue
		}
		switch p.Model {
		case model:
			s++
		case "*":
		default:
			continue
		}
		if s > score {
			best, score = i, s
		}
	}
	if best >= 0 {
		return Rate{Price: c.Prices[best], Source: "catalogue", Version: c.Version}
	}
	if m, ok := llm.PluginManifest(typ); ok && m.Local {
		return Rate{Price: Price{Type: typ, Model: "*"}, Source: "local", Version: c.Version}
	}
	return Rate{Price: Price{Provider: provider, Type: typ, Model: model}, Source: "unpriced", Version: c.Version}
}

// Default is the catalogue before an administrator saves one.
func Default() Catalogue {
	return Catalogue{Currency: "USD", Unpriced: UnpricedAllow, Prices: []Price{}}
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func validate(in *CatalogueInput) *ValidationError {
	ve := &ValidationError{}
	add := func(path, f string, a ...any) { ve.Errors = append(ve.Errors, Issue{path, fmt.Sprintf(f, a...)}) }
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if !currencyRe.MatchString(in.Currency) {
		add("currency", "use a three-letter ISO 4217 code, e.g. USD")
	}
	if in.Unpriced == "" {
		in.Unpriced = UnpricedAllow
	}
	if in.Unpriced != UnpricedAllow && in.Unpriced != UnpricedRefuse {
		add("unpriced", `must be "allow" or "refuse"`)
	}
	if len(in.Prices) > 500 {
		add("prices", "at most 500 prices")
	}
	seen := map[string]bool{}
	for i := range in.Prices {
		p := &in.Prices[i]
		path := fmt.Sprintf("prices[%d]", i)
		p.Provider, p.Type, p.Model = strings.TrimSpace(p.Provider), strings.ToLower(strings.TrimSpace(p.Type)), strings.TrimSpace(p.Model)
		if (p.Provider == "") == (p.Type == "") {
			add(path, "set either a provider name or a plugin type")
		}
		if p.Type != "" {
			if _, ok := llm.PluginManifest(p.Type); !ok {
				add(path+".type", "unknown inference plugin %q", p.Type)
			}
		}
		if p.Model == "" || len(p.Model) > 200 {
			add(path+".model", `name a model (1-200 characters) or "*" for any model`)
		}
		for name, v := range map[string]*int64{"inputMicrosPerMTok": &p.InputMicrosPerMTok, "outputMicrosPerMTok": &p.OutputMicrosPerMTok,
			"cacheReadMicrosPerMTok": p.CacheReadMicrosPerMTok, "cacheWriteMicrosPerMTok": p.CacheWriteMicrosPerMTok} {
			if v != nil && (*v < 0 || *v > MaxMicrosPerMTok) {
				add(path+"."+name, "must be between 0 and %d", MaxMicrosPerMTok)
			}
		}
		key := p.Provider + "\x00" + p.Type + "\x00" + p.Model
		if seen[key] {
			add(path, "duplicates an earlier price for the same provider/type and model")
		}
		seen[key] = true
	}
	if len(ve.Errors) > 0 {
		return ve
	}
	return nil
}

// Prices manages the catalogue.
type Prices struct {
	Store storage.Store
	Bus   *events.Bus // optional
}

// Get returns the current catalogue and its setting revision (0 when none
// was saved yet).
func (m Prices) Get(ctx context.Context) (Catalogue, int64, error) {
	st, err := m.Store.GetSetting(ctx, PricesKey)
	if errors.Is(err, storage.ErrNotFound) {
		return Default(), 0, nil
	}
	if err != nil {
		return Catalogue{}, 0, fmt.Errorf("cost: read prices: %w", err)
	}
	var c Catalogue
	if err := json.Unmarshal(st.Value, &c); err != nil || c.Version < 1 {
		return Catalogue{}, 0, errors.New("cost: the stored price catalogue is corrupt")
	}
	return c, st.Revision, nil
}

// Version returns a past catalogue version.
func (m Prices) Version(ctx context.Context, v int) (Catalogue, error) {
	st, err := m.Store.GetSetting(ctx, fmt.Sprintf("%s/versions/%d", PricesKey, v))
	if err != nil {
		return Catalogue{}, err
	}
	var c Catalogue
	if err := json.Unmarshal(st.Value, &c); err != nil {
		return Catalogue{}, errors.New("cost: a stored price version is corrupt")
	}
	return c, nil
}

// Put saves a new catalogue version. ifRevision > 0 requires the current
// setting revision (optimistic concurrency).
func (m Prices) Put(ctx context.Context, in CatalogueInput, actor string, ifRevision int64) (Catalogue, error) {
	if ve := validate(&in); ve != nil {
		return Catalogue{}, ve
	}
	cur, rev, err := m.Get(ctx)
	if err != nil {
		return Catalogue{}, err
	}
	if ifRevision > 0 && ifRevision != rev {
		return Catalogue{}, fmt.Errorf("%w: prices changed (revision %d, you edited %d); reload", storage.ErrConflict, rev, ifRevision)
	}
	now := time.Now().UTC()
	next := Catalogue{Version: cur.Version + 1, Currency: in.Currency, Unpriced: in.Unpriced, Prices: in.Prices, UpdatedAt: &now, UpdatedBy: actor}
	if next.Prices == nil {
		next.Prices = []Price{}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return Catalogue{}, err
	}
	// The version is written first: a version without a current pointer is
	// harmless, a pointer to a missing version is not.
	versionKey := fmt.Sprintf("%s/versions/%d", PricesKey, next.Version)
	_, err = m.Store.PutSetting(ctx, versionKey, raw, 0, actor)
	if errors.Is(err, storage.ErrConflict) {
		// An earlier attempt stored this version but not the pointer, so
		// nothing refers to it yet: replace it.
		if old, gerr := m.Store.GetSetting(ctx, versionKey); gerr == nil {
			_, err = m.Store.PutSetting(ctx, versionKey, raw, old.Revision, actor)
		}
	}
	if err != nil {
		return Catalogue{}, fmt.Errorf("cost: store price version: %w", err)
	}
	if _, err := m.Store.PutSetting(ctx, PricesKey, raw, rev, actor); err != nil {
		return Catalogue{}, fmt.Errorf("cost: store prices: %w", err)
	}
	if m.Bus != nil {
		m.Bus.Publish(ctx, domain.NewEvent(domain.EvPricesChanged, "", map[string]any{
			"version": next.Version, "currency": next.Currency, "prices": len(next.Prices), "unpriced": next.Unpriced,
		}).WithActor(actor))
	}
	return next, nil
}
