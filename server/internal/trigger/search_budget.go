package trigger

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

const (
	githubSearchesPerMinute   = 30
	githubHeldSearchesPerPoll = 8
	githubSearchBudgetKey     = "trigger-search-budget/github"
)

var errSearchBudgetDeferred = errors.New("GitHub search deferred until the polling budget is available")

const githubSearchDeferred = "GitHub search was deferred because the shared polling and preview budget was reached."

type githubSearchWindow struct {
	Calls []time.Time `json:"calls"`
	Turn  uint64      `json:"turn"`
}

type searchBudgetDeferral struct {
	reopensAt time.Time
}

func (e searchBudgetDeferral) Error() string { return errSearchBudgetDeferred.Error() }
func (e searchBudgetDeferral) Unwrap() error { return errSearchBudgetDeferred }

type githubSearchPoll struct {
	attempted bool
	reopensAt time.Time
	turn      uint64
}

func compareSearchTurn(a, b *Trigger) int {
	if deferredA, deferredB := a.SearchDeferredReason != "", b.SearchDeferredReason != ""; deferredA != deferredB {
		if deferredA {
			return -1
		}
		return 1
	}
	return cmp.Compare(a.SearchTurn, b.SearchTurn)
}

// reserveGitHubSearch counts attempted searches across concurrent pollers,
// replicas and previews. Different connections can share a GitHub quota.
func reserveGitHubSearch(ctx context.Context, st storage.Store, now time.Time) (uint64, error) {
	cutoff := now.Add(-time.Minute)
	for range 10 {
		var window githubSearchWindow
		var revision int64
		row, err := st.GetSetting(ctx, githubSearchBudgetKey)
		if err == nil {
			revision = row.Revision
			if err := json.Unmarshal(row.Value, &window); err != nil {
				// Earlier installations stored only the rolling timestamps.
				if err := json.Unmarshal(row.Value, &window.Calls); err != nil {
					return 0, errors.New("GitHub search budget could not be read")
				}
			}
		} else if !errors.Is(err, storage.ErrNotFound) {
			return 0, err
		}
		active := window.Calls[:0]
		for _, at := range window.Calls {
			if at.After(cutoff) {
				active = append(active, at)
			}
		}
		if len(active) >= githubSearchesPerMinute {
			oldest := active[0]
			for _, at := range active[1:] {
				if at.Before(oldest) {
					oldest = at
				}
			}
			return 0, searchBudgetDeferral{reopensAt: oldest.Add(time.Minute)}
		}
		window.Calls = append(active, now)
		window.Turn++
		value, err := json.Marshal(window)
		if err != nil {
			return 0, err
		}
		if _, err := st.PutSetting(ctx, githubSearchBudgetKey, value, revision, "system:trigger"); !errors.Is(err, storage.ErrConflict) {
			return window.Turn, err
		}
	}
	// A concurrent reservation kept winning the compare-and-set. The
	// next runner tick can reload the window rather than wait a full interval.
	return 0, searchBudgetDeferral{reopensAt: now}
}

func (r *Runner) boundGitHubSearch(t *Trigger, call Invoker, poll *githubSearchPoll) Invoker {
	search := tool(t.Source, "search_issues")
	return func(ctx context.Context, name string, args any) (*tools.Result, error) {
		if name == search {
			turn, err := reserveGitHubSearch(ctx, r.Store, r.Triggers.now())
			if poll != nil {
				poll.attempted = true
				var deferred searchBudgetDeferral
				if errors.As(err, &deferred) {
					poll.reopensAt = deferred.reopensAt
				}
				if err == nil {
					poll.turn = turn
				}
			}
			if err != nil {
				return nil, err
			}
		}
		return call(ctx, name, args)
	}
}

func boundHeldGitHubSearch(t *Trigger, call Invoker) Invoker {
	search := tool(t.Source, "search_issues")
	calls := 0
	return func(ctx context.Context, name string, args any) (*tools.Result, error) {
		if name == search {
			if calls >= githubHeldSearchesPerPoll {
				return nil, errSearchBudgetDeferred
			}
			calls++
		}
		return call(ctx, name, args)
	}
}
