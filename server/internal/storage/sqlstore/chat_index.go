package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Store) PutChatSetting(ctx context.Context, meta storage.ChatSummary, value []byte, expected int64) (int64, error) {
	var revision int64
	err := s.withSettingRevisions(ctx, meta.Key, nil, func(tx *sql.Tx) error {
		var err error
		revision, err = s.putSetting(ctx, tx, meta.Key, value, expected, "chat")
		if err != nil {
			return err
		}
		meta.Revision = revision
		return s.writeChatIndex(ctx, tx, meta)
	})
	return revision, err
}

func (s *Store) IndexChatSetting(ctx context.Context, meta storage.ChatSummary, expected int64) error {
	return s.withSettingRevisions(ctx, meta.Key, []storage.SettingRevision{{Key: meta.Key, Revision: expected}}, func(tx *sql.Tx) error {
		meta.Revision = expected
		return s.writeChatIndex(ctx, tx, meta)
	})
}

func (s *Store) writeChatIndex(ctx context.Context, tx *sql.Tx, meta storage.ChatSummary) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	archived := 0
	if meta.Archived {
		archived = 1
	}
	_, err = tx.ExecContext(ctx, s.q(`INSERT INTO chat_session_index (key, owner, archived, activity_ns, unread, running, revision, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET archived = excluded.archived, activity_ns = excluded.activity_ns,
		unread = excluded.unread, running = excluded.running, revision = excluded.revision, summary = excluded.summary`),
		meta.Key, meta.Owner, archived, meta.LastActivity.UnixNano(), meta.Unread, meta.WatchCount, meta.Revision, string(raw))
	return err
}

func (s *Store) ListChatSummaries(ctx context.Context, filter storage.ChatSummaryFilter) ([]storage.ChatSummary, error) {
	archived := 0
	if filter.Archived {
		archived = 1
	}
	var w where
	w.add("owner = ?", filter.Owner)
	w.add("archived = ?", archived)
	if filter.AfterKey != "" {
		after := filter.AfterActivity.UnixNano()
		w.add("(activity_ns < ? OR (activity_ns = ? AND key > ?))", after, after, filter.AfterKey)
	}
	w.args = append(w.args, storage.Limit(filter.Limit, 31, 200))
	return s.queryChatSummaries(ctx, `SELECT summary FROM chat_session_index`+w.String()+` ORDER BY activity_ns DESC, key LIMIT ?`, w.args...)
}

func (s *Store) ArchivedChatUnread(ctx context.Context, owner string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, s.q(`SELECT COALESCE(SUM(unread),0) FROM chat_session_index WHERE owner = ? AND archived = 1`), owner).Scan(&count)
	return count, err
}

func (s *Store) ListWatchedChats(ctx context.Context, after string, limit int) ([]storage.ChatSummary, error) {
	return s.queryChatSummaries(ctx, `SELECT summary FROM chat_session_index WHERE running > 0 AND key > ? ORDER BY key LIMIT ?`, after, storage.Limit(limit, 32, 200))
}

func (s *Store) queryChatSummaries(ctx context.Context, query string, args ...any) ([]storage.ChatSummary, error) {
	rows, err := s.db.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]storage.ChatSummary, 0)
	for rows.Next() {
		var raw []byte
		var summary storage.ChatSummary
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &summary); err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, rows.Err()
}

func (s *Store) ListChatIndexGaps(ctx context.Context, after string, limit int) ([]*storage.Setting, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT s.key, s.value, s.revision FROM settings s
		LEFT JOIN chat_session_index i ON s.key = i.key
		WHERE s.key >= 'chat/sessions/' AND s.key < 'chat/sessions0' AND s.key > ?
		  AND (i.key IS NULL OR i.revision <> s.revision)
		ORDER BY s.key LIMIT ?`), after, storage.Limit(limit, 32, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.Setting
	for rows.Next() {
		row := &storage.Setting{}
		if err := rows.Scan(&row.Key, &row.Value, &row.Revision); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
