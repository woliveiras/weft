// Package fsstore implements storage.Store on plain files, for single-node
// deployments that want zero dependencies and state an operator can read
// with cat, grep and jq.
//
// # Layout
//
//	<dir>/LOCK                               exclusive lock (one process per directory)
//	<dir>/graphs/<id>/<version>.json         one immutable file per graph version
//	<dir>/records/<id>.json                  graph control state
//	<dir>/events/events-<n>.jsonl            append-only event log, 64 MiB segments
//	<dir>/messages/<message-id>.json         every history row of one message
//	<dir>/processed.jsonl                    append-only dedupe log (group, message id)
//	<dir>/executions/<id>.json
//	<dir>/approvals/<id>.json
//	<dir>/operator/<id>.json                 operator messages
//	<dir>/replicas/<graph/node#n>.json       replica status
//	<dir>/kv/<namespace>/<key>               raw values
//	<dir>/leases/<key>.json
//	<dir>/settings/<key>.json                installation settings (revisioned)
//
// Names are encoded with encodeName so any key is a valid, case-safe file
// name on Linux, macOS and Windows.
//
// # Model
//
// The whole dataset is loaded into memory at Open and every mutation is
// written through to disk before the call returns. Reads never touch the
// disk. That makes the store simple and its reads fast, at the price of
// memory proportional to history - the reason it is positioned for
// development, demos and small single-node installs, with SQLite as the
// default and PostgreSQL for production.
//
// # Crash safety
//
//   - Entity files are replaced atomically (temp file, fsync, rename, fsync
//     directory), so a crash leaves the old or the new version, never a torn
//     one. Leftover temp files are removed at Open.
//   - Append-only logs (events, processed) fsync after every append. A crash
//     in the middle of an append can leave a partial last line; Open detects
//     it and truncates the log back to the last complete record. A corrupt
//     line anywhere else is reported as an error rather than silently skipped.
//   - Graph deletion renames the graph directory to a .trash- name first
//     (atomic) and removes it afterwards; Open finishes interrupted deletes.
//
// # Concurrency
//
// One mutex serialises every operation in the process; the LOCK file keeps
// a second process (or a second Store in this process) out, because the
// in-memory model assumes it is the only writer. Compare-and-set operations
// (graph versions, approvals, operator messages, leases, dedupe) are trivially
// atomic under the mutex.
package fsstore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// SegmentSize is the size at which a new event log segment is started.
const SegmentSize = 64 << 20

// AuthorLabel is the metadata label recorded as a version's author.
const AuthorLabel = "weft.io/author"

type graphVersion struct {
	version int
	created time.Time
	author  string
	doc     []byte
}

// Store implements storage.Store.
type Store struct {
	dir  string
	mu   sync.Mutex
	lock *dirLock
	open bool

	graphs     map[string][]graphVersion // ascending by version
	records    map[string]*domain.GraphRecord
	events     []*domain.Event
	eventIDs   map[string]struct{}
	segment    *os.File
	segNum     int
	segSize    int64
	messages   map[string][]*domain.MessageRecord // by message id
	processed  map[string]struct{}
	procFile   *os.File
	executions map[string]*domain.Execution
	approvals  map[string]*domain.Approval
	operator   map[string]*domain.OperatorMessage
	replicas   map[string]*domain.ReplicaStatus
	kv         map[string]map[string][]byte
	leases     map[string]domain.Lease
	settings   map[string]*storage.Setting
}

var _ storage.Store = (*Store)(nil)

// Open locks dir, creating it if needed, and loads its contents.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("fsstore: directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	lock, err := acquireDirLock(filepath.Join(abs, "LOCK"))
	if err != nil {
		return nil, err
	}
	s := &Store{dir: abs, lock: lock}
	if err := s.load(); err != nil {
		s.closeFiles()
		lock.release()
		return nil, err
	}
	s.open = true
	return s, nil
}

func (s *Store) Name() string { return "fs" }

// Dir is the store directory.
func (s *Store) Dir() string { return s.dir }

