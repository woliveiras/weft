package review

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Request)
	}{
		{"empty title", func(r *Request) { r.Title = "" }}, {"escape", func(r *Request) { r.Cwd = "../outside" }}, {"privileged port", func(r *Request) { r.Port = 80 }}, {"external probe", func(r *Request) { r.ReadyPath = "//metadata/x" }}, {"unbounded lifetime", func(r *Request) { r.LifetimeSeconds = 99999 }}, {"URL credentials", func(r *Request) { r.ExternalURL = "https://secret@example.com" }}, {"command NUL", func(r *Request) { r.Command = []string{"x\x00"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Request{Title: "Review", Command: []string{"node", "app.js"}, Port: 5173}
			tc.change(&r)
			if !errors.Is(r.Validate(), ErrInvalid) {
				t.Fatal("accepted invalid request")
			}
		})
	}
	r := Request{Title: "Review", Command: []string{"node", "app.js"}, Port: 5173}
	if e := r.Validate(); e != nil {
		t.Fatal(e)
	}
	if r.Cwd != "." || r.ReadyPath != "/" || r.LifetimeSeconds != 1800 {
		t.Fatal(r)
	}
}
func TestCandidate(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	dest := filepath.Join(t.TempDir(), "snapshot")
	if e := os.WriteFile(filepath.Join(source, "app.js"), []byte("version one"), 0644); e != nil {
		t.Fatal(e)
	}
	hash, e := Candidate(ctx, source, dest)
	if e != nil {
		t.Fatal(e)
	}
	if again, e := Fingerprint(ctx, dest); e != nil || hash != again {
		t.Fatal(again, e)
	}
	if e = os.WriteFile(filepath.Join(source, "app.js"), []byte("version two"), 0644); e != nil {
		t.Fatal(e)
	}
	again, _ := Fingerprint(ctx, source)
	if hash == again {
		t.Fatal("changes not detected")
	}
	b, _ := os.ReadFile(filepath.Join(dest, "app.js"))
	if string(b) != "version one" {
		t.Fatal("candidate changed")
	}
	if e = os.Symlink("../outside", filepath.Join(source, "escape")); e != nil {
		t.Fatal(e)
	}
	if _, e = Fingerprint(ctx, source); e == nil {
		t.Fatal("escaping symlink accepted")
	}
}

