package taskconfig

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// ReplacedRepositoryInput changes a single existing checkout while retaining
// its position and workspace alias. The complete list must still be valid.
func (d Draft) ReplacedRepositoryInput(before, after domain.TaskRepository) (Input, error) {
	if !d.editableDraftInput() || before.Alias != after.Alias || before.ProjectID == after.ProjectID {
		return Input{}, fmt.Errorf("%w: expected a different project for the same editable checkout", ErrInvalid)
	}
	in := d.Input
	for i, r := range in.Repositories {
		if r.Alias != before.Alias {
			continue
		}
		if !reflect.DeepEqual(r, before) {
			return Input{}, storage.ErrConflict
		}
		in.Repositories = slices.Clone(in.Repositories)
		in.Repositories[i] = after
		return in, Validate(in)
	}
	return Input{}, fmt.Errorf("%w: checkout is absent from the draft", ErrInvalid)
}

// ReplaceRepositoryForOperation stores the reviewed change and its receipt in
// the same CAS. Project and connection guards fence separately versioned data.
func (m Manager) ReplaceRepositoryForOperation(ctx context.Context, id string, revision int64, before, after domain.TaskRepository, actor, operation string, guards ...storage.SettingRevision) (*Draft, error) {
	if operation == "" || revision < 1 {
		return nil, ErrInvalid
	}
	d, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.Revision != revision {
		return nil, storage.ErrConflict
	}
	in, err := d.ReplacedRepositoryInput(before, after)
	if err != nil {
		return nil, err
	}
	if err = m.validateDraftBinding(ctx, in); err != nil {
		return nil, err
	}
	d.Input = in
	d.UpdatedAt, d.UpdatedBy = time.Now().UTC(), actor
	d.RepositoryRevision = revision + 1
	d.CorrectionOperation, d.CorrectionRevision = operation, revision+1
	return m.persistDraft(ctx, d, revision, actor, guards...)
}
