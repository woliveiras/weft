package cost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// DefinitionsKey holds the monthly budgets administrators define.
const DefinitionsKey = "cost/definitions"

// Definition is a monthly budget for a scope: the whole installation
// (id "*"), a user (identity name), a project (project id) or a workflow
// (source graph id). Its ledger restarts every calendar month (UTC).
type Definition struct {
	Scope       string `json:"scope"`
	ID          string `json:"id"`
	LimitMicros int64  `json:"limitMicros"`
	// WarningPercent defaults to the policy's warning level.
	WarningPercent int `json:"warningPercent,omitempty"`
	// InterruptPercent (101-1000) cancels the running call that takes spend
	// past it and pauses the run; 0 disables the interrupt level.
	InterruptPercent int `json:"interruptPercent,omitempty"`
}

type definitions struct {
	Budgets   []Definition `json:"budgets"`
	UpdatedAt *time.Time   `json:"updatedAt,omitempty"`
	UpdatedBy string       `json:"updatedBy,omitempty"`
}

// Definitions returns the monthly budgets and the setting revision.
func (b Budgets) Definitions(ctx context.Context) ([]Definition, int64, error) {
	st, err := b.Store.GetSetting(ctx, DefinitionsKey)
	if errors.Is(err, storage.ErrNotFound) {
		return []Definition{}, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("cost: read budgets: %w", err)
	}
	var d definitions
	if err := json.Unmarshal(st.Value, &d); err != nil || validateDefinitions(d.Budgets) != nil {
		return nil, 0, errors.New("cost: the stored budget definitions are corrupt")
	}
	return d.Budgets, st.Revision, nil
}

func validateDefinitions(defs []Definition) *ValidationError {
	ve := &ValidationError{}
	add := func(path, f string, a ...any) { ve.Errors = append(ve.Errors, Issue{path, fmt.Sprintf(f, a...)}) }
	if len(defs) > 500 {
		add("budgets", "at most 500 budgets")
	}
	seen := map[string]bool{}
	for i := range defs {
		d := &defs[i]
		path := fmt.Sprintf("budgets[%d]", i)
		d.ID = strings.TrimSpace(d.ID)
		switch d.Scope {
		case Installation:
			if d.ID == "" {
				d.ID = "*"
			}
			if d.ID != "*" {
				add(path+".id", `the installation budget has id "*"`)
			}
		case User, Project, Workflow:
			if d.ID == "" || len(d.ID) > 200 || strings.ContainsAny(d.ID, "@/*") {
				add(path+".id", "name the %s (1-200 characters, without @, / or *)", d.Scope)
			}
		default:
			add(path+".scope", "must be installation, user, project or workflow")
		}
		if d.LimitMicros < 1 || d.LimitMicros > MaxBudgetMicros {
			add(path+".limitMicros", "must be between 1 and %d", MaxBudgetMicros)
		}
		if d.WarningPercent < 0 || d.WarningPercent > 100 {
			add(path+".warningPercent", "must be between 1 and 100, or 0 for the policy default")
		}
		if d.InterruptPercent != 0 && (d.InterruptPercent <= 100 || d.InterruptPercent > 1000) {
			add(path+".interruptPercent", "must be between 101 and 1000, or 0 for none")
		}
		key := d.Scope + "/" + d.ID
		if seen[key] {
			add(path, "duplicates an earlier %s budget for %q", d.Scope, d.ID)
		}
		seen[key] = true
	}
	if len(ve.Errors) > 0 {
		return ve
	}
	return nil
}

// SetDefinitions replaces the monthly budgets. A changed limit applies to
// the current month's ledger during reconciliation or the next reservation
// unless a person raised that ledger above it.
func (b Budgets) SetDefinitions(ctx context.Context, defs []Definition, actor string, ifRevision int64) ([]Definition, error) {
	if defs == nil {
		defs = []Definition{}
	}
	if ve := validateDefinitions(defs); ve != nil {
		return nil, ve
	}
	_, rev, err := b.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	if ifRevision > 0 && ifRevision != rev {
		return nil, fmt.Errorf("%w: budgets changed (revision %d, you edited %d); reload", storage.ErrConflict, rev, ifRevision)
	}
	now := time.Now().UTC()
	raw, _ := json.Marshal(definitions{Budgets: defs, UpdatedAt: &now, UpdatedBy: actor})
	if _, err := b.Store.PutSetting(ctx, DefinitionsKey, raw, rev, actor); err != nil {
		return nil, fmt.Errorf("cost: store budgets: %w", err)
	}
	b.publish(ctx, domain.NewEvent(domain.EvBudgetChanged, "", map[string]any{"scope": "definitions", "budgets": len(defs)}).WithActor(actor))
	return defs, nil
}

// CheckAdmission returns the reached ledger as an error before a new task.
func (b Budgets) CheckAdmission(ctx context.Context, t Target) error {
	reached, err := b.Check(ctx, t)
	if err != nil {
		return err
	}
	if reached != nil {
		return reached
	}
	return nil
}

// Check reports the first budget that is reached for a target, so new task
// runs can be refused before they start. It returns nil when none is.
func (b Budgets) Check(ctx context.Context, t Target) (*ReachedError, error) {
	apply, err := b.applicable(ctx, t, time.Now())
	if err != nil {
		return nil, err
	}
	for _, a := range apply {
		l, _, err := b.read(ctx, a.scope, a.id)
		if err != nil {
			return nil, err
		}
		if l == nil {
			continue
		}
		a.applyLimit(l)
		if v := l.View(); l.Reached || (v.Limit > 0 && v.Remaining == 0) {
			v.Reached = true
			return &ReachedError{Scope: a.scope, ID: a.id, View: v}, nil
		}
	}
	return nil, nil
}
