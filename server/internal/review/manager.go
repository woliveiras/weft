package review

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

var ErrForbidden = errors.New("review access denied")

var ErrInvalid = errors.New("invalid review request")
var ErrUnavailable = errors.New("preview is unavailable")
var ErrStale = errors.New("candidate changed; request a new review")

const prefix = "reviews/"

type Request struct {
	Title           string   `json:"title"`
	Instructions    string   `json:"instructions"`
	Command         []string `json:"command,omitempty"`
	Cwd             string   `json:"cwd,omitempty"`
	Port            int      `json:"port,omitempty"`
	ReadyPath       string   `json:"readyPath,omitempty"`
	LifetimeSeconds int      `json:"lifetimeSeconds,omitempty"`
	ExternalURL     string   `json:"externalUrl,omitempty"`
	Checks          []string `json:"checks,omitempty"`
}

func (r *Request) Validate() error {
	if len(strings.TrimSpace(r.Title)) == 0 || len(r.Title) > 200 || len(r.Instructions) > 16000 || len(r.Checks) > 50 {
		return fmt.Errorf("%w: title, instructions or checks", ErrInvalid)
	}
	if r.Cwd == "" {
		r.Cwd = "."
	}
	if !filepath.IsLocal(r.Cwd) {
		return fmt.Errorf("%w: cwd must stay within workspace", ErrInvalid)
	}
	if len(r.Command) > 64 {
		return fmt.Errorf("%w: too many arguments", ErrInvalid)
	}
	for _, a := range r.Command {
		if a == "" || len(a) > 8192 || strings.ContainsRune(a, 0) {
			return fmt.Errorf("%w: invalid command argument", ErrInvalid)
		}
	}
	if len(r.Command) > 0 && (r.Port < 1024 || r.Port > 65535) {
		return fmt.Errorf("%w: HTTP port must be between 1024 and 65535", ErrInvalid)
	}
	if r.ReadyPath == "" {
		r.ReadyPath = "/"
	}
	u, err := url.Parse(r.ReadyPath)
	if err != nil || !strings.HasPrefix(r.ReadyPath, "/") || strings.HasPrefix(r.ReadyPath, "//") || u.Host != "" || u.Fragment != "" {
		return fmt.Errorf("%w: readiness must be a local HTTP path", ErrInvalid)
	}
	if r.LifetimeSeconds == 0 {
		r.LifetimeSeconds = 1800
	}
	if r.LifetimeSeconds < 60 || r.LifetimeSeconds > 7200 {
		return fmt.Errorf("%w: preview lifetime must be 60–7200 seconds", ErrInvalid)
	}
	if r.ExternalURL != "" {
		u, e := url.Parse(r.ExternalURL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("%w: external URL must be HTTPS without credentials", ErrInvalid)
		}
	}
	return nil
}

