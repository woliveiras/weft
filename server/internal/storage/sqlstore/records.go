package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// ---------------------------------------------------------------------------
// message history
// ---------------------------------------------------------------------------

func (s *Store) RecordMessage(ctx context.Context, r *domain.MessageRecord) error {
	if r == nil || r.Message.ID == "" {
		return fmt.Errorf("sqlstore: message record needs a message id")
	}
	if r.Channel == "" {
		return fmt.Errorf("sqlstore: message record needs a channel")
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	env, err := json.Marshal(r.Message)
	if err != nil {
		return fmt.Errorf("sqlstore: marshal message: %w", err)
	}
	m := &r.Message
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO messages (message_id, channel, grp, graph_id, type, source, target, correlation_id,
			status, attempt, consumer, error, envelope, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (message_id, channel, grp) DO UPDATE SET
			graph_id = excluded.graph_id, type = excluded.type, source = excluded.source, target = excluded.target,
			correlation_id = excluded.correlation_id, status = excluded.status, attempt = excluded.attempt,
			consumer = excluded.consumer, error = excluded.error, envelope = excluded.envelope, updated_at = excluded.updated_at`),
		m.ID, r.Channel, r.Group, r.GraphID, m.Type, m.Source, m.Target, m.MetaString(domain.MetaCorrelationID),
		string(r.Status), r.Attempt, r.Consumer, r.Error, string(env), s.ts(r.CreatedAt), s.ts(r.UpdatedAt))
	return err
}

const messageCols = `envelope, graph_id, channel, grp, status, attempt, consumer, error, created_at, updated_at`

func (s *Store) scanMessages(rows *sql.Rows) ([]*domain.MessageRecord, error) {
	defer rows.Close()
	out := []*domain.MessageRecord{}
	for rows.Next() {
		var r domain.MessageRecord
		var env []byte
		var status string
		if err := rows.Scan(&env, &r.GraphID, &r.Channel, &r.Group, &status, &r.Attempt, &r.Consumer, &r.Error,
			tsScanner{&r.CreatedAt}, tsScanner{&r.UpdatedAt}); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(env, &r.Message); err != nil {
			return nil, fmt.Errorf("sqlstore: decode message: %w", err)
		}
		r.Status = domain.MessageStatus(status)
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *Store) ListMessages(ctx context.Context, f storage.MessageFilter) ([]*domain.MessageRecord, error) {
	var w where
	if f.GraphID != "" {
		w.add("graph_id = ?", f.GraphID)
	}
	if f.Channel != "" {
		w.add("channel = ?", f.Channel)
	}
	if f.NodeID != "" {
		w.add("(source = ? OR target = ?)", f.NodeID, f.NodeID)
	}
	if f.CorrelationID != "" {
		w.add("correlation_id = ?", f.CorrelationID)
	}
	if f.Status != "" {
		w.add("status = ?", string(f.Status))
	}
	if !f.Before.IsZero() {
		w.add("created_at < ?", s.ts(f.Before))
	}
	limit := storage.Limit(f.Limit, 100, 1000)
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+messageCols+` FROM messages`+w.String()+
		fmt.Sprintf(` ORDER BY created_at DESC, message_id DESC LIMIT %d`, limit)), w.args...)
	if err != nil {
		return nil, err
	}
	return s.scanMessages(rows)
}

func (s *Store) GetMessage(ctx context.Context, id string) ([]*domain.MessageRecord, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+messageCols+` FROM messages WHERE message_id = ? ORDER BY created_at, channel, grp`), id)
	if err != nil {
		return nil, err
	}
	out, err := s.scanMessages(rows)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, storage.ErrNotFound
	}
	return out, nil
}

func (s *Store) MarkProcessed(ctx context.Context, group, messageID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.q(`INSERT INTO processed (grp, message_id, processed_at) VALUES (?, ?, ?) ON CONFLICT (grp, message_id) DO NOTHING`),
		group, messageID, s.ts(time.Now()))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *Store) IsProcessed(ctx context.Context, group, messageID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM processed WHERE grp = ? AND message_id = ?`), group, messageID).Scan(&n)
	return n > 0, err
}

