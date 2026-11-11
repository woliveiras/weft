package sqlstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

func (s *Store) AppendEvent(ctx context.Context, ev *domain.Event) error {
	if ev == nil {
		return fmt.Errorf("sqlstore: event is nil")
	}
	if ev.ID == "" {
		ev.ID = domain.NewID("evt")
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if ev.Type == "" {
		return fmt.Errorf("sqlstore: event type is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if s.dialect == Postgres {
		// Serialise appends so seq order equals commit order (see package doc).
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", lockEvents); err != nil {
			return err
		}
	}
	var seq int64
	err = tx.QueryRowContext(ctx, s.q(`INSERT INTO events (id, type, time, graph_id, node_id, replica, execution_id, message_id, actor, trace_id, level, data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING seq`),
		ev.ID, string(ev.Type), s.ts(ev.Time), ev.GraphID, ev.NodeID, ev.Replica, ev.ExecutionID, ev.MessageID,
		ev.Actor, ev.TraceID, ev.Level, jsonArg(ev.Data)).Scan(&seq)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: event %s already exists", storage.ErrConflict, ev.ID)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	ev.Seq = seq
	return nil
}

func (s *Store) ListEvents(ctx context.Context, f storage.EventFilter) ([]*domain.Event, error) {
	var w where
	if f.GraphID != "" {
		w.add("graph_id = ?", f.GraphID)
	}
	if f.NodeID != "" {
		w.add("node_id = ?", f.NodeID)
	}
	if len(f.Types) > 0 {
		args := make([]any, len(f.Types))
		for i, t := range f.Types {
			args[i] = string(t)
		}
		w.add("type IN ("+placeholders(len(args))+")", args...)
	}
	if f.AfterSeq > 0 {
		w.add("seq > ?", f.AfterSeq)
	}
	if f.BeforeSeq > 0 {
		w.add("seq < ?", f.BeforeSeq)
	}
	order := "DESC"
	if f.Ascending {
		order = "ASC"
	}
	limit := storage.Limit(f.Limit, 200, 5000)
	query := `SELECT seq, id, type, time, graph_id, node_id, replica, execution_id, message_id, actor, trace_id, level, data FROM events` +
		w.String() + " ORDER BY seq " + order + fmt.Sprintf(" LIMIT %d", limit)
	rows, err := s.db.QueryContext(ctx, s.q(query), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Event{}
	for rows.Next() {
		var ev domain.Event
		var typ string
		var data []byte
		if err := rows.Scan(&ev.Seq, &ev.ID, &typ, tsScanner{&ev.Time}, &ev.GraphID, &ev.NodeID, &ev.Replica,
			&ev.ExecutionID, &ev.MessageID, &ev.Actor, &ev.TraceID, &ev.Level, &data); err != nil {
			return nil, err
		}
		ev.Type = domain.EventType(typ)
		if len(data) > 0 {
			ev.Data = json.RawMessage(data)
		}
		out = append(out, &ev)
	}
	return out, rows.Err()
}
