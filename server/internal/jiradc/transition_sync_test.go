package jiradc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func intent() TransitionIntent {
	return TransitionIntent{OperationID: "task-7:pr-delivered", Connection: "jira-team", ConnectionRevision: 1, IssueKey: "APP-7", IssueID: "10007", IssueTypeID: "10002", FromStatusID: "10", TransitionID: "31", TargetStatusID: "20"}
}
func TestTransitionIntentRequiresScopedIDsAndRejectsSameStatusActions(t *testing.T) {
	if err := intent().validate(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*TransitionIntent){func(v *TransitionIntent) { v.OperationID = "../task" }, func(v *TransitionIntent) { v.ConnectionRevision = 0 }, func(v *TransitionIntent) { v.TransitionID = "Review" }, func(v *TransitionIntent) { v.IssueID = "APP-7" }, func(v *TransitionIntent) { v.IssueKey = "../APP-7" }, func(v *TransitionIntent) { v.IssueTypeID = "" }, func(v *TransitionIntent) { v.TargetStatusID = v.FromStatusID }} {
		v := intent()
		edit(&v)
		if !errors.Is(v.validate(), ErrInvalid) {
			t.Fatal(v)
		}
	}
}

type syncFixture struct {
	beforeMetadata func()
	issueReads     atomic.Int32
	root           string
	syncer         TransitionSync
	status         atomic.Int32
	posts          atomic.Int32
	mode           atomic.Int32
	afterPost      func()
	entered        chan struct{}
	release        chan struct{}
}

