package trigger

import (
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

// reserveGitHubSearch counts attempted searches across concurrent pollers and
// replicas. Keep one bounded rolling window for the installation, since
// different configured connections can share the same GitHub search quota.
func reserveGitHubSearch(ctx context.Context, st storage.Store, now time.Time) error {
	cutoff := now.Add(-time.Minute)
	for range 10 {
		var window []time.Time
		var revision int64
		row, err := st.GetSetting(ctx, githubSearchBudgetKey)
		if err == nil {
			revision = row.Revision
			if err := json.Unmarshal(row.Value, &window); err != nil {
				return errors.New("GitHub search budget could not be read")
			}
		} else if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		active := window[:0]
		for _, at := range window {
			if at.After(cutoff) {
				active = append(active, at)
			}
		}
		if len(active) >= githubSearchesPerMinute {
			return errSearchBudgetDeferred
		}
		value, err := json.Marshal(append(active, now))
		if err != nil {
			return err
		}
		if _, err := st.PutSetting(ctx, githubSearchBudgetKey, value, revision, "system:trigger"); !errors.Is(err, storage.ErrConflict) {
			return err
		}
	}
	return errSearchBudgetDeferred
}

func (r *Runner) boundGitHubSearch(t *Trigger, call Invoker) Invoker {
	search := tool(t.Source, "search_issues")
	return func(ctx context.Context, name string, args any) (*tools.Result, error) {
		if name == search {
			if err := reserveGitHubSearch(ctx, r.Store, r.Triggers.now()); err != nil {
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
