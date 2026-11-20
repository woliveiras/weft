package reviewroute

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

type fixture struct {
	store   storage.Store
	manager Manager
	input   Input
	calls   atomic.Int64
	open    func() storage.Store
}

func newFixture(t *testing.T, backend, provider string) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{}
	f.open = func() storage.Store {
		var s storage.Store
		var err error
		if backend == "fs" {
			s, err = fsstore.Open(dir)
		} else {
			var sql *sqlstore.Store
			sql, err = sqlstore.OpenSQLite(t.Context(), filepath.Join(dir, "review.db"))
			if err == nil {
				err = sql.Migrate(t.Context())
			}
			s = sql
		}
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	f.store = f.open()
	t.Cleanup(func() {
		if err := f.store.Close(); err != nil {
			t.Error(err)
		}
	})
	box, err := secrets.NewBox([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	f.manager = Manager{Store: f.store, SCM: scm.Manager{Store: f.store, Box: box}, Validate: func(context.Context, Route) error { return nil }}
	f.manager.Dispatch = func(ctx context.Context, r Route, v Receipt) (string, error) {
		f.calls.Add(1)
		stored, err := f.manager.GetReceipt(ctx, v.ID)
		if err != nil {
			return "", err
		}
		if stored.State != "dispatching" || stored.ExpectedRunID != "run-reviewed" || stored.MessageID != "review-route-"+v.ID {
			return "", errors.New("dispatch did not receive its durable reservation")
		}
		return v.ExpectedRunID, nil
	}
	token := "synthetic-route-token"
	in := scm.Input{Name: "team", Kind: provider, Repository: "team/app", Token: &token}
	if provider == "bitbucket-dc" {
		in.APIURL = "https://bitbucket.example/context"
	}
	c, err := f.manager.SCM.Save(t.Context(), in, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "review"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reviewer", Runtime: domain.RuntimeRef{Kind: domain.RuntimeEcho}}}, Channels: []domain.Channel{{ID: "requests", To: []string{"reviewer"}, Types: []string{MessageType}}}}}
	if _, err = f.store.SaveGraph(t.Context(), g, 0); err != nil {
		t.Fatal(err)
	}
	if err = f.store.PutGraphRecord(t.Context(), &domain.GraphRecord{ID: "review", Version: 1, RunID: "run-reviewed", State: domain.GraphRunning, DesiredState: domain.GraphRunning, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	url, err := connectionInstance(c.Connection)
	if err != nil {
		t.Fatal(err)
	}
	f.input = validInput()
	f.input.Provider = provider
	f.input.Instance = url
	f.input.ConnectionRevision = c.UpdatedAt.UTC().Format(time.RFC3339Nano)
	return f
}
func (f *fixture) event(t *testing.T, number int) *domain.Event {
	t.Helper()
	p := pull()
	p.ConnectionRevision = f.input.ConnectionRevision
	p.Provider = f.input.Provider
	p.Instance = f.input.Instance
	p.Number = number
	raw, err := json.Marshal(struct {
		PullRequest
		Ready bool `json:"reviewRevisionAvailable"`
	}{p, true})
	if err != nil {
		t.Fatal(err)
	}
	ev := domain.NewEvent(domain.EvPullRequestOpened, "task-source", json.RawMessage(raw)).WithExecution("source-execution")
	if err = f.store.AppendEvent(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	return ev
}
func (f *fixture) put(t *testing.T) Route {
	t.Helper()
	r, err := f.manager.Put(t.Context(), f.input, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *fixture) receipts(t *testing.T) []Receipt {
	t.Helper()
	rows, _, err := f.manager.ReceiptsPage(t.Context(), "", 200)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store = f.open()
	f.manager.Store = f.store
	f.manager.SCM.Store = f.store
}

func TestPreviewHasNoWritesAndNoImplicitBacklogAcrossProviders(t *testing.T) {
	for _, backend := range []string{"fs", "sqlite"} {
		for _, provider := range []string{"github", "bitbucket-cloud", "bitbucket-dc"} {
			t.Run(backend+"/"+provider, func(t *testing.T) {
				f := newFixture(t, backend, provider)
				f.event(t, 1)
				settings, _ := f.store.ListSettings(t.Context(), "")
				events, _ := f.store.ListEvents(t.Context(), storage.EventFilter{Limit: 200})
				preview, err := f.manager.Preview(t.Context(), f.input)
				if err != nil || preview.ActivationSeq != 0 {
					t.Fatal(preview, err)
				}
				afterSettings, _ := f.store.ListSettings(t.Context(), "")
				afterEvents, _ := f.store.ListEvents(t.Context(), storage.EventFilter{Limit: 200})
				if len(settings) != len(afterSettings) || len(events) != len(afterEvents) || f.calls.Load() != 0 {
					t.Fatal("preview mutated state")
				}
				r := f.put(t)
				if r.ActivationSeq <= events[0].Seq {
					t.Fatal("activation did not fence old facts")
				}
				if err = f.manager.Tick(t.Context()); err != nil {
					t.Fatal(err)
				}
				if len(f.receipts(t)) != 0 {
					t.Fatal("route consumed implicit backlog")
				}
				f.event(t, 7)
				f.event(t, 7)
				other := f.input
				other.Name = "another"
				if _, err = f.manager.Put(t.Context(), other, 0, "admin"); err != nil {
					t.Fatal(err)
				}
				f.event(t, 7)
				if err = f.manager.Tick(t.Context()); err != nil {
					t.Fatal(err)
				}
				rows := f.receipts(t)
				if len(rows) != 1 || rows[0].State != "published" || f.calls.Load() != 1 || rows[0].DispatchRunID != "run-reviewed" {
					t.Fatal(rows, f.calls.Load())
				}
				f.reopen(t)
				if err = f.manager.Tick(t.Context()); err != nil {
					t.Fatal(err)
				}
				if f.calls.Load() != 1 {
					t.Fatal("restart repeated a published review")
				}
			})
		}
	}
}

func TestRouteCASDisableReenableAndPrerequisites(t *testing.T) {
	for _, backend := range []string{"fs", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			f := newFixture(t, backend, "github")
			r := f.put(t)
			f.event(t, 7)
			if err := f.manager.scanRoutes(t.Context()); err != nil {
				t.Fatal(err)
			}
			changed := f.input
			changed.Enabled = false
			if _, err := f.manager.Put(t.Context(), changed, r.Revision+1, "admin"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal(err)
			}
			r, err := f.manager.Put(t.Context(), changed, r.Revision, "admin")
			if err != nil {
				t.Fatal(err)
			}
			if err = f.manager.Tick(t.Context()); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("disabled reservation was not refused", err)
			}
			if f.calls.Load() != 0 || f.receipts(t)[0].State != "failed" {
				t.Fatal("disabled route dispatched")
			}
			f.event(t, 8)
			if _, err = f.manager.Put(t.Context(), f.input, r.Revision, "admin"); err != nil {
				t.Fatal(err)
			}
			if err = f.manager.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(f.receipts(t)) != 1 {
				t.Fatal("reenable consumed old backlog")
			}
			f.event(t, 9)
			f.manager.Validate = func(context.Context, Route) error { return errors.New("grant revoked") }
			if err = f.manager.Tick(t.Context()); err == nil {
				t.Fatal("revoked grants accepted")
			}
			if f.calls.Load() != 0 {
				t.Fatal("revoked grant dispatched")
			}
		})
	}
}
func TestConnectionRotationAndRunStateRefuseDispatch(t *testing.T) {
	for _, mode := range []string{"connection", "paused", "version", "run-missing"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "sqlite", "github")
			f.put(t)
			f.event(t, 7)
			if err := f.manager.scanRoutes(t.Context()); err != nil {
				t.Fatal(err)
			}
			if mode == "connection" {
				if _, err := f.manager.SCM.Save(t.Context(), scm.Input{Name: "team", Kind: "github", Repository: "team/app"}, "admin", true); err != nil {
					t.Fatal(err)
				}
			} else {
				r, err := f.store.GetGraphRecord(t.Context(), "review")
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "paused":
					r.State = domain.GraphPaused
				case "version":
					r.Version = 2
				case "run-missing":
					r.RunID = ""
				}
				if err = f.store.PutGraphRecord(t.Context(), r); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.manager.Tick(t.Context()); err == nil {
				t.Fatal("changed prerequisite accepted")
			}
			if f.calls.Load() != 0 || f.receipts(t)[0].State != "failed" {
				t.Fatal("changed authority dispatched")
			}
		})
	}
}

type publishedFailureStore struct{ storage.Store }

func (s publishedFailureStore) PutSettingIf(ctx context.Context, key string, raw []byte, expected int64, actor string, guards []storage.SettingRevision) (int64, error) {
	var v Receipt
	if strings.HasPrefix(key, ReceiptPrefix) && json.Unmarshal(raw, &v) == nil && v.State == "published" {
		return 0, errors.New("synthetic post-dispatch storage failure")
	}
	return s.Store.PutSettingIf(ctx, key, raw, expected, actor, guards)
}
func TestUnknownAndInterruptedDispatchNeverReplayAfterRestart(t *testing.T) {
	for _, backend := range []string{"fs", "sqlite"} {
		for _, mode := range []string{"callback-error", "wrong-run", "final-store"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				f := newFixture(t, backend, "github")
				f.put(t)
				f.event(t, 7)
				if mode == "callback-error" {
					f.manager.Dispatch = func(context.Context, Route, Receipt) (string, error) {
						f.calls.Add(1)
						return "", errors.New("lost dispatch response")
					}
				}
				if mode == "wrong-run" {
					f.manager.Dispatch = func(context.Context, Route, Receipt) (string, error) { f.calls.Add(1); return "another-run", nil }
				}
				if mode == "final-store" {
					f.manager.Store = publishedFailureStore{Store: f.store}
				}
				if err := f.manager.Tick(t.Context()); err == nil {
					t.Fatal("uncertain dispatch reported success")
				}
				if f.calls.Load() != 1 {
					t.Fatal("callback count", f.calls.Load())
				}
				f.reopen(t)
				_ = f.manager.Tick(t.Context())
				rows := f.receipts(t)
				if len(rows) != 1 || rows[0].State != "unknown" || f.calls.Load() != 1 {
					t.Fatal(rows, f.calls.Load())
				}
			})
		}
	}
}
func TestConcurrentWorkersReserveOneGlobalReview(t *testing.T) {
	f := newFixture(t, "sqlite", "github")
	f.put(t)
	f.event(t, 7)
	entered, release := make(chan struct{}), make(chan struct{})
	f.manager.Dispatch = func(_ context.Context, _ Route, v Receipt) (string, error) {
		f.calls.Add(1)
		close(entered)
		<-release
		return v.ExpectedRunID, nil
	}
	done := make(chan error, 1)
	go func() { done <- f.manager.Tick(t.Context()) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first worker did not dispatch")
	}
	if err := f.manager.Tick(t.Context()); err != nil && !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("concurrent dispatch")
	}
	close(release)
	if err := <-done; err != nil && !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	if f.receipts(t)[0].State != "published" {
		t.Fatal(f.receipts(t))
	}
}

