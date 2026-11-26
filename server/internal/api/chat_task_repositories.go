package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

// Keep a dependency revision for every selected checkout. A project edit must
// invalidate the review even when the task draft itself has not changed.
func (s *Server) chatTaskRepositories(ctx context.Context, d *taskconfig.Draft) ([]chat.StartRepository, error) {
	refs := d.Repositories
	if d.ProjectID != "" {
		refs = []domain.TaskRepository{{ProjectID: d.ProjectID, BaseCommit: d.BaseCommit, SCMConnection: d.SCMConnection}}
	}
	var result []chat.StartRepository
	for _, ref := range refs {
		if ref.SCMConnection != "" {
			return nil, fmt.Errorf("%w: review external delivery in Tasks", taskconfig.ErrInvalid)
		}
		p, err := (projectconfig.Manager{Store: s.Store}).Get(ctx, ref.ProjectID)
		if err != nil {
			return nil, err
		}
		if p.Revision < 1 || p.ID != ref.ProjectID {
			return nil, taskconfig.ErrInvalid
		}
		binding := chat.StartRepository{Project: p.ID, Alias: ref.Alias, Commit: ref.BaseCommit, Revision: p.Revision}
		for _, check := range p.Checks {
			binding.Checks = append(binding.Checks, check.Name)
		}
		result = append(result, binding)
	}
	return result, nil
}

func (s *Server) bindDraftRepositories(ctx context.Context, selectors []chat.DraftRepositorySelector) ([]domain.TaskRepository, []chat.StartRepository, error) {
	if len(selectors) == 0 {
		return nil, nil, nil
	}
	projects, err := (projectconfig.Manager{Store: s.Store}).List(ctx)
	if err != nil {
		return nil, nil, err
	}
	var refs []domain.TaskRepository
	var bindings []chat.StartRepository
	for _, selector := range selectors {
		var selected *projectconfig.Project
		matches := 0
		for _, p := range projects {
			if p.ID == selector.Project {
				selected = p
				matches = 1
				break
			}
			if strings.EqualFold(p.Name, selector.Project) {
				selected = p
				matches++
			}
		}
		if matches != 1 {
			return nil, nil, storage.ErrNotFound
		}
		if selector.Commit != "" && selector.Commit != selected.Head {
			return nil, nil, taskconfig.ErrInvalid
		}
		refs = append(refs, domain.TaskRepository{Alias: selector.Alias, ProjectID: selected.ID, BaseCommit: selected.Head})
		bindings = append(bindings, chat.StartRepository{Alias: selector.Alias, Project: selected.ID, Commit: selected.Head, Revision: selected.Revision})
	}
	if err := domain.ValidateTaskRepositories(refs); err != nil {
		return nil, nil, taskconfig.ErrInvalid
	}
	return refs, bindings, nil
}
