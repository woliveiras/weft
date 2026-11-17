package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const RetentionSettingsKey = "environment/blocked-retention"

type RetentionPolicy struct {
	Days      int `json:"days"`
	MaxEvents int `json:"maxEvents"`
}

func (p RetentionPolicy) Validate() error {
	if p.Days < 1 || p.Days > 90 {
		return fmt.Errorf("retention days must be between 1 and 90")
	}
	if p.MaxEvents < 1 || p.MaxEvents > 10000 {
		return fmt.Errorf("retained blocks must be between 1 and 10000")
	}
	return nil
}

type RetentionView struct {
	Policy    RetentionPolicy `json:"policy"`
	Revision  int64           `json:"revision"`
	UpdatedAt *time.Time      `json:"updatedAt,omitempty"`
	UpdatedBy string          `json:"updatedBy,omitempty"`
}

func (m *Manager) Retention(ctx context.Context) (RetentionView, error) {
	v := RetentionView{Policy: RetentionPolicy{Days: 7, MaxEvents: blockedHistoryLimit}}
	rec, err := m.opt.Store.GetSetting(ctx, RetentionSettingsKey)
	if errors.Is(err, storage.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Policy = RetentionPolicy{}
	if err := json.Unmarshal(rec.Value, &v.Policy); err != nil {
		return RetentionView{}, fmt.Errorf("invalid stored history retention")
	}
	if err := v.Policy.Validate(); err != nil {
		return RetentionView{}, fmt.Errorf("invalid stored history retention: %w", err)
	}
	v.Revision = rec.Revision
	v.UpdatedAt = &rec.UpdatedAt
	v.UpdatedBy = rec.UpdatedBy
	return v, nil
}
func (m *Manager) SaveRetention(ctx context.Context, p RetentionPolicy, revision int64, actor string) (RetentionView, error) {
	if revision < 0 {
		return RetentionView{}, fmt.Errorf("invalid revision")
	}
	if err := p.Validate(); err != nil {
		return RetentionView{}, err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return RetentionView{}, err
	}
	next, err := m.opt.Store.PutSetting(ctx, RetentionSettingsKey, data, revision, actor)
	if err != nil {
		return RetentionView{}, err
	}
	now := time.Now().UTC()
	return RetentionView{Policy: p, Revision: next, UpdatedAt: &now, UpdatedBy: actor}, nil
}
