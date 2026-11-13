package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/storage"
)

// ---------------------------------------------------------------------------
// installation settings
// ---------------------------------------------------------------------------

// Every setting write checks its expected revision.

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
	return s.PutSettingIf(ctx, key, value, expectedRevision, by, nil)
}

func (s *Store) PutSettingIf(ctx context.Context, key string, value []byte, expectedRevision int64, by string, guards []storage.SettingRevision) (int64, error) {
	if len(guards) == 0 && s.dialect != Postgres {
		return s.putSetting(ctx, s.db, key, value, expectedRevision, by)
	}
	var revision int64
	err := s.withSettingRevisions(ctx, key, guards, func(tx *sql.Tx) error {
		var err error
		revision, err = s.putSetting(ctx, tx, key, value, expectedRevision, by)
		return err
	})
	if err != nil {
		return 0, err
	}
	return revision, nil
}

type statementExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) putSetting(ctx context.Context, db statementExecutor, key string, value []byte, expectedRevision int64, by string) (int64, error) {
	if key == "" {
		return 0, fmt.Errorf("sqlstore: setting key is required")
	}
	if value == nil {
		value = []byte{}
	}
	now := s.ts(time.Now())
	if expectedRevision == 0 {
		_, err := db.ExecContext(ctx, s.q(`INSERT INTO settings (key, value, revision, updated_at, updated_by) VALUES (?, ?, 1, ?, ?)`),
			key, value, now, by)
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("%w: setting %q already exists", storage.ErrConflict, key)
		}
		if err != nil {
			return 0, err
		}
		return 1, nil
	}
	res, err := db.ExecContext(ctx, s.q(`UPDATE settings SET value = ?, revision = revision + 1, updated_at = ?, updated_by = ?
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
	if s.dialect != Postgres {
		return s.deleteSetting(ctx, s.db, key, expectedRevision)
	}
	return s.withSettingRevisions(ctx, key, nil, func(tx *sql.Tx) error {
		return s.deleteSetting(ctx, tx, key, expectedRevision)
	})
}

type settingExecutor interface {
	statementExecutor
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) deleteSetting(ctx context.Context, db settingExecutor, key string, expectedRevision int64) error {
	res, err := db.ExecContext(ctx, s.q(`DELETE FROM settings WHERE key = ? AND revision = ?`), key, expectedRevision)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		var revision int64
		if err := db.QueryRowContext(ctx, s.q(`SELECT revision FROM settings WHERE key = ?`), key).Scan(&revision); err != nil {
			return notFound(err)
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

// withSettingRevisions holds the revisions stable through commit. SQLite opens
// write transactions with BEGIN IMMEDIATE. PostgreSQL uses transaction locks on
// the setting keys, including absent rows. All settings writers take these locks.
func (s *Store) withSettingRevisions(ctx context.Context, writeKey string, guards []storage.SettingRevision, write func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if s.dialect == Postgres {
		keys := make([]int32, 0, len(guards)+1)
		if writeKey != "" {
			keys = append(keys, settingLockKey(writeKey))
		}
		for _, guard := range guards {
			keys = append(keys, settingLockKey(guard.Key))
		}
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			query := "SELECT pg_advisory_xact_lock_shared($1, $2)"
			if writeKey != "" && key == settingLockKey(writeKey) {
				query = "SELECT pg_advisory_xact_lock($1, $2)"
			}
			if _, err := tx.ExecContext(ctx, query, lockSettings, key); err != nil {
				return err
			}
		}
	}
	for _, guard := range guards {
		var revision int64
		err := tx.QueryRowContext(ctx, s.q(`SELECT revision FROM settings WHERE key = ?`), guard.Key).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision != guard.Revision {
			return fmt.Errorf("%w: setting %q changed", storage.ErrConflict, guard.Key)
		}
	}
	if err := write(tx); err != nil {
		return err
	}
	return tx.Commit()
}

const lockSettings int32 = 0x4c46

func settingLockKey(key string) int32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return int32(hash.Sum32())
}