type Credential struct {
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
	Actor   string    `json:"actor"`
	Browser bool      `json:"browser"`
	Used    bool      `json:"used,omitempty"`
}
type Record struct {
	RunID          string     `json:"runId,omitempty"`
	Policy         Policy     `json:"policy"`
	ID             string     `json:"id"`
	GraphID        string     `json:"graphId"`
	NodeID         string     `json:"nodeId"`
	Replica        int        `json:"replica"`
	ExecutionID    string     `json:"executionId"`
	Request        Request    `json:"request"`
	Candidate      string     `json:"candidate"`
	State          string     `json:"state"`
	Session        string     `json:"session"`
	Generation     int        `json:"generation"`
	CreatedAt      time.Time  `json:"createdAt"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	SessionExpires time.Time  `json:"sessionExpires"`
	DecidedAt      *time.Time `json:"decidedAt,omitempty"`
	DecidedBy      string     `json:"decidedBy,omitempty"`
	Method         string     `json:"method,omitempty"`
	Comment        string     `json:"comment,omitempty"`
	Error          string     `json:"error,omitempty"`
	Notified       bool       `json:"notified"`
	// Internal fields never returned by View.
	Source      string        `json:"source"`
	Directory   string        `json:"directory"`
	Graph       *domain.Graph `json:"graph"`
	Credentials []Credential  `json:"credentials,omitempty"`
	Revision    int64         `json:"-"`
}

func (r *Record) View() map[string]any {
	return map[string]any{"id": r.ID, "graphId": r.GraphID, "nodeId": r.NodeID, "replica": r.Replica, "request": r.Request, "candidate": r.Candidate, "state": r.State, "session": r.Session, "generation": r.Generation, "revision": r.Generation, "createdAt": r.CreatedAt, "expiresAt": r.ExpiresAt, "sessionExpires": r.SessionExpires, "decidedBy": r.DecidedBy, "decidedAt": r.DecidedAt, "method": r.Method, "comment": r.Comment, "error": r.Error}
}
func (r *Record) Name() string { return fmt.Sprintf("weft-preview-%s-%d", r.ID, r.Generation) }
func (r *Record) Work() string {
	return filepath.Join(r.Directory, fmt.Sprintf("run-%d", r.Generation))
}

type Driver interface {
	Start(context.Context, *Record) error
	Ready(context.Context, *Record) error
	Stop(context.Context, *Record) error
	Dial(context.Context, *Record) (net.Conn, error)
	Logs(context.Context, *Record) (string, error)
}
type Options struct {
	Store    storage.Store
	Bus      *events.Bus
	Root     string
	Instance string
	Driver   Driver
}
type Manager struct {
	Options
	active  sync.Map
	workers sync.WaitGroup
}

func New(o Options) *Manager { return &Manager{Options: o} }
func (m *Manager) event(ctx context.Context, r *Record, action string) {
	observability.ReviewTransitions.WithLabelValues(action).Inc()
	if m.Bus != nil {
		m.Bus.Publish(ctx, domain.NewEvent(domain.EventType("review."+action), r.GraphID, map[string]any{"id": r.ID, "state": r.State, "session": r.Session, "generation": r.Generation}).WithNode(r.NodeID, r.Replica).WithExecution(r.ExecutionID))
	}
}
func (m *Manager) save(ctx context.Context, r *Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	v, err := m.Store.PutSetting(ctx, prefix+r.ID, b, r.Revision, r.DecidedBy)
	if err == nil {
		r.Revision = v
	}
	return err
}
func (m *Manager) Get(ctx context.Context, id string) (*Record, error) {
	s, e := m.Store.GetSetting(ctx, prefix+id)
	if e != nil {
		return nil, e
	}
	var r Record
	if e = json.Unmarshal(s.Value, &r); e != nil {
		return nil, e
	}
	r.Revision = s.Revision
	return &r, nil
}
func (m *Manager) List(ctx context.Context) ([]*Record, error) {
	ss, e := m.Store.ListSettings(ctx, prefix)
	if e != nil {
		return nil, e
	}
	out := make([]*Record, 0, len(ss))
	for _, s := range ss {
		var r Record
		if e = json.Unmarshal(s.Value, &r); e != nil {
			return nil, e
		}
		r.Revision = s.Revision
		out = append(out, &r)
	}
	return out, nil
}
func (m *Manager) Pending(ctx context.Context, g, n string, replica int) (bool, error) {
	r, e := m.latest(ctx, g, n, replica)
	if e != nil {
		return false, e
	}
	return r != nil && (r.State == "pending" || !r.Notified), nil
}
func (m *Manager) Terminal(ctx context.Context, g, n string, replica int) (string, error) {
	r, e := m.latest(ctx, g, n, replica)
	if e != nil {
		return "", e
	}
	if r != nil && (r.State == "rejected" || r.State == "expired" || r.State == "cancelled") {
		return r.State, nil
	}
	return "", nil
}
func (m *Manager) latest(ctx context.Context, g, n string, replica int) (*Record, error) {
	if m == nil {
		return nil, nil
	}
	run, e := m.Store.GetGraphRecord(ctx, g)
	if e != nil && !errors.Is(e, storage.ErrNotFound) {
		return nil, e
	}
	rs, e := m.List(ctx)
	if e != nil {
		return nil, e
	}
	var latest *Record
	for _, r := range rs {
		if r.RunID != "" && run != nil && r.RunID != run.RunID {
			continue
		}
		if r.GraphID == g && r.NodeID == n && r.Replica == replica && (latest == nil || r.CreatedAt.After(latest.CreatedAt)) {
			latest = r
		}
	}
	return latest, nil
}
func (m *Manager) Create(ctx context.Context, g *domain.Graph, n string, replica int, execution, source string, req Request) (*Record, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if node := g.Node(n); node == nil || node.Isolation.Mode != domain.IsolationDocker {
		return nil, fmt.Errorf("%w: reviews currently require a Docker node", ErrInvalid)
	}
	sum := sha256.Sum256([]byte(execution))
	id := hex.EncodeToString(sum[:16])
	if prior, e := m.Get(ctx, id); e == nil {
		return prior, nil
	} else if !errors.Is(e, storage.ErrNotFound) {
		return nil, e
	}
	if pending, e := m.Pending(ctx, g.Metadata.ID, n, replica); e != nil {
		return nil, e
	} else if pending {
		return nil, storage.ErrConflict
	}
	dir := filepath.Join(m.Root, id)
	if e := os.MkdirAll(dir, 0755); e != nil {
		return nil, e
	}
	staging, e := os.MkdirTemp(dir, "capture-")
	if e != nil {
		return nil, e
	}
	candidate, e := Candidate(ctx, source, staging)
	if e != nil {
		return nil, fmt.Errorf("capture candidate: %w", e)
	}
	again, e := Fingerprint(ctx, source)
	if e != nil {
		return nil, e
	}
	if again != candidate {
		return nil, ErrStale
	}
	if e = os.Rename(staging, filepath.Join(dir, "candidate")); e != nil {
		existing, readErr := Fingerprint(ctx, filepath.Join(dir, "candidate"))
		if readErr != nil || existing != candidate {
			return nil, fmt.Errorf("persist candidate: %w", e)
		}
	}
	var policy Policy
	if raw, ok := g.Node(n).Config["reviewPolicy"]; ok {
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &policy); err != nil {
			return nil, fmt.Errorf("%w: reviewPolicy", ErrInvalid)
		}
		switch policy.RequiredMethod {
		case "", "managed", "external", "local", "without_execution":
		default:
			return nil, fmt.Errorf("%w: required review method", ErrInvalid)
		}
	}
	now := time.Now().UTC()
	r := &Record{Policy: policy, ID: id, GraphID: g.Metadata.ID, NodeID: n, Replica: replica, ExecutionID: execution, Request: req, Candidate: candidate, Source: source, Directory: dir, Graph: g, State: "pending", Session: "none", Generation: 1, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), SessionExpires: now.Add(time.Duration(req.LifetimeSeconds) * time.Second)}
	if len(req.Command) > 0 {
		r.Session = "queued"
	}
	if executionRecord, err := m.Store.GetExecution(ctx, execution); err == nil {
		r.RunID = executionRecord.RunID
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if e = m.save(ctx, r); e != nil {
		return nil, e
	}
	m.event(ctx, r, "requested")
	return r, nil
}
func (m *Manager) Decide(ctx context.Context, id, state, method, actor, comment string, revision int64) (*Record, error) {
	return m.decide(ctx, id, state, method, actor, comment, revision, 0)
}
func (m *Manager) decide(ctx context.Context, id, state, method, actor, comment string, revision int64, retries int) (*Record, error) {
	if state != "approved" && state != "changes_requested" && state != "rejected" {
		return nil, ErrInvalid
	}
	if method != "managed" && method != "external" && method != "local" && method != "without_execution" {
		return nil, ErrInvalid
	}
	r, e := m.Get(ctx, id)
	if e != nil {
		return nil, e
	}
	if r.State != "pending" || int64(r.Generation) != revision || time.Now().After(r.ExpiresAt) {
		return nil, storage.ErrConflict
	}
	if !r.Allows(actor) {
		return nil, ErrForbidden
	}
	if state == "approved" {
		if r.Policy.RequiredMethod != "" && method != r.Policy.RequiredMethod {
			return nil, ErrForbidden
		}
		if method == "managed" && (r.Session != "ready" || time.Now().After(r.ExpiresAt) || time.Now().After(r.SessionExpires)) {
			return nil, ErrUnavailable
		}
		if method == "external" && r.Request.ExternalURL == "" {
			return nil, ErrInvalid
		}
		hash, e := Fingerprint(ctx, r.Source)
		if e != nil {
			return nil, e
		}
		if hash != r.Candidate {
			return nil, ErrStale
		}
	}
	if len(comment) > 16000 {
		return nil, ErrInvalid
	}
	r.State = state
	r.Method = method
	r.DecidedBy = actor
	now := time.Now().UTC()
	r.DecidedAt = &now
	r.Comment = comment
	r.Credentials = nil
	if e = m.save(ctx, r); e != nil {
		if errors.Is(e, storage.ErrConflict) && retries < 3 {
			return m.decide(ctx, id, state, method, actor, comment, revision, retries+1)
		}
		return nil, e
	}
	m.event(ctx, r, "decided")
	return r, nil
}
func (m *Manager) Restart(ctx context.Context, id string) (*Record, error) {
	r, e := m.Get(ctx, id)
	if e != nil {
		return nil, e
	}
	if r.State != "pending" || time.Now().After(r.ExpiresAt) || !(r.Session == "expired" || r.Session == "stopped" || r.Session == "failed") {
		return nil, storage.ErrConflict
	}
	if e = m.stop(ctx, r); e != nil {
		return nil, e
	}
	r.Generation++
	r.Session = "queued"
	r.Error = ""
	r.Credentials = nil
	r.SessionExpires = time.Now().Add(time.Duration(r.Request.LifetimeSeconds) * time.Second)
	if r.SessionExpires.After(r.ExpiresAt) {
		r.SessionExpires = r.ExpiresAt
	}
	if e = m.save(ctx, r); e != nil {
		return nil, e
	}
	m.event(ctx, r, "restarted")
	return r, nil
}
func (m *Manager) Renew(ctx context.Context, id string) (*Record, error) {
	r, e := m.Get(ctx, id)
	if e != nil {
		return nil, e
	}
	if r.State != "pending" || r.Session != "ready" || time.Now().After(r.ExpiresAt) || time.Now().After(r.SessionExpires) {
		return nil, ErrUnavailable
	}
	r.SessionExpires = time.Now().Add(time.Duration(r.Request.LifetimeSeconds) * time.Second)
	if r.SessionExpires.After(r.ExpiresAt) {
		r.SessionExpires = r.ExpiresAt
	}
	if e = m.save(ctx, r); e != nil {
		return nil, e
	}
	return r, nil
}
func tokenHash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func (m *Manager) Issue(ctx context.Context, id, actor string, browser bool) (string, error) {
	r, e := m.Get(ctx, id)
	if e != nil {
		return "", e
	}
	if !r.Allows(actor) {
		return "", ErrForbidden
	}
	if r.State != "pending" || r.Session != "ready" || time.Now().After(r.ExpiresAt) || time.Now().After(r.SessionExpires) {
		return "", ErrUnavailable
	}
	var b [32]byte
	if _, e = rand.Read(b[:]); e != nil {
		return "", e
	}
	tok := hex.EncodeToString(b[:])
	now := time.Now()
	live := r.Credentials[:0]
	for _, c := range r.Credentials {
		if now.Before(c.Expires) && !c.Used {
			live = append(live, c)
		}
	}
	r.Credentials = live
	if len(live) >= 32 {
		return "", fmt.Errorf("too many active preview credentials")
	}
	ttl := 15 * time.Minute
	if browser {
		ttl = time.Minute
	}
	r.Credentials = append(r.Credentials, Credential{Hash: tokenHash(tok), Actor: actor, Browser: browser, Expires: now.Add(ttl)})
	if e = m.save(ctx, r); e != nil {
		return "", e
	}
	return tok, nil
}
func (m *Manager) Authorize(ctx context.Context, id, token string, exchange bool) (*Record, error) {
	r, e := m.Get(ctx, id)
	if e != nil {
		return nil, e
	}
	now := time.Now()
	if r.State != "pending" || r.Session != "ready" || now.After(r.ExpiresAt) || now.After(r.SessionExpires) {
		return nil, ErrUnavailable
	}
	hash := tokenHash(token)
	for i, c := range r.Credentials {
		if c.Hash == hash && !c.Used && now.Before(c.Expires) && c.Browser == exchange {
			if exchange {
				r.Credentials[i].Browser = false
				r.Credentials[i].Expires = now.Add(15 * time.Minute)
				if e = m.save(ctx, r); e != nil {
					return nil, e
				}
			}
			return r, nil
		}
	}
	return nil, errors.New("invalid preview credential")
}
func (m *Manager) Run(ctx context.Context) {
	defer m.workers.Wait()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		m.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
func (m *Manager) Tick(ctx context.Context) {
	rs, e := m.List(ctx)
	if e != nil {
		if ctx.Err() == nil {
			slog.Error("review reconciliation", "err", e)
		}
		return
	}
	for _, r := range rs {
		if r.State != "pending" && r.Notified && (r.Session == "stopped" || r.Session == "expired" || r.Session == "none") {
			continue
		}
		if _, busy := m.active.LoadOrStore(r.ID, true); busy {
			continue
		}
		m.workers.Add(1)
		go func(id string) {
			defer m.workers.Done()
			defer m.active.Delete(id)
			if e := m.reconcile(ctx, id); e != nil && !errors.Is(e, context.Canceled) && !errors.Is(e, storage.ErrConflict) {
				slog.Error("review reconciliation", "review", id, "err", e)
			}
		}(r.ID)
	}
}
func (m *Manager) reconcile(ctx context.Context, id string) error {
	ctx, span := observability.Start(ctx, "review.reconcile")
	defer span.End()
	holder := m.Instance + ":" + domain.NewID("lease")
	ok, e := m.Store.AcquireLease(ctx, "review/"+id, holder, 30*time.Second)
	if e != nil || !ok {
		return e
	}
	defer m.Store.ReleaseLease(context.WithoutCancel(ctx), "review/"+id, holder)
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-t.C:
				ok, e := m.Store.AcquireLease(work, "review/"+id, holder, 30*time.Second)
				if e != nil || !ok {
					cancel()
					return
				}
			}
		}
	}()
	r, e := m.Get(work, id)
	if e != nil {
		return e
	}
	if r.State == "pending" && r.RunID != "" {
		run, err := m.Store.GetGraphRecord(work, r.GraphID)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		var control struct {
			Stopped bool `json:"stopped"`
		}
		raw, controlErr := m.Store.KVGet(work, "_control/"+r.GraphID, "node/"+r.NodeID)
		if controlErr != nil && !errors.Is(controlErr, storage.ErrNotFound) {
			return controlErr
		}
		if controlErr == nil {
			if e := json.Unmarshal(raw, &control); e != nil {
				return e
			}
		}
		if control.Stopped || errors.Is(err, storage.ErrNotFound) || run.RunID != r.RunID || run.DesiredState == domain.GraphStopped {
			r.State, r.Notified, r.Credentials = "cancelled", true, nil
			if err := m.save(work, r); err != nil {
				return err
			}
			m.event(work, r, "cancelled")
		}
	}
	now := time.Now()
	if r.Session == "failed" {
		// Retain failed state for diagnosis/restart; retry cleanup on every tick.
		if err := m.stop(work, r); err != nil {
			return err
		}
	}
	if r.State == "pending" && now.After(r.ExpiresAt) {
		r.State = "expired"
		r.Credentials = nil
		if e = m.save(work, r); e != nil {
			return e
		}
		m.event(work, r, "expired")
	}
	if r.State != "pending" || now.After(r.SessionExpires) {
		if r.Session != "none" && r.Session != "stopped" && r.Session != "expired" {
			if e = m.stop(work, r); e != nil {
				return e
			}
			r.Session = "stopped"
			if r.State == "pending" {
				r.Session = "expired"
			}
			r.Credentials = nil
			if e = m.save(work, r); e != nil {
				return e
			}
			m.event(work, r, "session_stopped")
		}
		if r.State != "pending" && !r.Notified {
			return m.notify(work, r)
		}
		return nil
	}
	if r.Session == "queued" || r.Session == "starting" {
		r.Session = "starting"
		if e = m.save(work, r); e != nil {
			return e
		}
		start, c := context.WithTimeout(work, 2*time.Minute)
		e = m.Driver.Start(start, r)
		c()
		current, loadErr := m.Get(work, id)
		if loadErr != nil {
			return loadErr
		}
		if current.State != "pending" || current.Generation != r.Generation {
			return nil
		}
		r = current
		if e != nil {
			r.Session = "failed"
			r.Error = e.Error()
		} else {
			r.Session = "ready"
			r.Error = ""
		}
		if e = m.save(work, r); e != nil {
			return e
		}
		m.event(work, r, "session")
		if r.Session == "failed" {
			return m.stop(work, r)
		}
		return nil
	}
	if r.Session == "ready" {
		probe, c := context.WithTimeout(work, 5*time.Second)
		e = m.Driver.Ready(probe, r)
		c()
		if e != nil {
			r.Session = "failed"
			r.Error = "Application is no longer ready"
			r.Credentials = nil
			if e = m.save(work, r); e != nil {
				return e
			}
			m.event(work, r, "session_failed")
			return m.stop(work, r)
		}
	}
	return nil
}
func (m *Manager) notify(ctx context.Context, r *Record) error {
	if r.State == "rejected" || r.State == "expired" || r.State == "cancelled" {
		r.Notified = true
		return m.save(ctx, r)
	}
	// The decision itself is the outbox entry. Insert-once preserves a consumed
	// message if a crash occurs between insertion and marking this entry delivered.
	if r.State == "approved" {
		hash, e := Fingerprint(ctx, r.Source)
		if e != nil {
			return e
		}
		if hash != r.Candidate {
			r.State = "superseded"
			r.Error = ErrStale.Error()
			if e = m.save(ctx, r); e != nil {
				return e
			}
		}
	}
	payload, _ := json.Marshal(map[string]any{"reviewId": r.ID, "state": r.State, "candidate": r.Candidate, "method": r.Method, "comment": r.Comment})
	msg := &domain.OperatorMessage{ID: "review-" + r.ID, GraphID: r.GraphID, NodeID: r.NodeID, Replica: r.Replica, Text: "Human review result: " + string(payload), CreatedAt: time.Now().UTC()}
	if e := m.Store.PutOperatorMessageOnce(ctx, msg); e != nil {
		return e
	}
	r.Notified = true
	if e := m.save(ctx, r); e != nil {
		return e
	}
	m.event(ctx, r, "continuation_queued")
	return nil
}

// Policy comes only from the operator-saved node configuration, never tool args.
type Policy struct {
	Reviewers      []string `json:"reviewers,omitempty"`
	RequiredMethod string   `json:"requiredMethod,omitempty"`
}

func (r *Record) Allows(actor string) bool {
	if len(r.Policy.Reviewers) == 0 {
		return true
	}
	for _, name := range r.Policy.Reviewers {
		if name == actor {
			return true
		}
	}
	return false
}

func (m *Manager) Cancel(ctx context.Context, graph, node, actor string) error {
	if m == nil {
		return nil
	}
	rs, e := m.List(ctx)
	if e != nil {
		return e
	}
	for _, r := range rs {
		if r.GraphID != graph || (node != "" && r.NodeID != node) || r.State != "pending" {
			continue
		}
		r.State = "cancelled"
		r.DecidedBy = actor
		now := time.Now().UTC()
		r.DecidedAt = &now
		r.Notified = true
		r.Credentials = nil
		if e = m.save(ctx, r); e != nil {
			return e
		}
		m.event(ctx, r, "cancelled")
	}
	return nil
}

// AssertApproved prevents a runtime from treating rejected or stale work as a
// completed result. A request-changes continuation may edit and request another
// review, but cannot publish the candidate as approved merely by ignoring feedback.
func (m *Manager) AssertApproved(ctx context.Context, graph, node string, replica int) error {
	latest, e := m.latest(ctx, graph, node, replica)
	if e != nil {
		return e
	}
	if latest == nil {
		return nil
	}
	if latest.State != "approved" {
		return fmt.Errorf("human review %s is %s; request a new review before completing", latest.ID, latest.State)
	}
	hash, e := Fingerprint(ctx, latest.Source)
	if e != nil {
		return e
	}
	if hash != latest.Candidate {
		return ErrStale
	}
	return nil
}

func (m *Manager) RevokeAccess(ctx context.Context, id string) (*Record, error) {
	r, e := m.Get(ctx, id)
	if e != nil {
		return nil, e
	}
	r.Credentials = nil
	if e = m.save(ctx, r); e != nil {
		return nil, e
	}
	m.event(ctx, r, "access_revoked")
	return r, nil
}

// A failed cleanup stays visible and retains the reservation for reconciliation.
func (m *Manager) stop(ctx context.Context, r *Record) error {
	err := m.Driver.Stop(ctx, r)
	if err == nil {
		return nil
	}
	detail := "Cleanup pending: " + err.Error()
	if r.Error != detail {
		r.Error = detail
		if saveErr := m.save(ctx, r); saveErr != nil {
			return errors.Join(err, saveErr)
		}
	}
	return err
}
