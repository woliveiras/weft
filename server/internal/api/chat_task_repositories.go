package api

import (
	"context"
	"fmt"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/projectconfig"
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
