package fsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// ---------------------------------------------------------------------------
// graphs
// ---------------------------------------------------------------------------

func (s *Store) SaveGraph(ctx context.Context, g *domain.Graph, expectedVersion int) (int, error) {
	if g == nil || g.Metadata.ID == "" {
		return 0, fmt.Errorf("fsstore: graph id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return 0, err
	}
	id := g.Metadata.ID
	versions := s.graphs[id]
	current := 0
	now := time.Now().UTC()
	created := now
	if len(versions) > 0 {
		current = versions[len(versions)-1].version
		created = versions[0].created
		var first domain.Graph
		if err := json.Unmarshal(versions[len(versions)-1].doc, &first); err == nil && !first.Metadata.CreatedAt.IsZero() {
			created = first.Metadata.CreatedAt
		}
	}
	if expectedVersion > 0 && expectedVersion != current {
		return 0, fmt.Errorf("%w: graph %s is at version %d, expected %d", storage.ErrConflict, id, current, expectedVersion)
	}
	next := current + 1
	saved := g.Metadata
	g.Metadata.Version, g.Metadata.CreatedAt, g.Metadata.UpdatedAt = next, created, now
	doc, err := json.Marshal(g)
	if err == nil {
		err = writeJSON(s.path("graphs", encodeName(id), fmt.Sprintf("%08d.json", next)), g)
	}
	if err != nil {
		g.Metadata = saved
		return 0, err
	}
	s.graphs[id] = append(versions, graphVersion{version: next, created: now, author: g.Metadata.Labels[AuthorLabel], doc: doc})
	return next, nil
}

func (s *Store) GetGraph(ctx context.Context, id string, version int) (*domain.Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	versions := s.graphs[id]
	if len(versions) == 0 {
		return nil, storage.ErrNotFound
	}
	var doc []byte
	if version <= 0 {
		doc = versions[len(versions)-1].doc
	} else {
		for _, v := range versions {
			if v.version == version {
				doc = v.doc
			}
		}
	}
	if doc == nil {
		return nil, storage.ErrNotFound
	}
	var g domain.Graph
	if err := json.Unmarshal(doc, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) ListGraphs(ctx context.Context) ([]*domain.Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(s.graphs))
	for id := range s.graphs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*domain.Graph, 0, len(ids))
	for _, id := range ids {
		vs := s.graphs[id]
		var g domain.Graph
		if err := json.Unmarshal(vs[len(vs)-1].doc, &g); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, nil
}

func (s *Store) ListGraphVersions(ctx context.Context, id string) ([]storage.GraphVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	vs := s.graphs[id]
	if len(vs) == 0 {
		return nil, storage.ErrNotFound
	}
	out := make([]storage.GraphVersion, len(vs))
	for i, v := range vs {
		out[i] = storage.GraphVersion{Version: v.version, CreatedAt: v.created, Author: v.author}
	}
	return out, nil
}

func (s *Store) DeleteGraph(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	if len(s.graphs[id]) == 0 {
		return storage.ErrNotFound
	}
	dir := s.path("graphs", encodeName(id))
	trash := s.path("graphs", ".trash-"+encodeName(id)+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.Rename(dir, trash); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := syncDir(s.path("graphs")); err != nil {
		return err
	}
	delete(s.graphs, id)
	if err := os.RemoveAll(trash); err != nil {
		return err
	}
	if _, ok := s.records[id]; ok {
		if err := removeFile(s.path("records", encodeName(id)+".json")); err != nil {
			return err
		}
		delete(s.records, id)
	}
	return nil
}

func (s *Store) GetGraphRecord(ctx context.Context, id string) (*domain.GraphRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	r, ok := s.records[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return clone(r), nil
}

func (s *Store) PutGraphRecord(ctx context.Context, r *domain.GraphRecord) error {
	if r == nil || r.ID == "" {
		return fmt.Errorf("fsstore: graph record id is required")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	c := clone(r)
	if err := writeJSON(s.path("records", encodeName(r.ID)+".json"), c); err != nil {
		return err
	}
	s.records[r.ID] = c
	return nil
}

func (s *Store) ListGraphRecords(ctx context.Context) ([]*domain.GraphRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	out := make([]*domain.GraphRecord, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, clone(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ---------------------------------------------------------------------------
// events
// ---------------------------------------------------------------------------

func (s *Store) AppendEvent(ctx context.Context, ev *domain.Event) error {
	if ev == nil {
		return fmt.Errorf("fsstore: event is nil")
	}
	if ev.Type == "" {
		return fmt.Errorf("fsstore: event type is required")
	}
	if ev.ID == "" {
		ev.ID = domain.NewID("evt")
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	if _, dup := s.eventIDs[ev.ID]; dup {
		return fmt.Errorf("%w: event %s already exists", storage.ErrConflict, ev.ID)
	}
	var seq int64 = 1
	if n := len(s.events); n > 0 {
		seq = s.events[n-1].Seq + 1
	}
	c := clone(ev)
	c.Seq = seq
	if s.segSize >= SegmentSize {
		if err := s.segment.Close(); err != nil {
			return err
		}
		f, err := os.OpenFile(s.segPath(s.segNum+1), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		s.segNum, s.segment, s.segSize = s.segNum+1, f, 0
		if err := syncDir(s.path("events")); err != nil {
			return err
		}
	}
	n, err := appendLine(s.segment, c)
	s.segSize += int64(n)
	if err != nil {
		return fmt.Errorf("fsstore: append event: %w", err)
	}
	s.events = append(s.events, c)
	s.eventIDs[c.ID] = struct{}{}
	ev.Seq = seq
	return nil
}

func (s *Store) ListEvents(ctx context.Context, f storage.EventFilter) ([]*domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	limit := storage.Limit(f.Limit, 200, 5000)
	types := map[domain.EventType]bool{}
	for _, t := range f.Types {
		types[t] = true
	}
	match := func(e *domain.Event) bool {
		return (f.GraphID == "" || e.GraphID == f.GraphID) &&
			(f.NodeID == "" || e.NodeID == f.NodeID) &&
			(len(types) == 0 || types[e.Type]) &&
			e.Seq > f.AfterSeq && (f.BeforeSeq == 0 || e.Seq < f.BeforeSeq)
	}
	out := []*domain.Event{}
	if f.Ascending {
		// events are in seq order: binary search the start position
		start := sort.Search(len(s.events), func(i int) bool { return s.events[i].Seq > f.AfterSeq })
		for i := start; i < len(s.events) && len(out) < limit; i++ {
			if match(s.events[i]) {
				out = append(out, clone(s.events[i]))
			}
		}
		return out, nil
	}
	for i := len(s.events) - 1; i >= 0 && len(out) < limit; i-- {
		e := s.events[i]
		if e.Seq <= f.AfterSeq {
			break
		}
		if match(e) {
			out = append(out, clone(e))
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// messages
// ---------------------------------------------------------------------------

func (s *Store) RecordMessage(ctx context.Context, r *domain.MessageRecord) error {
	if r == nil || r.Message.ID == "" {
		return fmt.Errorf("fsstore: message record needs a message id")
	}
	if r.Channel == "" {
		return fmt.Errorf("fsstore: message record needs a channel")
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	c := clone(r)
	rows := append([]*domain.MessageRecord(nil), s.messages[r.Message.ID]...)
	replaced := false
	for i, old := range rows {
		if old.Channel == c.Channel && old.Group == c.Group {
			c.CreatedAt = old.CreatedAt // history keeps the first sighting
			rows[i] = c
			replaced = true
		}
	}
	if !replaced {
		rows = append(rows, c)
	}
	if err := writeJSON(s.path("messages", encodeName(r.Message.ID)+".json"), rows); err != nil {
		return err
	}
	s.messages[r.Message.ID] = rows
	return nil
}

func (s *Store) ListMessages(ctx context.Context, f storage.MessageFilter) ([]*domain.MessageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	var all []*domain.MessageRecord
	for _, rows := range s.messages {
		for _, r := range rows {
			m := &r.Message
			if (f.GraphID == "" || r.GraphID == f.GraphID) &&
				(f.Channel == "" || r.Channel == f.Channel) &&
				(f.NodeID == "" || m.Source == f.NodeID || m.Target == f.NodeID) &&
				(f.CorrelationID == "" || m.MetaString(domain.MetaCorrelationID) == f.CorrelationID) &&
				(f.Status == "" || r.Status == f.Status) &&
				(f.Before.IsZero() || r.CreatedAt.Before(f.Before)) {
				all = append(all, r)
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].Message.ID > all[j].Message.ID
	})
	limit := storage.Limit(f.Limit, 100, 1000)
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]*domain.MessageRecord, len(all))
	for i, r := range all {
		out[i] = clone(r)
	}
	return out, nil
}

func (s *Store) GetMessage(ctx context.Context, id string) ([]*domain.MessageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	rows := s.messages[id]
	if len(rows) == 0 {
		return nil, storage.ErrNotFound
	}
	out := make([]*domain.MessageRecord, len(rows))
	for i, r := range rows {
		out[i] = clone(r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) MarkProcessed(ctx context.Context, group, messageID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return false, err
	}
	k := procKey(group, messageID)
	if _, ok := s.processed[k]; ok {
		return false, nil
	}
	if _, err := appendLine(s.procFile, [2]string{group, messageID}); err != nil {
		return false, fmt.Errorf("fsstore: append processed: %w", err)
	}
	s.processed[k] = struct{}{}
	return true, nil
}

func (s *Store) IsProcessed(ctx context.Context, group, messageID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return false, err
	}
	_, ok := s.processed[procKey(group, messageID)]
	return ok, nil
}

// ---------------------------------------------------------------------------
// executions
// ---------------------------------------------------------------------------

func (s *Store) PutExecution(ctx context.Context, e *domain.Execution) error {
	if e == nil || e.ID == "" {
		return fmt.Errorf("fsstore: execution id is required")
	}
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	c := clone(e)
	if err := writeJSON(s.path("executions", encodeName(e.ID)+".json"), c); err != nil {
		return err
	}
	s.executions[e.ID] = c
	return nil
}

func (s *Store) GetExecution(ctx context.Context, id string) (*domain.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	e, ok := s.executions[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return clone(e), nil
}

func (s *Store) ListExecutions(ctx context.Context, f storage.ExecutionFilter) ([]*domain.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	var all []*domain.Execution
	for _, e := range s.executions {
		if (f.ParentExecutionID == "" || e.ParentExecutionID == f.ParentExecutionID) && (f.GraphID == "" || e.GraphID == f.GraphID) && (f.NodeID == "" || e.NodeID == f.NodeID) && (f.Status == "" || e.Status == f.Status) {
			all = append(all, e)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].StartedAt.Equal(all[j].StartedAt) {
			return all[i].StartedAt.After(all[j].StartedAt)
		}
		return all[i].ID > all[j].ID
	})
	return cloneLimited(all, storage.Limit(f.Limit, 100, 1000)), nil
}

// ---------------------------------------------------------------------------
// approvals
// ---------------------------------------------------------------------------

func (s *Store) PutApproval(ctx context.Context, a *domain.Approval) error {
	if a == nil || a.ID == "" {
		return fmt.Errorf("fsstore: approval id is required")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = domain.ApprovalPending
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	return s.putApprovalLocked(clone(a))
}

func (s *Store) putApprovalLocked(a *domain.Approval) error {
	if err := writeJSON(s.path("approvals", encodeName(a.ID)+".json"), a); err != nil {
		return err
	}
	s.approvals[a.ID] = a
	return nil
}

func (s *Store) GetApproval(ctx context.Context, id string) (*domain.Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	a, ok := s.approvals[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return clone(a), nil
}

func (s *Store) ListApprovals(ctx context.Context, f storage.ApprovalFilter) ([]*domain.Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	var all []*domain.Approval
	for _, a := range s.approvals {
		if (f.GraphID == "" || a.GraphID == f.GraphID) && (f.Status == "" || a.Status == f.Status) {
			all = append(all, a)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].ID > all[j].ID
	})
	return cloneLimited(all, storage.Limit(f.Limit, 100, 1000)), nil
}

func (s *Store) DecideApproval(ctx context.Context, id string, status domain.ApprovalStatus, by, comment, answer string) (*domain.Approval, error) {
	if status == domain.ApprovalPending || status == "" {
		return nil, fmt.Errorf("fsstore: cannot decide approval to %q", status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	cur, ok := s.approvals[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	if cur.Status != domain.ApprovalPending {
		return nil, fmt.Errorf("%w: approval %s is already %s", storage.ErrConflict, id, cur.Status)
	}
	a := clone(cur)
	now := time.Now().UTC()
	a.Status, a.DecidedBy, a.Comment, a.Answer, a.DecidedAt = status, by, comment, answer, &now
	if err := s.putApprovalLocked(a); err != nil {
		return nil, err
	}
	return clone(a), nil
}

// ---------------------------------------------------------------------------
// operator messages
// ---------------------------------------------------------------------------

func (s *Store) PutOperatorMessage(ctx context.Context, m *domain.OperatorMessage) error {
	if m == nil || m.ID == "" {
		return fmt.Errorf("fsstore: operator message id is required")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	return s.putOperatorLocked(clone(m))
}

func (s *Store) putOperatorLocked(m *domain.OperatorMessage) error {
	if err := writeJSON(s.path("operator", encodeName(m.ID)+".json"), m); err != nil {
		return err
	}
	s.operator[m.ID] = m
	return nil
}

func sortOperator(ms []*domain.OperatorMessage) {
	sort.Slice(ms, func(i, j int) bool {
		if !ms[i].CreatedAt.Equal(ms[j].CreatedAt) {
			return ms[i].CreatedAt.Before(ms[j].CreatedAt)
		}
		return ms[i].ID < ms[j].ID
	})
}

func (s *Store) ConsumeOperatorMessages(ctx context.Context, graph, node string, replica int) ([]*domain.OperatorMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	var cands []*domain.OperatorMessage
	for _, m := range s.operator {
		if m.GraphID == graph && m.NodeID == node && m.Consumed == nil && (m.Replica == replica || m.Replica == -1) {
			cands = append(cands, m)
		}
	}
	sortOperator(cands)
	out := []*domain.OperatorMessage{}
	for _, cur := range cands {
		m := clone(cur)
		now := time.Now().UTC()
		m.Consumed = &now
		if err := s.putOperatorLocked(m); err != nil {
			return out, err
		}
		out = append(out, clone(m))
	}
	return out, nil
}

func (s *Store) ListOperatorMessages(ctx context.Context, graph, node string, limit int) ([]*domain.OperatorMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	var all []*domain.OperatorMessage
	for _, m := range s.operator {
		if m.GraphID == graph && (node == "" || m.NodeID == node) {
			all = append(all, m)
		}
	}
	sortOperator(all)
	limit = storage.Limit(limit, 100, 1000)
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return cloneLimited(all, limit), nil
}

// ---------------------------------------------------------------------------
// replica status
// ---------------------------------------------------------------------------

func (s *Store) PutReplicaStatus(ctx context.Context, st *domain.ReplicaStatus) error {
	if st == nil || st.GraphID == "" || st.NodeID == "" {
		return fmt.Errorf("fsstore: replica status needs graph and node")
	}
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	k := replicaKey(st.GraphID, st.NodeID, st.Replica)
	c := clone(st)
	if err := writeJSON(s.path("replicas", encodeName(k)+".json"), c); err != nil {
		return err
	}
	s.replicas[k] = c
	return nil
}

func (s *Store) ListReplicaStatus(ctx context.Context, graph string) ([]*domain.ReplicaStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	var all []*domain.ReplicaStatus
	for _, r := range s.replicas {
		if r.GraphID == graph {
			all = append(all, r)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].NodeID != all[j].NodeID {
			return all[i].NodeID < all[j].NodeID
		}
		return all[i].Replica < all[j].Replica
	})
	return cloneLimited(all, len(all)), nil
}

func (s *Store) DeleteReplicaStatus(ctx context.Context, graph, node string, replica int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	k := replicaKey(graph, node, replica)
	if _, ok := s.replicas[k]; !ok {
		return nil
	}
	if err := removeFile(s.path("replicas", encodeName(k)+".json")); err != nil {
		return err
	}
	delete(s.replicas, k)
	return nil
}

// ---------------------------------------------------------------------------
// KV
// ---------------------------------------------------------------------------

func (s *Store) KVGet(ctx context.Context, ns, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	v, ok := s.kv[ns][key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return append([]byte{}, v...), nil
}

func (s *Store) KVPut(ctx context.Context, ns, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	v := append([]byte{}, value...)
	if err := writeFileAtomic(s.path("kv", encodeName(ns), encodeName(key)), v); err != nil {
		return err
	}
	if s.kv[ns] == nil {
		s.kv[ns] = map[string][]byte{}
	}
	s.kv[ns][key] = v
	return nil
}

func (s *Store) KVDelete(ctx context.Context, ns, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	if _, ok := s.kv[ns][key]; !ok {
		return nil
	}
	if err := removeFile(s.path("kv", encodeName(ns), encodeName(key))); err != nil {
		return err
	}
	delete(s.kv[ns], key)
	return nil
}

func (s *Store) KVList(ctx context.Context, ns, prefix string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for k, v := range s.kv[ns] {
		if strings.HasPrefix(k, prefix) {
			out[k] = append([]byte{}, v...)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// leases
// ---------------------------------------------------------------------------

func (s *Store) AcquireLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	if key == "" || holder == "" {
		return false, fmt.Errorf("fsstore: lease key and holder are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	if cur, ok := s.leases[key]; ok && cur.Holder != holder && cur.ExpiresAt.After(now) {
		return false, nil
	}
	l := domain.Lease{Key: key, Holder: holder, ExpiresAt: now.Add(ttl)}
	if err := writeJSON(s.path("leases", encodeName(key)+".json"), l); err != nil {
		return false, err
	}
	s.leases[key] = l
	return true, nil
}

func (s *Store) ReleaseLease(ctx context.Context, key, holder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	cur, ok := s.leases[key]
	if !ok || cur.Holder != holder {
		return nil
	}
	if err := removeFile(s.path("leases", encodeName(key)+".json")); err != nil {
		return err
	}
	delete(s.leases, key)
	return nil
}

func (s *Store) ListLeases(ctx context.Context, prefix string) ([]domain.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	out := []domain.Lease{}
	for k, l := range s.leases {
		if strings.HasPrefix(k, prefix) {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func cloneLimited[T any](in []*T, limit int) []*T {
	if len(in) > limit {
		in = in[:limit]
	}
	out := make([]*T, len(in))
	for i, v := range in {
		out[i] = clone(v)
	}
	return out
}

func (s *Store) PutOperatorMessageOnce(ctx context.Context, m *domain.OperatorMessage) error {
	if m == nil || m.ID == "" {
		return fmt.Errorf("fsstore: operator message id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	if _, ok := s.operator[m.ID]; ok {
		return nil
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	return s.putOperatorLocked(clone(m))
}

func (s *Store) UpdateGraphState(ctx context.Context, expected *domain.GraphRecord, state domain.GraphState) error {
	if expected == nil || expected.ID == "" {
		return fmt.Errorf("fsstore: graph record id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	current, ok := s.records[expected.ID]
	if !ok {
		return storage.ErrNotFound
	}
	if !reflect.DeepEqual(current, expected) {
		return storage.ErrConflict
	}
	next := clone(current)
	next.State = state
	next.UpdatedAt = time.Now().UTC()
	if err := writeJSON(s.path("records", encodeName(next.ID)+".json"), next); err != nil {
		return err
	}
	s.records[next.ID] = next
	return nil
}