// ---------------------------------------------------------------------------
// executions
// ---------------------------------------------------------------------------

func (s *Store) PutExecution(ctx context.Context, e *domain.Execution) error {
	if e == nil || e.ID == "" {
		return fmt.Errorf("sqlstore: execution id is required")
	}
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now().UTC()
	}
	doc, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO executions (id, graph_id, node_id, replica, status, started_at, doc, parent_execution_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET graph_id = excluded.graph_id, node_id = excluded.node_id, replica = excluded.replica,
		status = excluded.status, started_at = excluded.started_at, doc = excluded.doc, parent_execution_id = excluded.parent_execution_id`),
		e.ID, e.GraphID, e.NodeID, e.Replica, string(e.Status), s.ts(e.StartedAt), string(doc), e.ParentExecutionID)
	return err
}

func (s *Store) GetExecution(ctx context.Context, id string) (*domain.Execution, error) {
	var doc []byte
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT doc FROM executions WHERE id = ?`), id).Scan(&doc); err != nil {
		return nil, notFound(err)
	}
	var e domain.Execution
	if err := json.Unmarshal(doc, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListExecutions(ctx context.Context, f storage.ExecutionFilter) ([]*domain.Execution, error) {
	var w where
	if f.ParentExecutionID != "" {
		w.add("parent_execution_id = ?", f.ParentExecutionID)
	}
	if f.GraphID != "" {
		w.add("graph_id = ?", f.GraphID)
	}
	if f.NodeID != "" {
		w.add("node_id = ?", f.NodeID)
	}
	if f.Status != "" {
		w.add("status = ?", string(f.Status))
	}
	limit := storage.Limit(f.Limit, 100, 1000)
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT doc FROM executions`+w.String()+
		fmt.Sprintf(` ORDER BY started_at DESC, id DESC LIMIT %d`, limit)), w.args...)
	if err != nil {
		return nil, err
	}
	return scanDocs[domain.Execution](rows)
}

// ---------------------------------------------------------------------------
// approvals
// ---------------------------------------------------------------------------

func (s *Store) PutApproval(ctx context.Context, a *domain.Approval) error {
	if a == nil || a.ID == "" {
		return fmt.Errorf("sqlstore: approval id is required")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = domain.ApprovalPending
	}
	doc, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO approvals (id, graph_id, status, created_at, doc) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET graph_id = excluded.graph_id, status = excluded.status, created_at = excluded.created_at, doc = excluded.doc`),
		a.ID, a.GraphID, string(a.Status), s.ts(a.CreatedAt), string(doc))
	return err
}

func (s *Store) GetApproval(ctx context.Context, id string) (*domain.Approval, error) {
	var doc []byte
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT doc FROM approvals WHERE id = ?`), id).Scan(&doc); err != nil {
		return nil, notFound(err)
	}
	var a domain.Approval
	if err := json.Unmarshal(doc, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) ListApprovals(ctx context.Context, f storage.ApprovalFilter) ([]*domain.Approval, error) {
	var w where
	if f.GraphID != "" {
		w.add("graph_id = ?", f.GraphID)
	}
	if f.Status != "" {
		w.add("status = ?", string(f.Status))
	}
	limit := storage.Limit(f.Limit, 100, 1000)
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT doc FROM approvals`+w.String()+
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT %d`, limit)), w.args...)
	if err != nil {
		return nil, err
	}
	return scanDocs[domain.Approval](rows)
}

// DecideApproval reads the approval, then updates it with a guard on
// status = 'pending'. Two deciders racing both read "pending", but only one
// UPDATE matches the guard; the other sees zero rows and gets ErrConflict.
func (s *Store) DecideApproval(ctx context.Context, id string, status domain.ApprovalStatus, by, comment, answer string) (*domain.Approval, error) {
	if status == domain.ApprovalPending || status == "" {
		return nil, fmt.Errorf("sqlstore: cannot decide approval to %q", status)
	}
	a, err := s.GetApproval(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.Status != domain.ApprovalPending {
		return nil, fmt.Errorf("%w: approval %s is already %s", storage.ErrConflict, id, a.Status)
	}
	now := time.Now().UTC()
	a.Status, a.DecidedBy, a.Comment, a.Answer, a.DecidedAt = status, by, comment, answer, &now
	doc, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE approvals SET status = ?, doc = ? WHERE id = ? AND status = ?`),
		string(status), string(doc), id, string(domain.ApprovalPending))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("%w: approval %s was decided concurrently", storage.ErrConflict, id)
	}
	return a, nil
}

