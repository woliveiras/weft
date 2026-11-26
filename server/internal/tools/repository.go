package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// gitRepository chooses the checkout on each call, independent of previous
// commands. The sandbox remains mounted at the common task workspace.
func gitRepository(inv *Invocation) (string, error) {
	if inv.Principal == nil || len(inv.Principal.Repositories) == 0 {
		return "", nil
	}
	var args struct {
		Repository string `json:"repository"`
	}
	if err := decodeArgs(inv, &args); err != nil {
		return "", err
	}
	for _, r := range inv.Principal.Repositories {
		if r.Alias == args.Repository {
			_, rel, err := ResolvePath(workdirOf(inv), r.Alias)
			if err != nil || rel != r.Alias {
				return "", fmt.Errorf("%w: checkout directory changed", ErrDenied)
			}
			return r.Alias, nil
		}
	}
	return "", fmt.Errorf("%w: select a reviewed repository by its task alias", ErrDenied)
}

func repositorySchema(schema json.RawMessage, p *Principal) json.RawMessage {
	if p == nil || len(p.Repositories) == 0 {
		return schema
	}
	var params struct {
		Type                 string         `json:"type"`
		Properties           map[string]any `json:"properties"`
		Required             []string       `json:"required"`
		AdditionalProperties bool           `json:"additionalProperties"`
	}
	if json.Unmarshal(schema, &params) != nil || params.Properties == nil {
		return schema
	}
	var aliases []string
	for _, r := range p.Repositories {
		aliases = append(aliases, r.Alias)
	}
	params.Properties["repository"] = map[string]any{"type": "string", "enum": aliases}
	params.Required = append(params.Required, "repository")
	b, _ := json.Marshal(params)
	return b
}

func checkRepositoryStates(ctx context.Context, inv *Invocation) map[string]CheckWorkspace {
	if inv.Principal == nil || len(inv.Principal.Repositories) == 0 {
		return nil
	}
	states := map[string]CheckWorkspace{}
	for _, r := range inv.Principal.Repositories {
		state := CheckWorkspace{Partial: true}
		err := workspace.WithRepositorySnapshot(ctx, workdirOf(inv), r, func(c workspace.RepositoryState, _ string) error {
			state = CheckWorkspace{Head: c.Head, Base: c.Base, State: c.State}
			return nil
		})
		if err != nil {
			state.Partial = true
		}
		states[r.Alias] = state
	}
	return states
}

func (r CheckRun) Current(states map[string]CheckWorkspace) bool {
	if !r.Passed || r.Stale || len(states) == 0 || len(states) != len(r.Repositories) {
		return false
	}
	for alias, current := range states {
		prev, ok := r.Repositories[alias]
		if !ok || prev.Partial || current.Partial || prev.Head == "" || prev.State == "" || current.Head != prev.Head || current.State != prev.State {
			return false
		}
	}
	return true
}

func scopedGitPath(inv *Invocation, p string) (string, error) {
	dir, err := gitRepository(inv)
	if err != nil {
		return "", err
	}
	_, rel, err := ResolvePath(filepath.Join(workdirOf(inv), dir), p)
	if err != nil {
		return "", err
	}
	if err := CheckPath(inv.Grant, filepath.ToSlash(filepath.Join(dir, rel))); err != nil {
		return "", err
	}
	return rel, nil
}

func RequiredRepositoryChecks(repos []domain.TaskRepository) []string {
	var names []string
	for _, r := range repos {
		for _, c := range r.RequiredChecks {
			names = append(names, r.Alias+"."+c)
		}
	}
	return names
}