// Migrate creates the directory layout. The file format is versioned by
// <dir>/FORMAT so a future layout change can be detected and converted.
func (s *Store) Migrate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	for _, d := range []string{"graphs", "records", "events", "messages", "executions", "approvals", "operator", "replicas", "kv", "leases"} {
		if err := os.MkdirAll(filepath.Join(s.dir, d), 0o755); err != nil {
			return err
		}
	}
	format := filepath.Join(s.dir, "FORMAT")
	if b, err := os.ReadFile(format); err == nil {
		if strings.TrimSpace(string(b)) != "1" {
			return fmt.Errorf("fsstore: unsupported format %q in %s", strings.TrimSpace(string(b)), format)
		}
		return nil
	}
	return writeFileAtomic(format, []byte("1\n"))
}

func (s *Store) Ping(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	_, err := os.Stat(s.dir)
	return err
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open {
		return nil
	}
	s.open = false
	err := s.closeFiles()
	if lerr := s.lock.release(); err == nil {
		err = lerr
	}
	return err
}

func (s *Store) closeFiles() error {
	var first error
	for _, f := range []*os.File{s.segment, s.procFile} {
		if f != nil {
			if err := f.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	s.segment, s.procFile = nil, nil
	return first
}

func (s *Store) check() error {
	if !s.open {
		return errors.New("fsstore: store is closed")
	}
	return nil
}

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.dir}, parts...)...)
}

// ---------------------------------------------------------------------------
// loading
// ---------------------------------------------------------------------------

func (s *Store) load() error {
	s.graphs = map[string][]graphVersion{}
	s.records = map[string]*domain.GraphRecord{}
	s.eventIDs = map[string]struct{}{}
	s.messages = map[string][]*domain.MessageRecord{}
	s.processed = map[string]struct{}{}
	s.executions = map[string]*domain.Execution{}
	s.approvals = map[string]*domain.Approval{}
	s.operator = map[string]*domain.OperatorMessage{}
	s.replicas = map[string]*domain.ReplicaStatus{}
	s.kv = map[string]map[string][]byte{}
	s.leases = map[string]domain.Lease{}
	s.settings = map[string]*storage.Setting{}

	if err := cleanTemp(s.dir); err != nil {
		return fmt.Errorf("fsstore: clean temp files: %w", err)
	}
	if err := s.loadGraphs(); err != nil {
		return err
	}
	if err := loadDir(s.path("records"), func(_ string, r *domain.GraphRecord) { s.records[r.ID] = r }); err != nil {
		return err
	}
	if err := loadDir(s.path("executions"), func(_ string, e *domain.Execution) { s.executions[e.ID] = e }); err != nil {
		return err
	}
	if err := loadDir(s.path("approvals"), func(_ string, a *domain.Approval) { s.approvals[a.ID] = a }); err != nil {
		return err
	}
	if err := loadDir(s.path("operator"), func(_ string, m *domain.OperatorMessage) { s.operator[m.ID] = m }); err != nil {
		return err
	}
	if err := loadDir(s.path("replicas"), func(_ string, r *domain.ReplicaStatus) {
		s.replicas[replicaKey(r.GraphID, r.NodeID, r.Replica)] = r
	}); err != nil {
		return err
	}
	if err := loadDir(s.path("leases"), func(_ string, l *domain.Lease) { s.leases[l.Key] = *l }); err != nil {
		return err
	}
	if err := loadDir(s.path("settings"), func(_ string, st *storage.Setting) { s.settings[st.Key] = st }); err != nil {
		return err
	}
	if err := loadDir(s.path("messages"), func(_ string, rs *[]*domain.MessageRecord) {
		if len(*rs) > 0 {
			s.messages[(*rs)[0].Message.ID] = *rs
		}
	}); err != nil {
		return err
	}
	if err := s.loadKV(); err != nil {
		return err
	}
	if err := s.loadProcessed(); err != nil {
		return err
	}
	return s.loadEvents()
}

// loadDir decodes every *.json file in dir into a fresh T.
func loadDir[T any](dir string, add func(name string, v *T)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		v := new(T)
		if err := json.Unmarshal(b, v); err != nil {
			return fmt.Errorf("fsstore: decode %s: %w", filepath.Join(dir, e.Name()), err)
		}
		add(e.Name(), v)
	}
	return nil
}

func (s *Store) loadGraphs() error {
	root := s.path("graphs")
	dirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		id, err := decodeName(d.Name())
		if err != nil {
			return err
		}
		var versions []graphVersion
		err = loadDir(filepath.Join(root, d.Name()), func(name string, g *domain.Graph) {
			n, _ := strconv.Atoi(strings.TrimSuffix(name, ".json"))
			doc, _ := json.Marshal(g)
			versions = append(versions, graphVersion{version: n, created: g.Metadata.UpdatedAt, author: g.Metadata.Labels[AuthorLabel], doc: doc})
		})
		if err != nil {
			return err
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i].version < versions[j].version })
		if len(versions) > 0 {
			s.graphs[id] = versions
		}
	}
	return nil
}