// ---------------------------------------------------------------------------
// operator messages
// ---------------------------------------------------------------------------

func (s *Store) PutOperatorMessage(ctx context.Context, m *domain.OperatorMessage) error {
	if m == nil || m.ID == "" {
		return fmt.Errorf("sqlstore: operator message id is required")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	doc, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO operator_messages (id, graph_id, node_id, replica, created_at, consumed_at, doc) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET graph_id = excluded.graph_id, node_id = excluded.node_id, replica = excluded.replica,
		created_at = excluded.created_at, consumed_at = excluded.consumed_at, doc = excluded.doc`),
		m.ID, m.GraphID, m.NodeID, m.Replica, s.ts(m.CreatedAt), s.tsPtr(m.Consumed), string(doc))
	return err
}

// ConsumeOperatorMessages claims each candidate with a guarded update
// ("consumed_at IS NULL"), so a message addressed to any replica (-1) is
// handed to exactly one of the replicas asking concurrently.
func (s *Store) ConsumeOperatorMessages(ctx context.Context, graph, node string, replica int) ([]*domain.OperatorMessage, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT doc FROM operator_messages
		WHERE graph_id = ? AND node_id = ? AND consumed_at IS NULL AND (replica = ? OR replica = -1)
		ORDER BY created_at, id`), graph, node, replica)
	if err != nil {
		return nil, err
	}
	candidates, err := scanDocs[domain.OperatorMessage](rows)
	if err != nil {
		return nil, err
	}
	out := []*domain.OperatorMessage{}
	for _, m := range candidates {
		now := time.Now().UTC()
		m.Consumed = &now
		doc, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		res, err := s.db.ExecContext(ctx, s.q(`UPDATE operator_messages SET consumed_at = ?, doc = ? WHERE id = ? AND consumed_at IS NULL`),
			s.ts(now), string(doc), m.ID)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			out = append(out, m)
		}
	}
	return out, nil
}

// ListOperatorMessages returns the latest limit messages in chronological
// order (oldest first), the order a chat transcript is read in.
func (s *Store) ListOperatorMessages(ctx context.Context, graph, node string, limit int) ([]*domain.OperatorMessage, error) {
	var w where
	w.add("graph_id = ?", graph)
	if node != "" {
		w.add("node_id = ?", node)
	}
	limit = storage.Limit(limit, 100, 1000)
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT doc FROM operator_messages`+w.String()+
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT %d`, limit)), w.args...)
	if err != nil {
		return nil, err
	}
	out, err := scanDocs[domain.OperatorMessage](rows)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// replica status
// ---------------------------------------------------------------------------

func (s *Store) PutReplicaStatus(ctx context.Context, st *domain.ReplicaStatus) error {
	if st == nil || st.GraphID == "" || st.NodeID == "" {
		return fmt.Errorf("sqlstore: replica status needs graph and node")
	}
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	doc, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO replica_status (graph_id, node_id, replica, updated_at, doc) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (graph_id, node_id, replica) DO UPDATE SET updated_at = excluded.updated_at, doc = excluded.doc`),
		st.GraphID, st.NodeID, st.Replica, s.ts(st.UpdatedAt), string(doc))
	return err
}

func (s *Store) ListReplicaStatus(ctx context.Context, graph string) ([]*domain.ReplicaStatus, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT doc FROM replica_status WHERE graph_id = ? ORDER BY node_id, replica`), graph)
	if err != nil {
		return nil, err
	}
	return scanDocs[domain.ReplicaStatus](rows)
}

