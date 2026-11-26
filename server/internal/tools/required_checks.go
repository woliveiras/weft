package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// RepositoryCheckBlockers verifies the newest evidence for every required check
// against the same candidate used by the delivery gate and task settlement.
func RepositoryCheckBlockers(ctx context.Context, store storage.Store, graph string, repos []domain.TaskRepository, states map[string]workspace.RepositoryState) ([]string, error) {
	required := RequiredRepositoryChecks(repos)
	evidence, err := RepositoryCheckEvidence(ctx, store, graph, required, states)
	if err != nil {
		return nil, err
	}
	var blockers []string
	for _, name := range required {
		if outcome := evidence[name]; outcome != "passed" {
			blockers = append(blockers, fmt.Sprintf("%s: %s", name, outcome))
		}
	}
	return blockers, nil
}

// RepositoryCheckEvidence reports the latest outcome for each named check
// evaluated against all participants of a specific workspace combination.
func RepositoryCheckEvidence(ctx context.Context, store storage.Store, graph string, names []string, states map[string]workspace.RepositoryState) (map[string]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	pending := map[string]bool{}
	for _, name := range names {
		pending[name] = true
	}
	latest := map[string]CheckRun{}
	var cursor int64
	for len(pending) > 0 {
		rows, err := store.ListEvents(ctx, storage.EventFilter{GraphID: graph, Types: []domain.EventType{domain.EvCheckCompleted}, BeforeSeq: cursor, Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, ev := range rows {
			var run CheckRun
			if err := json.Unmarshal(ev.Data, &run); err != nil {
				return nil, err
			}
			if pending[run.Check] {
				latest[run.Check] = run
				delete(pending, run.Check)
			}
			cursor = ev.Seq
		}
		if len(rows) < 200 {
			break
		}
	}
	current := map[string]CheckWorkspace{}
	for alias, s := range states {
		current[alias] = CheckWorkspace{Head: s.Head, State: s.State}
	}
	outcomes := map[string]string{}
	for _, name := range names {
		r, ok := latest[name]
		reason := "passed"
		switch {
		case !ok:
			reason = "not run"
		case r.InfraError:
			reason = "could not run"
		case !r.Passed:
			reason = "failed"
		case !r.Current(current):
			reason = "stale"
		}
		outcomes[name] = reason
	}
	return outcomes, nil
}