type boundedStore struct {
	storage.Store
	eventCalls int
	pageCalls  int
}

func (s *boundedStore) ListEvents(ctx context.Context, f storage.EventFilter) ([]*domain.Event, error) {
	if f.Limit > 20 {
		return nil, errors.New("unbounded event read")
	}
	s.eventCalls++
	return s.Store.ListEvents(ctx, f)
}
func (s *boundedStore) ListSettingsPage(ctx context.Context, prefix, after string, limit int) ([]*storage.Setting, error) {
	if limit > 20 {
		return nil, errors.New("unbounded settings read")
	}
	s.pageCalls++
	return s.Store.ListSettingsPage(ctx, prefix, after, limit)
}
func TestBoundedCursorsReachPendingWorkAcrossPages(t *testing.T) {
	f := newFixture(t, "sqlite", "github")
	f.put(t)
	for n := 1; n <= 45; n++ {
		f.event(t, n)
	}
	b := &boundedStore{Store: f.store}
	f.manager.Store = b
	for n := 0; n < 12; n++ {
		if err := f.manager.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls.Load() != 45 || b.eventCalls < 3 || b.pageCalls < 12 {
		t.Fatal(f.calls.Load(), b.eventCalls, b.pageCalls)
	}
}

type reservationFaultStore struct {
	storage.Store
	after func(context.Context) error
}

func (s reservationFaultStore) PutSettingIf(ctx context.Context, key string, raw []byte, expected int64, actor string, guards []storage.SettingRevision) (int64, error) {
	rev, err := s.Store.PutSettingIf(ctx, key, raw, expected, actor, guards)
	var v Receipt
	if err == nil && strings.HasPrefix(key, ReceiptPrefix) && json.Unmarshal(raw, &v) == nil && v.State == "dispatching" {
		if err = s.after(ctx); err != nil {
			return 0, err
		}
	}
	return rev, err
}
func TestReservedDispatchCannotReplayWhenInterruptedBeforeCallback(t *testing.T) {
	for _, backend := range []string{"fs", "sqlite"} {
		for _, mode := range []string{"crash", "run-changed"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				f := newFixture(t, backend, "github")
				f.put(t)
				f.event(t, 7)
				f.manager.Store = reservationFaultStore{Store: f.store, after: func(ctx context.Context) error {
					if mode == "crash" {
						return errors.New("simulated loss after journal commit")
					}
					r, err := f.store.GetGraphRecord(ctx, "review")
					if err != nil {
						return err
					}
					r.RunID = "new-run"
					return f.store.PutGraphRecord(ctx, r)
				}}
				if err := f.manager.Tick(t.Context()); err == nil {
					t.Fatal("interrupted reservation reported success")
				}
				if f.calls.Load() != 0 {
					t.Fatal("callback crossed interrupted reservation")
				}
				f.reopen(t)
				_ = f.manager.Tick(t.Context())
				if f.calls.Load() != 0 || f.receipts(t)[0].State != "unknown" {
					t.Fatal("restart replayed uncertain reservation", f.receipts(t))
				}
			})
		}
	}
}