func (s *Store) DeleteReplicaStatus(ctx context.Context, graph, node string, replica int) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM replica_status WHERE graph_id = ? AND node_id = ? AND replica = ?`), graph, node, replica)
	return err
}

// ---------------------------------------------------------------------------
// KV
// ---------------------------------------------------------------------------

func (s *Store) KVGet(ctx context.Context, ns, key string) ([]byte, error) {
	var v []byte
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT value FROM kv WHERE ns = ? AND key = ?`), ns, key).Scan(&v); err != nil {
		return nil, notFound(err)
	}
	if v == nil {
		v = []byte{}
	}
	return v, nil
}

func (s *Store) KVPut(ctx context.Context, ns, key string, value []byte) error {
	if value == nil {
		value = []byte{}
	}
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO kv (ns, key, value, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (ns, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`),
		ns, key, value, s.ts(time.Now()))
	return err
}

func (s *Store) KVDelete(ctx context.Context, ns, key string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM kv WHERE ns = ? AND key = ?`), ns, key)
	return err
}

// KVList matches the prefix with substr() rather than LIKE: SQLite's LIKE is
// case-insensitive by default and both databases would need escaping of %
// and _, while substr() is exact and portable.
func (s *Store) KVList(ctx context.Context, ns, prefix string) (map[string][]byte, error) {
	var w where
	w.add("ns = ?", ns)
	if prefix != "" {
		w.add("substr(key, 1, ?) = ?", utf8.RuneCountInString(prefix), prefix)
	}
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT key, value FROM kv`+w.String()), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if v == nil {
			v = []byte{}
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// leases
// ---------------------------------------------------------------------------

// AcquireLease is a single compare-and-set upsert: insert when free, or
// overwrite only when the current holder is the caller or the lease has
// expired. The database evaluates the guard atomically, so of N concurrent
// callers exactly one observes a changed row.
func (s *Store) AcquireLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	if key == "" || holder == "" {
		return false, fmt.Errorf("sqlstore: lease key and holder are required")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, s.q(`INSERT INTO leases (key, holder, expires_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET holder = excluded.holder, expires_at = excluded.expires_at
		WHERE leases.holder = excluded.holder OR leases.expires_at <= ?`),
		key, holder, s.ts(now.Add(ttl)), s.ts(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *Store) ReleaseLease(ctx context.Context, key, holder string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM leases WHERE key = ? AND holder = ?`), key, holder)
	return err
}

func (s *Store) ListLeases(ctx context.Context, prefix string) ([]domain.Lease, error) {
	var w where
	if prefix != "" {
		w.add("substr(key, 1, ?) = ?", utf8.RuneCountInString(prefix), prefix)
	}
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT key, holder, expires_at FROM leases`+w.String()+` ORDER BY key`), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Lease{}
	for rows.Next() {
		var l domain.Lease
		if err := rows.Scan(&l.Key, &l.Holder, tsScanner{&l.ExpiresAt}); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------

func scanDocs[T any](rows *sql.Rows) ([]*T, error) {
	defer rows.Close()
	out := []*T{}
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		v := new(T)
		if err := json.Unmarshal(doc, v); err != nil {
			return nil, fmt.Errorf("sqlstore: decode %T: %w", v, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PutOperatorMessageOnce is an idempotent outbox delivery; it never resurrects
// a message consumed before a controller crash.
func (s *Store) PutOperatorMessageOnce(ctx context.Context, m *domain.OperatorMessage) error {
	if m == nil || m.ID == "" {
		return fmt.Errorf("sqlstore: operator message id is required")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	doc, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.q(`INSERT INTO operator_messages (id, graph_id, node_id, replica, created_at, consumed_at, doc) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`), m.ID, m.GraphID, m.NodeID, m.Replica, s.ts(m.CreatedAt), s.tsPtr(m.Consumed), string(doc))
	return err
}