func (s *Store) loadKV() error {
	root := s.path("kv")
	nss, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, nd := range nss {
		if !nd.IsDir() {
			continue
		}
		ns, err := decodeName(nd.Name())
		if err != nil {
			return err
		}
		keys, err := os.ReadDir(filepath.Join(root, nd.Name()))
		if err != nil {
			return err
		}
		m := map[string][]byte{}
		for _, kf := range keys {
			if kf.IsDir() {
				continue
			}
			key, err := decodeName(kf.Name())
			if err != nil {
				return err
			}
			v, err := os.ReadFile(filepath.Join(root, nd.Name(), kf.Name()))
			if err != nil {
				return err
			}
			m[key] = v
		}
		s.kv[ns] = m
	}
	return nil
}

// readLog reads a JSONL file line by line. A truncated or undecodable final
// line is the signature of a crash mid-append: the file is truncated back to
// the end of the last good line. Damage anywhere else is an error.
func readLog(path string, each func(line []byte) error) (size int64, err error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var good int64
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			complete := line[len(line)-1] == '\n'
			body := bytes.TrimSpace(line)
			var perr error
			if len(body) > 0 {
				if !complete {
					perr = errors.New("incomplete line")
				} else {
					perr = each(body)
				}
			}
			if perr != nil {
				// Is anything after this line? If so it is corruption, not a torn tail.
				if _, peek := r.Peek(1); peek == nil {
					return 0, fmt.Errorf("fsstore: %s: corrupt record at offset %d: %v", path, good, perr)
				}
				if err := f.Truncate(good); err != nil {
					return 0, err
				}
				if err := f.Sync(); err != nil {
					return 0, err
				}
				return good, nil
			}
			good += int64(len(line))
		}
		if rerr == io.EOF {
			return good, nil
		}
		if rerr != nil {
			return 0, rerr
		}
	}
}

func (s *Store) loadEvents() error {
	dir := s.path("events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var segs []int
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "events-%06d.jsonl", &n); err == nil && !e.IsDir() {
			segs = append(segs, n)
		}
	}
	sort.Ints(segs)
	var lastSize int64
	for _, n := range segs {
		size, err := readLog(s.segPath(n), func(line []byte) error {
			var ev domain.Event
			if err := json.Unmarshal(line, &ev); err != nil {
				return err
			}
			s.events = append(s.events, &ev)
			s.eventIDs[ev.ID] = struct{}{}
			return nil
		})
		if err != nil {
			return err
		}
		lastSize = size
	}
	if len(segs) == 0 {
		s.segNum = 1
	} else {
		s.segNum = segs[len(segs)-1]
	}
	f, err := os.OpenFile(s.segPath(s.segNum), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	s.segment, s.segSize = f, lastSize
	return nil
}

func (s *Store) segPath(n int) string {
	return s.path("events", fmt.Sprintf("events-%06d.jsonl", n))
}

func (s *Store) loadProcessed() error {
	path := s.path("processed.jsonl")
	if _, err := os.Stat(path); err == nil {
		if _, err := readLog(path, func(line []byte) error {
			var p [2]string
			if err := json.Unmarshal(line, &p); err != nil {
				return err
			}
			s.processed[procKey(p[0], p[1])] = struct{}{}
			return nil
		}); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	s.procFile = f
	return nil
}

// appendLine writes one record and fsyncs it.
func appendLine(f *os.File, v any) (int, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	b = append(b, '\n')
	n, err := f.Write(b)
	if err != nil {
		return n, err
	}
	return n, f.Sync()
}

// writeJSON persists v atomically.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// clone deep-copies a value through JSON so callers can never mutate the
// in-memory state (and so returned values look exactly like reloaded ones).
func clone[T any](v *T) *T {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("fsstore: clone %T: %v", v, err))
	}
	out := new(T)
	if err := json.Unmarshal(b, out); err != nil {
		panic(fmt.Sprintf("fsstore: clone %T: %v", v, err))
	}
	return out
}

func procKey(group, id string) string { return group + "\x00" + id }

func replicaKey(graph, node string, r int) string { return domain.ReplicaKey(graph, node, r) }
