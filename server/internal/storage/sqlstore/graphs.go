package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// AuthorLabel is the metadata label recorded as a version's author.
const AuthorLabel = "weft.io/author"

func (s *Store) SaveGraph(ctx context.Context, g *domain.Graph, expectedVersion int) (int, error) {
	if g == nil || g.Metadata.ID == "" {
		return 0, fmt.Errorf("sqlstore: graph id is required")
	}
	id := g.Metadata.ID
	// Microsecond precision is what PostgreSQL keeps; truncating on both
	// dialects makes the document and the column agree byte for byte.
	now := time.Now().UTC().Truncate(time.Microsecond)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	var current int
	var created time.Time
	err = tx.QueryRowContext(ctx, s.q("SELECT latest_version, created_at FROM graphs WHERE id = ?"), id).
		Scan(&current, tsScanner{&created})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		current, created = 0, now
	case err != nil:
		return 0, err
	}
	if expectedVersion > 0 && expectedVersion != current {
		return 0, fmt.Errorf("%w: graph %s is at version %d, expected %d", storage.ErrConflict, id, current, expectedVersion)
	}
	next := current + 1

	saved := g.Metadata
	g.Metadata.Version, g.Metadata.CreatedAt, g.Metadata.UpdatedAt = next, created, now
	doc, err := json.Marshal(g)
	if err != nil {
		g.Metadata = saved
		return 0, fmt.Errorf("sqlstore: marshal graph: %w", err)
	}
	fail := func(err error) (int, error) {
		g.Metadata = saved
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("%w: graph %s version %d was saved concurrently", storage.ErrConflict, id, next)
		}
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO graphs (id, latest_version, created_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET latest_version = excluded.latest_version, updated_at = excluded.updated_at`),
		id, next, s.ts(created), s.ts(now)); err != nil {
		return fail(err)
	}
	author := g.Metadata.Labels[AuthorLabel]
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO graph_versions (id, version, document, author, created_at) VALUES (?, ?, ?, ?, ?)`),
		id, next, string(doc), author, s.ts(now)); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	return next, nil
}

func (s *Store) GetGraph(ctx context.Context, id string, version int) (*domain.Graph, error) {
	var doc []byte
	var err error
	if version <= 0 {
		err = s.db.QueryRowContext(ctx, s.q(`SELECT v.document FROM graph_versions v JOIN graphs g ON g.id = v.id AND g.latest_version = v.version WHERE v.id = ?`), id).Scan(&doc)
	} else {
		err = s.db.QueryRowContext(ctx, s.q(`SELECT document FROM graph_versions WHERE id = ? AND version = ?`), id, version).Scan(&doc)
	}
	if err != nil {
		return nil, notFound(err)
	}
	var g domain.Graph
	if err := json.Unmarshal(doc, &g); err != nil {
		return nil, fmt.Errorf("sqlstore: decode graph %s: %w", id, err)
	}
	return &g, nil
}

func (s *Store) ListGraphs(ctx context.Context) ([]*domain.Graph, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT v.document FROM graph_versions v JOIN graphs g ON g.id = v.id AND g.latest_version = v.version ORDER BY v.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Graph{}
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var g domain.Graph
		if err := json.Unmarshal(doc, &g); err != nil {
			return nil, fmt.Errorf("sqlstore: decode graph: %w", err)
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

func (s *Store) ListGraphVersions(ctx context.Context, id string) ([]storage.GraphVersion, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT version, created_at, author FROM graph_versions WHERE id = ? ORDER BY version`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.GraphVersion
	for rows.Next() {
		var v storage.GraphVersion
		if err := rows.Scan(&v.Version, tsScanner{&v.CreatedAt}, &v.Author); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, storage.ErrNotFound
	}
	return out, nil
}

func (s *Store) DeleteGraph(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM graph_versions WHERE id = ?`), id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, s.q(`DELETE FROM graphs WHERE id = ?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM graph_records WHERE id = ?`), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetGraphRecord(ctx context.Context, id string) (*domain.GraphRecord, error) {
	var doc []byte
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT doc FROM graph_records WHERE id = ?`), id).Scan(&doc); err != nil {
		return nil, notFound(err)
	}
	var r domain.GraphRecord
	if err := json.Unmarshal(doc, &r); err != nil {
		return nil, fmt.Errorf("sqlstore: decode graph record: %w", err)
	}
	return &r, nil
}

func (s *Store) PutGraphRecord(ctx context.Context, r *domain.GraphRecord) error {
	return s.putGraphRecord(ctx, s.db, r)
}

func (s *Store) PutGraphRecordIf(ctx context.Context, r *domain.GraphRecord, guards []storage.SettingRevision) error {
	if len(guards) == 0 {
		return s.PutGraphRecord(ctx, r)
	}
	return s.withSettingRevisions(ctx, "", guards, func(tx *sql.Tx) error {
		return s.putGraphRecord(ctx, tx, r)
	})
}

func (s *Store) putGraphRecord(ctx context.Context, db statementExecutor, r *domain.GraphRecord) error {
	if r == nil || r.ID == "" {
		return fmt.Errorf("sqlstore: graph record id is required")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	doc, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, s.q(`INSERT INTO graph_records (id, version, desired_state, state, run_id, updated_at, doc) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET version = excluded.version, desired_state = excluded.desired_state, state = excluded.state,
		run_id = excluded.run_id, updated_at = excluded.updated_at, doc = excluded.doc`),
		r.ID, r.Version, string(r.DesiredState), string(r.State), r.RunID, s.ts(r.UpdatedAt), string(doc))
	return err
}

func (s *Store) ListGraphRecords(ctx context.Context) ([]*domain.GraphRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc FROM graph_records ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.GraphRecord{}
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var r domain.GraphRecord
		if err := json.Unmarshal(doc, &r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateGraphState(ctx context.Context, expected *domain.GraphRecord, state domain.GraphState) error {
	if expected == nil || expected.ID == "" {
		return fmt.Errorf("sqlstore: graph record id is required")
	}
	old, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	next := *expected
	next.State = state
	next.UpdatedAt = time.Now().UTC()
	doc, err := json.Marshal(next)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, s.q(`UPDATE graph_records SET state = ?, updated_at = ?, doc = ? WHERE id = ? AND doc = ?`), string(state), s.ts(next.UpdatedAt), string(doc), next.ID, string(old))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return storage.ErrConflict
	}
	return nil
}