type activationFaultStore struct {
	storage.Store
	audit bool
}

func (s activationFaultStore) AppendEvent(ctx context.Context, ev *domain.Event) error {
	if s.audit && ev.Type == ChangeRequested {
		return errors.New("synthetic activation audit failure")
	}
	return s.Store.AppendEvent(ctx, ev)
}
func (s activationFaultStore) PutSettingIf(ctx context.Context, key string, raw []byte, expected int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if !s.audit && strings.HasPrefix(key, RoutePrefix) {
		return 0, errors.New("synthetic route CAS failure")
	}
	return s.Store.PutSettingIf(ctx, key, raw, expected, actor, guards)
}
func TestActivationFailureNeverCreatesAnActiveRoute(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(map[bool]string{false: "CAS", true: "audit"}[audit], func(t *testing.T) {
			f := newFixture(t, "sqlite", "github")
			f.manager.Store = activationFaultStore{Store: f.store, audit: audit}
			if _, err := f.manager.Put(t.Context(), f.input, 0, "admin"); err == nil {
				t.Fatal("activation failure ignored")
			}
			if _, err := f.manager.Get(t.Context(), f.input.Name); !errors.Is(err, storage.ErrNotFound) {
				t.Fatal("failed activation created route", err)
			}
			f.event(t, 7)
			f.manager.Store = f.store
			if err := f.manager.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(f.receipts(t)) != 0 || f.calls.Load() != 0 {
				t.Fatal("audit-only request activated routing")
			}
		})
	}
}

