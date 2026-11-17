package repositoryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/storage"
)

const prefix = "repository-imports/"

var validID = regexp.MustCompile(`^imp_[a-f0-9]{64}$`)

func randomID() string { return domain.NewID("attempt") }

type Input struct {
	Authentication Authentication `json:"authentication,omitempty"`
	URL            string         `json:"url"`
	Ref            string         `json:"ref"`
	Retry          bool           `json:"retry,omitempty"`
	Revision       int64          `json:"revision,omitempty"`
}
type Record struct {
	Authentication string    `json:"authentication,omitempty"`
	ID             string    `json:"id"`
	URL            string    `json:"url"`
	Ref            string    `json:"ref"`
	State          string    `json:"state"`
	Message        string    `json:"message"`
	Revision       int64     `json:"revision"`
	Attempt        string    `json:"attempt"`
	ProjectID      string    `json:"projectId,omitempty"`
	Commit         string    `json:"commit,omitempty"`
	Bytes          int64     `json:"bytes"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Deadline       time.Time `json:"deadline"`
}
type Options struct {
	Native bool
	Store  storage.Store
	Root   string
	Log    *slog.Logger
	Client *http.Client
}
type progress struct {
	attempt string
	bytes   *atomic.Int64
}
type Manager struct {
	native   bool
	progress sync.Map
	store    storage.Store
	root     string
	log      *slog.Logger
	client   *http.Client
	ctx      context.Context
	stop     context.CancelFunc
	mu       sync.Mutex
	active   map[string]context.CancelFunc
	closed   bool
	wg       sync.WaitGroup
}

func New(o Options) (*Manager, error) {
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if o.Client == nil {
		o.Client = publicClient()
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Manager{native: o.Native, store: o.Store, root: root, log: o.Log, client: o.Client, ctx: ctx, stop: stop, active: map[string]context.CancelFunc{}}, nil
}
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.stop()
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		m.client.CloseIdleConnections()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) read(ctx context.Context, id string) (*Record, error) {
	if !validID.MatchString(id) {
		return nil, storage.ErrNotFound
	}
	row, err := m.store.GetSetting(ctx, prefix+id)
	if err != nil {
		return nil, err
	}
	var r Record
	if err = json.Unmarshal(row.Value, &r); err != nil {
		return nil, err
	}
	r.Revision = row.Revision
	return &r, nil
}
func (m *Manager) save(ctx context.Context, r *Record, actor string) error {
	r.UpdatedAt = time.Now().UTC()
	if err := m.store.AppendEvent(ctx, domain.NewEvent("repository.import_transition", "", map[string]any{"importId": r.ID, "attempt": r.Attempt, "state": r.State, "bytes": r.Bytes, "authentication": r.Authentication}).WithActor(actor)); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	rev, err := m.store.PutSetting(ctx, prefix+r.ID, b, r.Revision, actor)
	if err != nil {
		return err
	}
	r.Revision = rev
	return nil
}
func running(state string) bool {
	return state == "fetching" || state == "verifying" || state == "publishing" || state == "cancel_requested"
}
func (m *Manager) Get(ctx context.Context, id string) (*Record, error) {
	r, err := m.read(ctx, id)
	if err != nil {
		return nil, err
	}
	if running(r.State) && time.Now().After(r.Deadline) {
		if r.State == "publishing" {
			p, e := (projectconfig.Manager{Store: m.store}).Get(ctx, r.ProjectID)
			if e == nil && p.ImportID == r.ID && p.Head == r.Commit && projectconfig.Verify(ctx, p, r.Commit) == nil {
				r.State = "ready"
				r.Message = "Imported project recovered; select its pinned commit for a task."
			} else if e != nil && !errors.Is(e, storage.ErrNotFound) {
				return nil, e
			}
		}
		if r.State != "ready" {
			r.State = "interrupted"
			r.Message = "Import stopped before completion. Partial data is retained; retry creates a separate attempt."
		}
		if err = m.save(ctx, r, "system"); err != nil {
			return nil, err
		}
	}
	if value, ok := m.progress.Load(id); ok {
		p := value.(progress)
		if p.attempt == r.Attempt {
			r.Bytes = p.bytes.Load()
		}
	}
	return r, nil
}
func (m *Manager) List(ctx context.Context) ([]*Record, error) {
	rows, err := m.store.ListSettings(ctx, prefix)
	if err != nil {
		return nil, err
	}
	result := []*Record{}
	for _, row := range rows {
		var r Record
		if err = json.Unmarshal(row.Value, &r); err != nil {
			return nil, err
		}
		record, err := m.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}
func (m *Manager) Start(ctx context.Context, in Input, actor string) (*Record, error) {
	if err := in.Authentication.validate(m.native); err != nil {
		return nil, fmt.Errorf("%w: select anonymous, an explicit HTTPS username/token, or native-only local authentication", ErrInvalid)
	}
	source, err := ValidateURL(in.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: use a public HTTPS clone URL without credentials, query, fragment or port", ErrInvalid)
	}
	if in.Ref == "" {
		in.Ref = "HEAD"
	}
	if !ValidateRef(in.Ref) {
		return nil, fmt.Errorf("%w: select HEAD, refs/heads/name, refs/tags/name or a full commit hash", ErrInvalid)
	}
	key := source + "\n" + in.Ref
	if in.Authentication.Mode != "" && in.Authentication.Mode != "anonymous" {
		key += "\nprivate"
	}
	sum := sha256.Sum256([]byte(key))
	id := "imp_" + hex.EncodeToString(sum[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("repository importer is stopping")
	}
	old, err := m.Get(ctx, id)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if old != nil {
		if old.State == "ready" {
			p, e := (projectconfig.Manager{Store: m.store}).Get(ctx, old.ProjectID)
			if e != nil {
				return nil, e
			}
			if e = projectconfig.Verify(ctx, p, old.Commit); e != nil {
				return nil, e
			}
			return old, nil
		}
		if running(old.State) || !in.Retry {
			return old, nil
		}
		if _, finishing := m.active[id]; finishing {
			return nil, fmt.Errorf("%w: the prior attempt is finishing; retry shortly", storage.ErrConflict)
		}
		if old.Revision != in.Revision {
			return nil, storage.ErrConflict
		}
	}
	if len(m.active) >= 2 {
		return nil, fmt.Errorf("%w: two imports are active; wait or cancel one before importing", storage.ErrConflict)
	}
	now := time.Now().UTC()
	r := &Record{ID: id, URL: source, Ref: in.Ref, State: "fetching", Message: "Fetching Git snapshot. No repository code is executed.", Attempt: randomID(), CreatedAt: now, Deadline: now.Add(maxDuration + 10*time.Second)}
	if old != nil {
		r.Revision = old.Revision
		r.CreatedAt = old.CreatedAt
	}
	r.Authentication = in.Authentication.Mode
	r.ProjectID = domain.NewID("prj")
	if err = m.save(ctx, r, actor); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return m.Get(ctx, id)
		}
		return nil, err
	}
	work, cancel := context.WithTimeout(m.ctx, maxDuration)
	m.active[id] = cancel
	m.wg.Add(1)
	snapshot := *r
	go m.run(work, cancel, &snapshot, actor, in.Authentication)
	return r, nil
}
func (m *Manager) Cancel(ctx context.Context, id string, revision int64, actor string) (*Record, error) {
	r, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.Revision != revision || r.State == "publishing" || !running(r.State) {
		return nil, storage.ErrConflict
	}
	r.State = "cancel_requested"
	r.Message = "Stopping import. Partial files and existing projects are retained."
	if err = m.save(ctx, r, actor); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if cancel := m.active[id]; cancel != nil {
		cancel()
	}
	m.mu.Unlock()
	return r, nil
}
func (m *Manager) run(ctx context.Context, cancel context.CancelFunc, r *Record, actor string, auth Authentication) {
	start := time.Now()
	outcome := "failed"
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.active, r.ID)
		m.mu.Unlock()
		m.wg.Done()
		observability.RepositoryImports.WithLabelValues(outcome).Inc()
		observability.RepositoryImportSeconds.Observe(time.Since(start).Seconds())
	}()
	_, span := observability.Start(ctx, "repository.import")
	defer span.End()
	var received atomic.Int64
	m.progress.Store(r.ID, progress{attempt: r.Attempt, bytes: &received})
	defer m.progress.Delete(r.ID)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := m.read(ctx, r.ID)
				if err != nil || current.Attempt != r.Attempt || current.State == "cancel_requested" {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-monitorDone }()
	finish := func(state, message string) {
		endCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		current, err := m.read(endCtx, r.ID)
		if err != nil {
			m.log.Error("repository import state unavailable", "import_id", r.ID)
			return
		}
		if current.Attempt != r.Attempt {
			return
		}
		if current.State == "cancel_requested" {
			state = "cancelled"
			message = "Import cancelled; partial files and existing projects are retained."
		}
		current.State = state
		current.Message = message
		current.Bytes = received.Load()
		if err = m.save(endCtx, current, actor); err != nil {
			m.log.Error("repository import state could not be persisted", "import_id", r.ID)
			return
		}
		outcome = state
	}
	dir := filepath.Join(m.root, r.Attempt)
	if err := os.Mkdir(dir, 0700); err != nil {
		finish("failed", "Cannot create a new managed directory; existing data was not replaced.")
		return
	}
	credential, err := auth.resolve(ctx, r.URL, dir)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	head, message := fetch(ctx, dir, r.URL, r.Ref, m.client, &received, credential)
	if message != "" {
		finish("failed", message)
		return
	}
	r.Bytes = received.Load()
	r.Commit = head
	r.State = "verifying"
	r.Message = "Verifying the imported commit before registration."
	if err := m.save(ctx, r, actor); err != nil {
		finish("failed", "Import state changed before verification; data is retained.")
		return
	}
	real, verified, err := projectconfig.Inspect(ctx, dir)
	if err != nil || verified != head {
		finish("failed", "Imported snapshot verification failed; data is retained.")
		return
	}
	if ctx.Err() != nil {
		finish("failed", "Import stopped before registration; data is retained.")
		return
	}
	r.State = "publishing"
	r.Message = "Registering the verified project; cancellation is no longer available."
	if err = m.save(ctx, r, actor); err != nil {
		finish("failed", "Import state changed before registration; data is retained.")
		return
	}
	p := &projectconfig.Project{Input: projectconfig.Input{Name: filepath.Base(r.URL), Path: real}, ID: r.ProjectID, Head: head, CreatedAt: time.Now().UTC(), ImportID: r.ID, RepositoryURL: r.URL, RepositoryRef: r.Ref}
	b, err := json.Marshal(p)
	if err != nil {
		finish("failed", "Project registration encoding failed.")
		return
	}
	endCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if err = m.store.AppendEvent(endCtx, domain.NewEvent("project.registration_requested", "", map[string]any{"projectId": p.ID, "importId": r.ID}).WithActor(actor)); err != nil {
		finish("failed", "Project registration audit failed; data is retained.")
		return
	}
	if _, err = m.store.PutSetting(endCtx, "projects/"+p.ID, b, 0, actor); err != nil {
		outcome = "uncertain"
		m.log.Error("repository project publication outcome is uncertain; recovery will inspect the persisted project", "import_id", r.ID)
		return
	}
	finish("ready", "Repository imported. Select the project and pinned commit for a task; agents have not started.")
}
