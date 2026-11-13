package fsstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

// ---------------------------------------------------------------------------
// installation settings
// ---------------------------------------------------------------------------

func (s *Store) settingPath(key string) string {
	return s.path("settings", encodeName(key)+".json")
}

func cloneSetting(st *storage.Setting) *storage.Setting {
	c := *st
	c.Value = append([]byte{}, st.Value...)
	return &c
}

func (s *Store) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	st, ok := s.settings[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return cloneSetting(st), nil
}

func (s *Store) PutSetting(ctx context.Context, key string, value []byte, expectedRevision int64, by string) (int64, error) {
	return s.PutSettingIf(ctx, key, value, expectedRevision, by, nil)
}

func (s *Store) PutSettingIf(ctx context.Context, key string, value []byte, expectedRevision int64, by string, guards []storage.SettingRevision) (int64, error) {
	if key == "" {
		return 0, fmt.Errorf("fsstore: setting key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return 0, err
	}
	if err := s.checkSettingRevisions(guards); err != nil {
		return 0, err
	}
	cur := s.settingRevision(key)
	if cur != expectedRevision {
		if expectedRevision == 0 {
			return 0, fmt.Errorf("%w: setting %q already exists", storage.ErrConflict, key)
		}
		return 0, fmt.Errorf("%w: setting %q is not at revision %d", storage.ErrConflict, key, expectedRevision)
	}
	st := &storage.Setting{Key: key, Value: append([]byte{}, value...), Revision: cur + 1, UpdatedAt: time.Now().UTC(), UpdatedBy: by}
	if err := writeJSON(s.settingPath(key), st); err != nil {
		return 0, err
	}
	s.settings[key] = st
	return st.Revision, nil
}

func (s *Store) DeleteSetting(ctx context.Context, key string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	st, ok := s.settings[key]
	if !ok {
		return storage.ErrNotFound
	}
	if st.Revision != expectedRevision {
		return fmt.Errorf("%w: setting %q is not at revision %d", storage.ErrConflict, key, expectedRevision)
	}
	if err := removeFile(s.settingPath(key)); err != nil {
		return err
	}
	delete(s.settings, key)
	return nil
}

func (s *Store) ListSettings(ctx context.Context, prefix string) ([]*storage.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	var out []*storage.Setting
	for k, st := range s.settings {
		if strings.HasPrefix(k, prefix) {
			out = append(out, cloneSetting(st))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// checkSettingRevisions runs with the store mutex held.
func (s *Store) checkSettingRevisions(guards []storage.SettingRevision) error {
	for _, guard := range guards {
		if s.settingRevision(guard.Key) != guard.Revision {
			return fmt.Errorf("%w: setting %q changed", storage.ErrConflict, guard.Key)
		}
	}
	return nil
}

func (s *Store) settingRevision(key string) int64 {
	if setting := s.settings[key]; setting != nil {
		return setting.Revision
	}
	return 0
}
