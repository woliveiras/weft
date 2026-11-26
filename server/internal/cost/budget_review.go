package cost

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

// RaiseTarget identifies the current ledger and the recurring definition
// revision used to compute its effective limit. Run ledgers have no period.
type RaiseTarget struct {
	Scope               string   `json:"scope"`
	ID                  string   `json:"id"`
	Period              string   `json:"period,omitempty"`
	Currency            string   `json:"currency"`
	Limit               int64    `json:"limitMicros"`
	Revision            int64    `json:"revision"`
	DefinitionsRevision int64    `json:"definitionsRevision"`
	PausedGraphs        []string `json:"pausedGraphs,omitempty"`
}

// RaiseReceipt is saved atomically with the allowance and its resume actions.
type RaiseReceipt struct {
	Operation string        `json:"operation"`
	Revision  int64         `json:"revision"`
	Reached   bool          `json:"reached,omitempty"`
	Actions   []BudgetGraph `json:"actions,omitempty"`
}

func (b Budgets) ReviewRaise(ctx context.Context, scope, id string, now time.Time) (RaiseTarget, error) {
	period := Period(now)
	ledger, revision, err := b.read(ctx, scope, id)
	if err != nil {
		return RaiseTarget{}, err
	}
	if ledger == nil {
		return RaiseTarget{}, storage.ErrNotFound
	}
	target := RaiseTarget{Scope: scope, ID: id, Currency: ledger.Currency, Revision: revision, PausedGraphs: ledger.PausedGraphs}
	if scope != TaskRun {
		recurringID, ledgerPeriod, ok := strings.Cut(id, "@")
		if !ok || ledgerPeriod != period {
			return RaiseTarget{}, storage.ErrNotFound
		}
		definitions, rev, err := b.Definitions(ctx)
		if err != nil {
			return RaiseTarget{}, err
		}
		target.Period, target.DefinitionsRevision = period, rev
		found := false
		for _, d := range definitions {
			if d.Scope == scope && d.ID == recurringID {
				(applicable{scope: scope, limit: d.LimitMicros}).applyLimit(ledger)
				found = true
				break
			}
		}
		if !found {
			return RaiseTarget{}, storage.ErrNotFound
		}
	}
	target.Limit = ledger.Limit
	return target, nil
}

func (b Budgets) ReviewedRaiseReceipt(ctx context.Context, target RaiseTarget, limit int64, actor, operation string) (*RaiseReceipt, error) {
	ledger, _, err := b.read(ctx, target.Scope, target.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil || ledger == nil {
		return nil, err
	}
	receipt := ledger.ReviewedRaise
	if receipt == nil || receipt.Operation != operation || receipt.Revision != target.Revision+1 || ledger.RaisedBy != actor || ledger.RaisedLimit != limit {
		return nil, nil
	}
	return receipt, nil
}