func TestFingerprintDetectsNewReplicaScratch(t *testing.T) {
	source := t.TempDir()
	candidate, err := Candidate(t.Context(), source, filepath.Join(t.TempDir(), "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace.Scratch(source, "sink", 0), 0o755); err != nil {
		t.Fatal(err)
	}
	current, err := Fingerprint(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if candidate == current {
		t.Fatal("creating replica scratch did not change the candidate")
	}
}

type fakeDriver struct {
	startErr, readyErr, stopErr error
	stops                       int
}

func (d *fakeDriver) Start(context.Context, *Record) error { return d.startErr }
func (d *fakeDriver) Ready(context.Context, *Record) error { return d.readyErr }
func (d *fakeDriver) Stop(context.Context, *Record) error  { d.stops++; return d.stopErr }
func (d *fakeDriver) Dial(context.Context, *Record) (net.Conn, error) {
	return nil, errors.New("unused")
}
func (d *fakeDriver) Logs(context.Context, *Record) (string, error) { return "", nil }
func fixture(t *testing.T) (*Manager, *Record) {
	t.Helper()
	s, e := fsstore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	m := New(Options{Store: s, Root: t.TempDir(), Instance: "test", Driver: &fakeDriver{}})
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "g"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "n", Isolation: domain.IsolationSpec{Mode: domain.IsolationDocker}}}}}
	source := t.TempDir()
	if e = os.WriteFile(filepath.Join(source, "app"), []byte("candidate"), 0644); e != nil {
		t.Fatal(e)
	}
	r, e := m.Create(context.Background(), g, "n", 0, "execution", source, Request{Title: "Review", Command: []string{"node", "app"}, Port: 5173})
	if e != nil {
		t.Fatal(e)
	}
	return m, r
}
func TestReviewStorageIntegration(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.Session != "ready" {
		t.Fatal(r.Session)
	}
	tok, e := m.Issue(ctx, r.ID, "operator", false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Authorize(ctx, r.ID, tok, false); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if _, e = m.Decide(ctx, r.ID, "approved", "managed", "operator", "works", int64(r.Generation)); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Decide(ctx, r.ID, "approved", "managed", "operator", "", int64(r.Generation)); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("second decision", e)
	}
	if _, e = m.Authorize(ctx, r.ID, tok, false); e == nil {
		t.Fatal("revoked credential accepted")
	}
	// Recreate the controller against the same durable store before delivering.
	resumed := New(m.Options)
	if e = resumed.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	messages, e := m.Store.ConsumeOperatorMessages(ctx, "g", "n", 0)
	if e != nil || len(messages) != 1 {
		t.Fatal(messages, e)
	}
	// Simulate a crash after insert/consumption but before outbox acknowledgement.
	r, _ = m.Get(ctx, r.ID)
	r.Notified = false
	if e = m.save(ctx, r); e != nil {
		t.Fatal(e)
	}
	if e = resumed.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	messages, e = m.Store.ConsumeOperatorMessages(ctx, "g", "n", 0)
	if e != nil || len(messages) != 0 {
		t.Fatal("duplicate continuation", messages, e)
	}
}
func TestStaleCandidateAndSessionExpiry(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	r.SessionExpires = time.Now().Add(-time.Second)
	if e := m.save(ctx, r); e != nil {
		t.Fatal(e)
	}
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.State != "pending" || r.Session != "expired" {
		t.Fatal(r.State, r.Session)
	}
	if _, e := m.Restart(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(r.Source, "app"), []byte("changed"), 0644); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if _, e := m.Decide(ctx, r.ID, "approved", "managed", "operator", "", int64(r.Generation)); !errors.Is(e, ErrStale) {
		t.Fatal(e)
	}
}
func TestDependencyFailureAndCleanupRetention(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	driver := m.Driver.(*fakeDriver)
	driver.startErr = errors.New("no capacity")
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.Session != "failed" || r.Error == "" {
		t.Fatal(r)
	}
	_, e := m.Decide(ctx, r.ID, "rejected", "without_execution", "operator", "", int64(r.Generation))
	if e != nil {
		t.Fatal(e)
	}
	driver.stopErr = errors.New("daemon offline")
	if e = m.reconcile(ctx, r.ID); e == nil {
		t.Fatal("cleanup failure hidden")
	}
	r, _ = m.Get(ctx, r.ID)
	if r.Session == "stopped" || r.Notified {
		t.Fatal("released before confirmed cleanup")
	}
}
func TestBrowserExchangeOnce(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	token, e := m.Issue(ctx, r.ID, "reviewer", true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Authorize(ctx, r.ID, token, false); e == nil {
		t.Fatal("unexchanged token accepted")
	}
	if _, e = m.Authorize(ctx, r.ID, token, true); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Authorize(ctx, r.ID, token, true); e == nil {
		t.Fatal("exchange replayed")
	}
	if _, e = m.Authorize(ctx, r.ID, token, false); e != nil {
		t.Fatal(e)
	}
}

func TestRequiredMethodAndReviewers(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	r.Policy = Policy{Reviewers: []string{"alice"}, RequiredMethod: "managed"}
	if e := m.save(ctx, r); e != nil {
		t.Fatal(e)
	}
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Issue(ctx, r.ID, "bob", false); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if _, e := m.Decide(ctx, r.ID, "approved", "without_execution", "alice", "", int64(r.Generation)); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := m.Decide(ctx, r.ID, "approved", "managed", "alice", "", int64(r.Generation)); e != nil {
		t.Fatal(e)
	}
}
func TestCancellationRevokesAndStops(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	token, e := m.Issue(ctx, r.ID, "operator", false)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cancel(ctx, "g", "n", "operator"); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Authorize(ctx, r.ID, token, false); e == nil {
		t.Fatal("cancelled preview accepted")
	}
	if e = m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.State != "cancelled" || r.Session != "stopped" {
		t.Fatal(r)
	}
	messages, e := m.Store.ConsumeOperatorMessages(ctx, "g", "n", 0)
	if e != nil || len(messages) != 0 {
		t.Fatal("cancelled task resumed", messages, e)
	}
}

func TestReviewCannotBeBypassedByCompletion(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if e := m.AssertApproved(ctx, "g", "n", 0); e == nil {
		t.Fatal("pending completed")
	}
	if _, e := m.Decide(ctx, r.ID, "changes_requested", "without_execution", "operator", "fix it", int64(r.Generation)); e != nil {
		t.Fatal(e)
	}
	if e := m.AssertApproved(ctx, "g", "n", 0); e == nil {
		t.Fatal("request changes completed")
	}
	if e := m.AssertApproved(ctx, "g", "other-node", 0); e != nil {
		t.Fatal("unrelated node blocked", e)
	}
}
func TestChangesAfterApprovalInvalidateCompletion(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if _, e := m.Decide(ctx, r.ID, "approved", "without_execution", "operator", "", int64(r.Generation)); e != nil {
		t.Fatal(e)
	}
	if e := m.AssertApproved(ctx, "g", "n", 0); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(r.Source, "app"), []byte("new code"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := m.AssertApproved(ctx, "g", "n", 0); !errors.Is(e, ErrStale) {
		t.Fatal(e)
	}
}

func TestConcurrentDecisionHasOneWinner(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	results := make(chan error, 2)
	for _, state := range []string{"approved", "rejected"} {
		go func(state string) {
			_, e := m.Decide(ctx, r.ID, state, "without_execution", "operator", "", int64(r.Generation))
			results <- e
		}(state)
	}
	wins := 0
	for range 2 {
		e := <-results
		if e == nil {
			wins++
		} else if !errors.Is(e, storage.ErrConflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatal("decision winners", wins)
	}
}
func TestOldPreviewGenerationCannotBeApproved(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	r.Session = "expired"
	if e := m.save(ctx, r); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Restart(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Decide(ctx, r.ID, "approved", "without_execution", "operator", "", 1); !errors.Is(e, storage.ErrConflict) {
		t.Fatal(e)
	}
}

func TestControlConfigReplace(t *testing.T) {
	dir := t.TempDir()
	if err := WriteControl(dir, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := WriteControl(dir, []byte("second")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || string(body) != "second" {
		t.Fatal(string(body), err)
	}
}
func TestReviewCancelsWhenRunChanges(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	r.RunID = "old-run"
	if err := m.save(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.PutGraphRecord(ctx, &domain.GraphRecord{ID: r.GraphID, RunID: "new-run", DesiredState: domain.GraphRunning}); err != nil {
		t.Fatal(err)
	}
	if err := m.reconcile(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.State != "cancelled" || r.Session != "stopped" {
		t.Fatal(r)
	}
}
func TestFailedPreviewReleasesCompute(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	d := m.Driver.(*fakeDriver)
	d.startErr = errors.New("readiness failed")
	if err := m.reconcile(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = m.Get(ctx, r.ID)
	if r.Session != "failed" || d.stops != 1 {
		t.Fatal(r.Session, d.stops)
	}
}

func TestCandidatePreservesDirectoryModes(t *testing.T) {
	source := t.TempDir()
	if e := os.Chmod(source, 0700); e != nil {
		t.Fatal(e)
	}
	destination := filepath.Join(t.TempDir(), "candidate")
	hash, err := Candidate(context.Background(), source, destination)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := Fingerprint(context.Background(), destination)
	if err != nil || hash != copied {
		t.Fatal("snapshot identity differs", hash, copied, err)
	}
}

func TestExternalAndLocalReviewMethods(t *testing.T) {
	for _, method := range []string{"external", "local", "without_execution"} {
		t.Run(method, func(t *testing.T) {
			ctx := context.Background()
			m, r := fixture(t)
			r.Session = "none"
			r.Request.Command = nil
			r.Request.ExternalURL = "https://ci.example.invalid/build/123"
			if e := m.save(ctx, r); e != nil {
				t.Fatal(e)
			}
			if _, e := m.Decide(ctx, r.ID, "approved", method, "operator", "checked candidate", int64(r.Generation)); e != nil {
				t.Fatal(e)
			}
			if e := m.reconcile(ctx, r.ID); e != nil {
				t.Fatal(e)
			}
			r, _ = m.Get(ctx, r.ID)
			if !r.Notified || r.Method != method || r.DecidedAt == nil {
				t.Fatal(r)
			}
		})
	}
}
func TestCredentialsAreScopedToOneReview(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if e := m.reconcile(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	token, e := m.Issue(ctx, r.ID, "operator", false)
	if e != nil {
		t.Fatal(e)
	}
	other := *r
	other.ID = "00000000000000000000000000000000"
	other.Revision = 0
	if e = m.save(ctx, &other); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Authorize(ctx, other.ID, token, false); e == nil {
		t.Fatal("cross-session access accepted")
	}
}

func TestReviewDeadlineDeniesAccessBeforeReconciliation(t *testing.T) {
	ctx := context.Background()
	m, r := fixture(t)
	if err := m.reconcile(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	token, err := m.Issue(ctx, r.ID, "operator", false)
	if err != nil {
		t.Fatal(err)
	}
	r, _ = m.Get(ctx, r.ID)
	r.ExpiresAt = time.Now().Add(-time.Second)
	if err := m.save(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authorize(ctx, r.ID, token, false); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := m.Issue(ctx, r.ID, "operator", false); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := m.Renew(ctx, r.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
