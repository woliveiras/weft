package taskauth

import (
	"context"

	"github.com/woliveiras/weft/server/internal/storage"
)

// StartConsent describes the existing authorization or the canonical Ask
// default the operator reviewed before starting a task.
type StartConsent struct {
	Scope    string `json:"scope"`
	Mode     Mode   `json:"mode"`
	Revision int64  `json:"revision"`
}

func (r *Review) StartConsent() StartConsent {
	if r.Record != nil {
		return StartConsent{Scope: r.Scope, Mode: r.Record.Mode, Revision: r.Record.Revision}
	}
	return StartConsent{Scope: r.Scope, Mode: Ask}
}

// GuardStart revalidates the displayed consent and returns the exact
// authorization revision that must still hold at the admission write.
func (m Manager) GuardStart(ctx context.Context, id, actor string, draftRevision int64, expected StartConsent) (storage.SettingRevision, error) {
	review, err := m.Review(ctx, id)
	if err != nil {
		return storage.SettingRevision{}, err
	}
	if review.StartConsent() != expected || (review.Record != nil && review.Record.Scope != expected.Scope) {
		return storage.SettingRevision{}, storage.ErrConflict
	}
	record := review.Record
	if record == nil {
		record, err = m.save(ctx, id, Ask, review.Scope, 0, actor, true, draftRevision, "")
		if err != nil {
			return storage.SettingRevision{}, err
		}
	}
	return storage.SettingRevision{Key: prefix + id, Revision: record.Revision}, nil
}