func TestPreviewRejectsNonIngressAndMissingDestinationWithoutWrites(t *testing.T) {
	f := newFixture(t, "sqlite", "github")
	for _, edit := range []func(*Input){func(v *Input) { v.NodeID = "missing" }, func(v *Input) { v.ChannelID = "missing" }, func(v *Input) { v.GraphVersion = 99 }, func(v *Input) { v.ConnectionRevision = "2026-11-21T12:00:00Z" }} {
		in := f.input
		edit(&in)
		if _, err := f.manager.Preview(t.Context(), in); err == nil {
			t.Fatal(in)
		}
	}
	g, err := f.store.GetGraph(t.Context(), "review", 0)
	if err != nil {
		t.Fatal(err)
	}
	g.Spec.Channels[0].From = "reviewer"
	if _, err = f.store.SaveGraph(t.Context(), g, 1); err != nil {
		t.Fatal(err)
	}
	in := f.input
	in.GraphVersion = 2
	if _, err = f.manager.Preview(t.Context(), in); !errors.Is(err, ErrInvalid) {
		t.Fatal("non-ingress channel accepted", err)
	}
	rows, _, err := f.manager.ListPage(t.Context(), "", 10)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

func TestDeliveryOrderingUsesDurableSequenceInsteadOfWallClock(t *testing.T) {
	f := newFixture(t, "sqlite", "github")
	f.put(t)
	p := pull()
	p.ConnectionRevision = f.input.ConnectionRevision
	raw, _ := json.Marshal(struct {
		PullRequest
		Ready bool `json:"reviewRevisionAvailable"`
	}{p, true})
	ev := domain.NewEvent(domain.EvPullRequestOpened, "task-source", json.RawMessage(raw))
	ev.Time = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := f.store.AppendEvent(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 || f.receipts(t)[0].EventSeq != ev.Seq {
		t.Fatal("wall clock suppressed a fact after activation")
	}
}
