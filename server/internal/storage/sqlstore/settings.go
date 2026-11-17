package sqlstore

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/storage"
)

// ---------------------------------------------------------------------------
// installation settings
// ---------------------------------------------------------------------------

// Settings writes are guarded single statements: an insert that collides on
// the primary key when the setting already exists, or an update whose WHERE
// clause pins the expected revision. Either way the database decides
// atomically, so of two racing writers exactly one changes a row.

func (s *Store) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	st := &storage.Setting{Key: key}
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT value, revision, updated_at, updated_by FROM settings WHERE key = ?`), key).
		Scan(&st.Value, &st.Revision, tsScanner{&st.UpdatedAt}, &st.UpdatedBy); err != nil {
		return nil, notFound(err)
	}
	if st.Value == nil {
		st.Value = []byte{}
	}
	return st, nil
}

func (s *Store) PutSetting(ctx context.Context, key string, value []byte, expectedRevision int64, by string) (int64, error) {
	if key == "" {
		return 0, fmt.Errorf("sqlstore: setting key is required")
	}
	if value == nil {
		value = []byte{}
	}
	now := s.ts(time.Now())
	if expectedRevision == 0 {
		_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO settings (key, value, revision, updated_at, updated_by) VALUES (?, ?, 1, ?, ?)`),
			key, value, now, by)
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("%w: setting %q already exists", storage.ErrConflict, key)
		}
		if err != nil {
			return 0, err
		}
		return 1, nil
	}
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE settings SET value = ?, revision = revision + 1, updated_at = ?, updated_by = ?
		WHERE key = ? AND revision = ?`), value, now, by, key, expectedRevision)
	if err != nil {
		return 0, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return 0, err
	} else if n == 0 {
		return 0, fmt.Errorf("%w: setting %q is not at revision %d", storage.ErrConflict, key, expectedRevision)
	}
	return expectedRevision + 1, nil
}

func (s *Store) DeleteSetting(ctx context.Context, key string, expectedRevision int64) error {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM settings WHERE key = ? AND revision = ?`), key, expectedRevision)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		if _, err := s.GetSetting(ctx, key); err != nil {
			return err
		}
		return fmt.Errorf("%w: setting %q is not at revision %d", storage.ErrConflict, key, expectedRevision)
	}
	return nil
}

func (s *Store) ListSettings(ctx context.Context, prefix string) ([]*storage.Setting, error) {
	var w where
	if prefix != "" {
		w.add("substr(key, 1, ?) = ?", utf8.RuneCountInString(prefix), prefix)
	}
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT key, value, revision, updated_at, updated_by FROM settings`+w.String()+` ORDER BY key`), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.Setting
	for rows.Next() {
		st := &storage.Setting{}
		if err := rows.Scan(&st.Key, &st.Value, &st.Revision, tsScanner{&st.UpdatedAt}, &st.UpdatedBy); err != nil {
			return nil, err
		}
		if st.Value == nil {
			st.Value = []byte{}
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