func transitionFixture(t *testing.T) *syncFixture {
	t.Helper()
	root := t.TempDir()
	st, err := fsstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secrets.NewBox(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	m := Connections{Store: st, Box: box}
	f := &syncFixture{root: root, syncer: TransitionSync{Connections: m}}
	f.status.Store(10)
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sync-fixture-token" {
			t.Error("unexpected native credential")
		}
		switch r.URL.Path {
		case "/jira/rest/api/2/issue/APP-7":
			reads := f.issueReads.Add(1)
			if r.Method != "GET" {
				t.Error("issue was modified")
			}
			issue := strings.Replace(strings.Replace(fixtureIssue, `"summary":`, `"updated":"2026-11-20T12:00:00.000+0000","summary":`, 1), `"status":{"id":"10"`, fmt.Sprintf(`"status":{"id":"%d"`, f.status.Load()), 1)
			if f.mode.Load() == 9 {
				issue = strings.Replace(issue, `"updated":"2026-11-20T12:00:00.000+0000",`, "", 1)
			}
			if f.mode.Load() == 10 {
				issue = strings.Replace(issue, "2026-11-20T12:00:00.000+0000", "invalid", 1)
			}
			if f.mode.Load() == 11 && reads >= 4 {
				issue = strings.Replace(issue, `"status":{"id":"10"`, `"status":{"id":"11"`, 1)
			}
			if f.mode.Load() == 5 {
				issue = strings.Replace(issue, `"issuetype":{"id":"10002"}`, `"issuetype":{"id":"10003"}`, 1)
			}
			fmt.Fprint(w, issue)
		case "/jira/rest/api/2/issue/10007/transitions":
			if r.Method == "GET" {
				if f.beforeMetadata != nil {
					f.beforeMetadata()
				}
				fields := `{}`
				if f.mode.Load() == 7 {
					fmt.Fprint(w, `{"transitions":[]}`)
					return
				}
				if f.mode.Load() == 8 {
					fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Review","to":{"id":"21","name":"Review"},"fields":{}}]}`)
					return
				}
				if f.mode.Load() == 3 {
					fields = `null`
				}
				if f.mode.Load() == 4 {
					fields = `{"resolution":{"required":true,"name":"Resolution","schema":{"type":"resolution"},"hasDefaultValue":true}}`
				}
				fmt.Fprintf(w, `{"transitions":[{"id":"31","name":"Revisão","to":{"id":"20","name":"Review"},"fields":%s}]}`, fields)
				return
			}
			if r.Method != "POST" {
				t.Error("unexpected mutation")
			}
			var payload map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload) != 1 || string(payload["transition"]) != `{"id":"31"}` {
				t.Error("transition sent arbitrary fields")
			}
			f.posts.Add(1)
			if f.entered != nil {
				close(f.entered)
				<-f.release
			}
			if f.mode.Load() != 2 {
				f.status.Store(20)
			}
			if f.afterPost != nil {
				f.afterPost()
			}
			if f.mode.Load() == 1 || f.mode.Load() == 2 {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(204)
		default:
			t.Error("unscoped request", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(remote.Close)
	old := http.DefaultTransport
	http.DefaultTransport = remote.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })
	token := "sync-fixture-token"
	_, err = m.Put(t.Context(), ConnectionInput{Name: "jira-team", Kind: "jira-dc", SiteURL: remote.URL + "/jira", ProjectID: "10001", Enabled: true, ProjectRestricted: true, Token: &token}, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func TestTransitionSyncPersistsObservedTargetAndNeverRepeatsCompletedOperation(t *testing.T) {
	f := transitionFixture(t)
	r, err := f.syncer.Apply(t.Context(), intent(), "system:test")
	if err != nil || r.State != "target_observed" || r.ObservedStatusID != "20" || f.posts.Load() != 1 {
		t.Fatal(r, err, f.posts.Load())
	}
	// Historical success does not claim this remains the current Jira status.
	f.status.Store(10)
	r, err = f.syncer.Apply(t.Context(), intent(), "system:test")
	if err != nil || r.State != "target_observed" || f.posts.Load() != 1 {
		t.Fatal("replayed completed operation", err)
	}
	changed := intent()
	changed.TargetStatusID = "21"
	if _, err = f.syncer.Apply(t.Context(), changed, "system:test"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("same operation rebound", err)
	}
}
func TestTransitionSyncNoOpAndPreflightFailuresAreDurableWithoutWrites(t *testing.T) {
	for _, mode := range []int32{0, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := transitionFixture(t)
			f.mode.Store(mode)
			if mode == 0 {
				f.status.Store(20)
			}
			if mode == 6 {
				f.status.Store(11)
			}
			r, err := f.syncer.Apply(t.Context(), intent(), "system:test")
			if f.posts.Load() != 0 {
				t.Fatal("preflight failure wrote")
			}
			if mode == 0 {
				if err != nil || r.State != "already_target" {
					t.Fatal(r, err)
				}
			} else {
				if err == nil || r.State != "failed" {
					t.Fatal(r, err)
				}
			}
			saved, getErr := f.syncer.Get(t.Context(), intent().OperationID)
			if getErr != nil || saved.State != r.State {
				t.Fatal(saved, getErr)
			}
			// Fixing remote metadata alone never grants a replay of a retained failure.
			f.mode.Store(0)
			f.status.Store(10)
			_, _ = f.syncer.Apply(t.Context(), intent(), "system:test")
			if f.posts.Load() != 0 {
				t.Fatal("failure blindly retried")
			}
		})
	}
}
func TestTransitionSyncReconcilesLostResponseAfterStorageRestartWithoutReposting(t *testing.T) {
	f := transitionFixture(t)
	f.mode.Store(1)
	r, err := f.syncer.Apply(t.Context(), intent(), "system:test")
	if err == nil || r.State != "unknown" || f.posts.Load() != 1 {
		t.Fatal(r, err)
	}
	// Reopen the real filesystem store with the original encrypted connection and
	// journal. The remote HTTP fixture retains only its simulated Jira state.
	if err = f.syncer.Connections.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := fsstore.Open(f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	next := TransitionSync{Connections: Connections{Store: reopened, Box: f.syncer.Connections.Box}}
	r, err = next.Apply(t.Context(), intent(), "system:recovery")
	if err != nil || r.State != "target_observed" || f.posts.Load() != 1 {
		t.Fatal(r, err)
	}
}
func TestTransitionSyncUnknownOutcomeAtOtherStatusNeverRetriesPOST(t *testing.T) {
	f := transitionFixture(t)
	f.mode.Store(2)
	r, err := f.syncer.Apply(t.Context(), intent(), "system:test")
	if err == nil || r.State != "unknown" {
		t.Fatal(r, err)
	}
	f.mode.Store(0)
	for range 2 {
		r, err = f.syncer.Apply(t.Context(), intent(), "system:test")
		if !errors.Is(err, ErrSyncUnconfirmed) || r.State != "unknown" || f.posts.Load() != 1 {
			t.Fatal(r, err)
		}
	}
}

type syncFaultStore struct {
	storage.Store
	fail          atomic.Bool
	beforeReserve func()
	afterReserve  func()
}

func (s *syncFaultStore) PutSettingIf(ctx context.Context, key string, value []byte, revision int64, actor string, guards []storage.SettingRevision) (int64, error) {
	if strings.HasPrefix(key, SyncPrefix) && s.fail.Load() {
		return 0, errors.New("fixture storage unavailable")
	}
	if strings.HasPrefix(key, SyncPrefix) && revision == 0 && s.beforeReserve != nil {
		s.beforeReserve()
	}
	r, err := s.Store.PutSettingIf(ctx, key, value, revision, actor, guards)
	if err == nil && strings.HasPrefix(key, SyncPrefix) && revision == 0 && s.afterReserve != nil {
		s.afterReserve()
	}
	return r, err
}
func TestTransitionSyncStorageFailuresDoNotLoseUncertainIntentOrDispatchWithoutReservation(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			f := transitionFixture(t)
			fault := &syncFaultStore{Store: f.syncer.Connections.Store}
			f.syncer.Connections.Store = fault
			if after {
				f.afterPost = func() { fault.fail.Store(true) }
			} else {
				fault.fail.Store(true)
			}
			if _, err := f.syncer.Apply(t.Context(), intent(), "system:test"); err == nil {
				t.Fatal("storage failure ignored")
			}
			if !after && f.posts.Load() != 0 {
				t.Fatal("dispatch without journal")
			}
			if after {
				saved, err := f.syncer.Get(t.Context(), intent().OperationID)
				if err != nil || saved.State != "pending" || f.posts.Load() != 1 {
					t.Fatal(saved, err)
				}
				fault.fail.Store(false)
				saved, err = f.syncer.Apply(t.Context(), intent(), "system:recovery")
				if err != nil || saved.State != "target_observed" || f.posts.Load() != 1 {
					t.Fatal(saved, err)
				}
			}
		})
	}
}
func TestTransitionSyncConcurrentReconciliationCannotCauseDuplicateDispatch(t *testing.T) {
	f := transitionFixture(t)
	f.entered = make(chan struct{})
	f.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := f.syncer.Apply(t.Context(), intent(), "system:first"); done <- err }()
	<-f.entered
	r, err := f.syncer.Apply(t.Context(), intent(), "system:second")
	if !errors.Is(err, ErrSyncUnconfirmed) || r.State != "unknown" {
		t.Fatal(r, err)
	}
	close(f.release)
	<-done
	r, err = f.syncer.Apply(t.Context(), intent(), "system:recovery")
	if err != nil || r.State != "target_observed" || f.posts.Load() != 1 {
		t.Fatal(r, err, f.posts.Load())
	}
}
func TestTransitionSyncConnectionChangeBlocksReplayAndRetainsOriginalScope(t *testing.T) {
	f := transitionFixture(t)
	f.mode.Store(2)
	_, _ = f.syncer.Apply(t.Context(), intent(), "system:test")
	c, err := f.syncer.Connections.Get(t.Context(), "jira-team")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.syncer.Connections.Put(t.Context(), ConnectionInput{Name: c.Name, Kind: c.Kind, SiteURL: c.SiteURL, ProjectID: c.ProjectID, Enabled: false, ProjectRestricted: true}, c.Revision, "admin")
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.syncer.Apply(t.Context(), intent(), "system:recovery")
	if !errors.Is(err, storage.ErrConflict) || r.State != "unknown" || r.Intent.ConnectionRevision != 1 || f.posts.Load() != 1 {
		t.Fatal(r, err)
	}
}

