package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

const BudgetPrefix = "delegation/budget/"
const DefaultTreeTokens = 65536
const MaxTreeTokens = 10000000

var ErrBudget = errors.New("execution tree token budget exhausted")

type Reservation struct {
	Amount     int  `json:"amount"`
	Dispatched bool `json:"dispatched"`
	Settled    bool `json:"settled"`
	Charged    int  `json:"charged"`
	Estimated  bool `json:"estimated"`
}
type Budget struct {
	Closed    bool                   `json:"closed,omitempty"`
	Compacted *BudgetView            `json:"compacted,omitempty"`
	Limit     int                    `json:"limit"`
	Entries   map[string]Reservation `json:"entries"`
}
type BudgetView struct {
	Limit     int `json:"limit"`
	Charged   int `json:"charged"`
	Reserved  int `json:"reserved"`
	Estimated int `json:"estimated"`
}

func (b Budget) View() BudgetView {
	if b.Compacted != nil {
		return *b.Compacted
	}
	v := BudgetView{Limit: b.Limit}
	for _, r := range b.Entries {
		if r.Settled {
			v.Charged += r.Charged
			if r.Estimated {
				v.Estimated += r.Charged
			}
		} else {
			v.Reserved += r.Amount
		}
	}
	return v
}
func (m Manager) readBudget(ctx context.Context, root string) (*Budget, int64, error) {
	row, e := m.Store.GetSetting(ctx, BudgetPrefix+root)
	if errors.Is(e, storage.ErrNotFound) {
		return nil, 0, nil
	}
	if e != nil {
		return nil, 0, e
	}
	var b Budget
	if json.Unmarshal(row.Value, &b) != nil || b.Limit < 1 || b.Limit > MaxTreeTokens || b.Entries == nil || len(b.Entries) > 1024 {
		return nil, 0, errors.New("invalid stored tree budget")
	}
	if b.Compacted != nil && (!b.Closed || len(b.Entries) != 0 || b.Compacted.Limit != b.Limit || b.Compacted.Reserved != 0 || b.Compacted.Charged < 0 || b.Compacted.Estimated < 0 || b.Compacted.Estimated > b.Compacted.Charged) {
		return nil, 0, errors.New("invalid compacted tree budget")
	}
	for id, r := range b.Entries {
		if (b.Closed && !r.Settled) || id == "" || r.Amount < 1 || r.Amount > MaxTreeTokens || r.Charged < 0 || r.Charged > MaxTreeTokens*100 || (!r.Settled && r.Charged != 0) {
			return nil, 0, errors.New("invalid stored token reservation")
		}
	}
	return &b, row.Revision, nil
}
func (m Manager) Budget(ctx context.Context, root string) (*BudgetView, error) {
	b, _, e := m.readBudget(ctx, root)
	if e != nil || b == nil {
		return nil, e
	}
	v := b.View()
	return &v, nil
}

// CAS keeps admission and settlement atomic across instances. Entries are kept
// for idempotency; uncertain dispatched work is never silently refunded.
func (m Manager) updateBudget(ctx context.Context, root, operation string, change func(*Budget) (*Budget, error)) (retErr error) {
	started := time.Now()
	ctx, span := observability.Start(ctx, "delegation.token_budget."+operation)
	defer func() {
		outcome := "ok"
		if retErr != nil {
			outcome = "error"
			if errors.Is(retErr, ErrBudget) {
				outcome = "exhausted"
			}
		}
		observability.TreeBudgetOperations.WithLabelValues(operation, outcome).Inc()
		observability.TreeBudgetSeconds.WithLabelValues(operation).Observe(time.Since(started).Seconds())
		observability.EndSpan(span, retErr)
	}()
	if root == "" {
		return errors.New("missing budget root")
	}
	for i := 0; i < 100; i++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		b, rev, e := m.readBudget(ctx, root)
		if e != nil {
			return e
		}
		b, e = change(b)
		if e != nil {
			return e
		}
		if b == nil {
			// Reconciliation made no change.
			return nil
		}
		raw, e := json.Marshal(b)
		if e != nil {
			return e
		}
		_, e = m.Store.PutSetting(ctx, BudgetPrefix+root, raw, rev, "system:token-budget")
		if errors.Is(e, storage.ErrConflict) {
			continue
		}
		return e
	}
	return fmt.Errorf("%w: token budget contention", storage.ErrConflict)
}
func (m Manager) ReserveTokens(ctx context.Context, root, id string, limit, amount int) error {
	if id == "" || limit < 1 || limit > MaxTreeTokens || amount < 1 || amount > MaxTreeTokens {
		return errors.New("invalid token reservation")
	}
	return m.updateBudget(ctx, root, "reserve", func(b *Budget) (*Budget, error) {
		if b == nil {
			b = &Budget{Limit: limit, Entries: map[string]Reservation{}}
		}
		if b.Closed {
			return nil, storage.ErrConflict
		}
		if _, exists := b.Entries[id]; exists {
			return nil, storage.ErrConflict
		}
		b.Limit = min(b.Limit, limit) // Raising policy never replenishes an existing tree.
		v := b.View()
		if len(b.Entries) >= 1024 || amount > b.Limit-v.Charged-v.Reserved {
			return nil, ErrBudget
		}
		b.Entries[id] = Reservation{Amount: amount}
		return b, nil
	})
}
func (m Manager) DispatchTokens(ctx context.Context, root, id string) error {
	return m.updateBudget(ctx, root, "dispatch", func(b *Budget) (*Budget, error) {
		if b == nil {
			return nil, storage.ErrNotFound
		}
		r, ok := b.Entries[id]
		if !ok {
			return nil, storage.ErrNotFound
		}
		if b.Closed || r.Settled || r.Dispatched {
			return nil, storage.ErrConflict
		}
		r.Dispatched = true
		b.Entries[id] = r
		return b, nil
	})
}

// Unknown post-dispatch usage consumes the reservation conservatively, while
// approval rejection/pre-dispatch failures release it without a charge.
func (m Manager) SettleTokens(ctx context.Context, root, id string, reported int, known bool) error {
	if reported < 0 || reported > MaxTreeTokens*100 {
		return errors.New("invalid reported token usage")
	}
	return m.updateBudget(ctx, root, "settle", func(b *Budget) (*Budget, error) {
		if b == nil {
			return nil, storage.ErrNotFound
		}
		r, ok := b.Entries[id]
		if !ok {
			return nil, storage.ErrNotFound
		}
		if r.Settled {
			return b, nil
		}
		if r.Dispatched {
			if known {
				r.Charged = reported
			} else {
				r.Charged = r.Amount
				r.Estimated = true
			}
		}
		r.Settled = true
		b.Entries[id] = r
		return b, nil
	})
}
