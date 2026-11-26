package chat

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/storage"
)

const RaiseBudget = "budget.raise"

var budgetAmount = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,6})?$`)
var budgetCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

type BudgetRaiseDraft struct{ Scope, Target, Amount, Currency string }

func (d BudgetRaiseDraft) Valid() bool {
	if !budgetCurrency.MatchString(d.Currency) || d.LimitMicros() == 0 {
		return false
	}
	switch d.Scope {
	case cost.Installation:
		return d.Target == ""
	case cost.TaskRun:
		return shortLabel(d.Target)
	case cost.User, cost.Project, cost.Workflow:
		return shortLabel(d.Target) && !strings.ContainsAny(d.Target, "@/*")
	}
	return false
}

// LimitMicros converts the displayed decimal without rounding.
func (d BudgetRaiseDraft) LimitMicros() int64 {
	if !budgetAmount.MatchString(d.Amount) {
		return 0
	}
	whole, fractional, _ := strings.Cut(d.Amount, ".")
	value, err := strconv.ParseInt(whole+fractional+strings.Repeat("0", 6-len(fractional)), 10, 64)
	if err != nil || value > cost.MaxBudgetMicros {
		return 0
	}
	return value
}

type BudgetRaise struct {
	Target  cost.RaiseTarget `json:"target"`
	Subject string           `json:"subject"`
	Actor   string           `json:"actor"`
	Limit   int64            `json:"limitMicros"`
}

type BudgetResume struct {
	Graph string `json:"graphId"`
	Run   string `json:"runId"`
	State string `json:"state"`
}

type BudgetRaiseResult struct {
	Revision int64          `json:"revision"`
	Reached  bool           `json:"reached"`
	Resumes  []BudgetResume `json:"resumes,omitempty"`
}

func NewBudgetRaiseReview(owner, session string, raise BudgetRaise, now time.Time) (*OperationReview, error) {
	target := raise.Target
	if !shortLabel(target.ID) || !shortLabel(raise.Subject) || !shortLabel(raise.Actor) || !budgetCurrency.MatchString(target.Currency) ||
		target.Revision < 1 || raise.Limit <= target.Limit || raise.Limit > cost.MaxBudgetMicros || len(target.PausedGraphs) > 16 {
		return nil, ErrInvalidPlan
	}
	for _, graph := range target.PausedGraphs {
		if !shortLabel(graph) {
			return nil, ErrInvalidPlan
		}
	}
	return newReview(owner, session, now, OperationReview{BudgetRaise: &raise})
}

func (s Sessions) RecordBudgetRaise(ctx context.Context, owner, sessionID, id, state string, result BudgetRaiseResult) (*Session, error) {
	return s.update(ctx, owner, sessionID, func(session *Session) (bool, error) {
		for i := range session.Messages {
			message := &session.Messages[i]
			review := message.Review
			if review == nil || review.BudgetRaise == nil || review.ID != id {
				continue
			}
			if review.State != ReviewApplying && review.State != ReviewAccepted && review.State != ReviewUnknown {
				return false, nil
			}
			if old := review.BudgetResult; old != nil && review.State == state && old.Revision == result.Revision && old.Reached == result.Reached && slices.Equal(old.Resumes, result.Resumes) {
				return false, nil
			}
			review.BudgetResult, review.State = &result, state
			if state != ReviewAccepted {
				message.Unread = true
				session.LastActivity = time.Now().UTC()
			}
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}