func TestTransitionSyncConcurrentFreshReservationsDispatchOnce(t *testing.T) {
	f := transitionFixture(t)
	reached := make(chan struct{}, 2)
	release := make(chan struct{})
	fault := &syncFaultStore{Store: f.syncer.Connections.Store, beforeReserve: func() { reached <- struct{}{}; <-release }}
	f.syncer.Connections.Store = fault
	done := make(chan error, 2)
	for range 2 {
		go func() { _, err := f.syncer.Apply(t.Context(), intent(), "system:race"); done <- err }()
	}
	<-reached
	<-reached
	close(release)
	a, b := <-done, <-done
	if (a == nil) == (b == nil) || (!errors.Is(a, storage.ErrConflict) && !errors.Is(b, storage.ErrConflict)) {
		t.Fatal("reservation CAS did not select one worker", a, b)
	}
	r, err := f.syncer.Get(t.Context(), intent().OperationID)
	if err != nil || r.State != "target_observed" || f.posts.Load() != 1 {
		t.Fatal(r, err, f.posts.Load())
	}
}
func TestTransitionSyncRevocationAfterJournalPreventsDispatch(t *testing.T) {
	f := transitionFixture(t)
	fault := &syncFaultStore{Store: f.syncer.Connections.Store, afterReserve: func() {
		c, err := f.syncer.Connections.Get(t.Context(), "jira-team")
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.syncer.Connections.Put(t.Context(), ConnectionInput{Name: c.Name, Kind: c.Kind, SiteURL: c.SiteURL, ProjectID: c.ProjectID, Enabled: false, ProjectRestricted: true}, c.Revision, "admin")
		if err != nil {
			t.Fatal(err)
		}
	}}
	f.syncer.Connections.Store = fault
	_, err := f.syncer.Apply(t.Context(), intent(), "system:test")
	if err == nil || f.posts.Load() != 0 {
		t.Fatal("revoked connection dispatched", err)
	}
	r, getErr := f.syncer.Get(t.Context(), intent().OperationID)
	if getErr != nil || r.State != "pending" {
		t.Fatal("original intent lost", r, getErr)
	}
}
func TestTransitionSyncDoesNotRepeatAWriteAfterAHumanMovesTheIssueBack(t *testing.T) {
	f := transitionFixture(t)
	f.mode.Store(1)
	_, err := f.syncer.Apply(t.Context(), intent(), "system:test")
	if err == nil || f.status.Load() != 20 {
		t.Fatal("fixture did not apply before response loss")
	}
	f.status.Store(10)
	f.mode.Store(0)
	r, err := f.syncer.Apply(t.Context(), intent(), "system:reconcile")
	if !errors.Is(err, ErrSyncUnconfirmed) || r.State != "unknown" || f.posts.Load() != 1 {
		t.Fatal(r, err, f.posts.Load())
	}
}
