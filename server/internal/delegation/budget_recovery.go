package delegation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BudgetDetailRetention bounds per-call detail, not the durable closed-tree
// summary. Keeping that tombstone prevents delayed workers from reopening credit.
const BudgetDetailRetention = 30 * 24 * time.Hour

// ReconcileBudgets never infers death from age or a missing execution. The engine
// must first fence an abandoned execution and persist its terminal status. CAS
// then orders closure against dispatch/settlement by any other worker.
func (m Manager) ReconcileBudgets(ctx context.Context, now time.Time) error {
	rows, err := m.Store.ListSettings(ctx, BudgetPrefix)
	if err != nil {
		return fmt.Errorf("list tree budgets: %w", err)
	}
	var failures []error
	for _, row := range rows {
		root := strings.TrimPrefix(row.Key, BudgetPrefix)
		x, err := m.Store.GetExecution(ctx, root)
		if err != nil {
			failures = append(failures, fmt.Errorf("budget root %s: %w", root, err))
			continue
		}
		if !x.Status.Terminal() {
			continue
		}
		compact := x.CompletedAt != nil && !x.CompletedAt.IsZero() && !now.Before(x.CompletedAt.Add(BudgetDetailRetention))
		err = m.updateBudget(ctx, root, "reconcile", func(b *Budget) (*Budget, error) {
			if b == nil || b.Compacted != nil || (b.Closed && !compact) {
				return nil, nil
			}
			closeBudget(b)
			if compact {
				v := b.View()
				b.Compacted = &v
				b.Entries = map[string]Reservation{}
			}
			return b, nil
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("reconcile tree budget %s: %w", root, err))
		}
	}
	return errors.Join(failures...)
}

func closeBudget(b *Budget) {
	for id, r := range b.Entries {
		if r.Settled {
			continue
		}
		if r.Dispatched {
			r.Charged = r.Amount
			r.Estimated = true
		}
		r.Settled = true
		b.Entries[id] = r
	}
	b.Closed = true
}
