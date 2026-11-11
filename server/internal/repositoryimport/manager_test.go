package repositoryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/repositoryimport/importtest"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func TestURLAndReferenceValidation(t *testing.T) {
	for _, raw := range []string{"file:///tmp/repo", "ssh://git@example.com/repo", "http://github.com/owner/repo", "https://user:secret@github.com/owner/repo", "https://github.com/owner/repo?token=secret", "https://localhost/repo", "https://127.0.0.1/repo", "https://192.168.1.2/repo", "https://example.com:443/repo", "https://example.com/../repo", "https://example.com/%2e%2e/repo", "https://example.com/repo#secret", "https://example.com/repo?", "https://github.com/owner/repo/tree/main"} {
		if _, err := ValidateURL(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	normalized, err := ValidateURL("https://GitHub.com/owner/repo/")
	if err != nil || normalized != "https://github.com/owner/repo.git" {
		t.Fatal(normalized, err)
	}
	for _, ref := range []string{"--upload-pack=evil", "main", "refs/heads/a..b", "refs/heads/a.lock", "refs/heads/x/", "refs/heads/a:b", "HEAD~1"} {
		if ValidateRef(ref) {
			t.Fatal(ref)
		}
	}
	for _, ref := range []string{"HEAD", "refs/heads/main", "refs/tags/v1.0", "0123456789012345678901234567890123456789"} {
		if !ValidateRef(ref) {
			t.Fatal(ref)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "100.64.0.1", "192.0.2.1", "2001:db8::1", "64:ff9b::7f00:1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatal(ip)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address denied")
	}
}
func wait(t *testing.T, m *Manager, id string) *Record {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := m.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !running(r.State) {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("import did not finish")
	return nil
}
func setup(t *testing.T) (*Manager, *importtest.Fixture, storage.Store) {
	t.Helper()
	root := t.TempDir()
	s, err := sqlstore.OpenSQLite(t.Context(), filepath.Join(root, "db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture, err := importtest.New(t.Context(), filepath.Join(root, "remote"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Store: s, Root: filepath.Join(root, "imports"), Client: fixture.Client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(context.Background()); err != nil {
			t.Error(err)
		}
		fixture.Server.Close()
		s.Close()
	})
	return m, fixture, s
}
func TestImportReuseConcurrentRequestsAndWorktreeIsolation(t *testing.T) {
	m, f, s := setup(t)
	var wg sync.WaitGroup
	results := make(chan *Record, 5)
	errs := make(chan error, 5)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := m.Start(t.Context(), Input{URL: importtest.URL, Ref: "refs/heads/main"}, "admin")
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id string
	for r := range results {
		if id != "" && id != r.ID {
			t.Fatal("duplicate imports")
		}
		id = r.ID
	}
	r := wait(t, m, id)
	if r.State != "ready" || r.Commit != f.Commit || r.Bytes == 0 {
		t.Fatal(r)
	}
	ps, err := (projectconfig.Manager{Store: s}).List(t.Context())
	if err != nil || len(ps) != 1 {
		t.Fatal(ps, err)
	}
	p := ps[0]
	if p.ImportID != r.ID || p.RepositoryURL != importtest.URL {
		t.Fatal(p)
	}
	again, err := m.Start(t.Context(), Input{URL: importtest.URL, Ref: r.Ref}, "admin")
	if err != nil || again.Attempt != r.Attempt {
		t.Fatal(again, err)
	}
	taskroot := filepath.Join(t.TempDir(), "tasks")
	if err = os.Mkdir(taskroot, 0700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"task-run-one", "task-run-two"} {
		path, err := projectconfig.CreateWorktree(t.Context(), p, r.Commit, taskroot, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(path, "README.md"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(filepath.Join(f.Root, "README.md"))
	if err != nil || string(content) != "Public import fixture\n" {
		t.Fatal("source modified", err)
	}
	content, _ = os.ReadFile(filepath.Join(taskroot, "task-run-one", "README.md"))
	if string(content) != "task-run-one" {
		t.Fatal("task workspaces overlapped")
	}
}
func TestFailedRefRetryPreservesAttemptAndCAS(t *testing.T) {
	m, _, s := setup(t)
	r, err := m.Start(t.Context(), Input{URL: importtest.URL, Ref: "refs/heads/missing"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	failed := wait(t, m, r.ID)
	if failed.State != "failed" {
		t.Fatal(failed)
	}
	if _, err = m.Start(t.Context(), Input{URL: r.URL, Ref: r.Ref, Retry: true, Revision: 1}, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal(err)
	}
	retry, err := m.Start(t.Context(), Input{URL: r.URL, Ref: r.Ref, Retry: true, Revision: failed.Revision}, "admin")
	if err != nil || retry.Attempt == failed.Attempt {
		t.Fatal(retry, err)
	}
	wait(t, m, retry.ID)
	if _, err = os.Stat(filepath.Join(m.root, failed.Attempt, ".git")); err != nil {
		t.Fatal("partial data removed", err)
	}
	ps, err := (projectconfig.Manager{Store: s}).List(t.Context())
	if err != nil || len(ps) != 0 {
		t.Fatal("failed import registered", ps, err)
	}
}

func TestBridgeWithRealGit(t *testing.T) {
	_, f, _ := setup(t)
	var received atomic.Int64
	endpoint, close, _, err := bridge(t.Context(), importtest.URL, f.Client, maxTransfer, &received)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"fetch", "--no-tags", "--depth=1", "--no-recurse-submodules", "--", endpoint, "refs/heads/main"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture Git: %v: %s", err, out)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCancelAndRetryAfterManagerRestart(t *testing.T) {
	m, f, s := setup(t)
	entered := make(chan struct{})
	var once sync.Once
	m.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	r, err := m.Start(t.Context(), Input{URL: importtest.URL}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not start")
	}
	if _, err = m.Cancel(t.Context(), r.ID, r.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	cancelled := wait(t, m, r.ID)
	if cancelled.State != "cancelled" {
		t.Fatal(cancelled)
	}
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	next, err := New(Options{Store: s, Root: m.root, Client: f.Client})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close(context.Background())
	resumed, err := next.Start(t.Context(), Input{URL: importtest.URL, Retry: true, Revision: cancelled.Revision}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ready := wait(t, next, resumed.ID)
	if ready.State != "ready" || ready.Attempt == cancelled.Attempt {
		t.Fatal(ready)
	}
	if _, err = os.Stat(filepath.Join(m.root, cancelled.Attempt, ".git")); err != nil {
		t.Fatal("cancelled data deleted", err)
	}
}
func TestExpiredAttemptRecoveryAndExistingDirectoryPreservation(t *testing.T) {
	m, _, _ := setup(t)
	sum := sha256.Sum256([]byte("expired"))
	id := "imp_" + hex.EncodeToString(sum[:])
	record := &Record{ID: id, Attempt: "attempt_existing", State: "fetching", Deadline: time.Now().Add(-time.Second)}
	if err := m.save(t.Context(), record, "fixture"); err != nil {
		t.Fatal(err)
	}
	recovered, err := m.Get(t.Context(), id)
	if err != nil || recovered.State != "interrupted" {
		t.Fatal(recovered, err)
	}
	// A destination collision must fail instead of resetting any existing tree.
	dir := filepath.Join(m.root, "attempt_collision")
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "keep")
	if err = os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &Record{ID: id, Attempt: "attempt_collision", State: "fetching", Revision: recovered.Revision, Deadline: time.Now().Add(time.Minute)}
	if err = m.save(t.Context(), r, "fixture"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	m.wg.Add(1)
	m.run(ctx, cancel, r, "fixture", Authentication{})
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "keep" {
		t.Fatal("existing files replaced", err)
	}
}

func TestImportConcurrencyCapacityAndAuditFailure(t *testing.T) {
	m, _, s := setup(t)
	m.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	for _, ref := range []string{"HEAD", "refs/heads/main"} {
		if _, err := m.Start(t.Context(), Input{URL: importtest.URL, Ref: ref}, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Start(t.Context(), Input{URL: importtest.URL, Ref: "refs/tags/extra"}, "admin"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("unbounded concurrency", err)
	}
	other, err := New(Options{Store: rejectAudit{Store: s}, Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(context.Background())
	if _, err = other.Start(t.Context(), Input{URL: importtest.URL, Ref: "refs/tags/blocked"}, "admin"); err == nil {
		t.Fatal("audit failure ignored")
	}
	entries, err := os.ReadDir(other.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("started after audit failure", err)
	}
}

type rejectAudit struct{ storage.Store }

func (rejectAudit) AppendEvent(context.Context, *domain.Event) error {
	return errors.New("audit unavailable")
}

type uncertainPublication struct {
	storage.Store
	once atomic.Bool
}

func (s *uncertainPublication) PutSetting(ctx context.Context, key string, value []byte, revision int64, actor string) (int64, error) {
	rev, err := s.Store.PutSetting(ctx, key, value, revision, actor)
	if err == nil && strings.HasPrefix(key, "projects/") && !s.once.Swap(true) {
		return 0, errors.New("lost write acknowledgement")
	}
	return rev, err
}
func TestRecoversUncertainPublicationWithoutDuplicatingProject(t *testing.T) {
	m, _, s := setup(t)
	m.store = &uncertainPublication{Store: s}
	r, err := m.Start(t.Context(), Input{URL: importtest.URL}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, e := m.read(t.Context(), r.ID)
		if e != nil {
			t.Fatal(e)
		}
		r = record
		ps, e := (projectconfig.Manager{Store: s}).List(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		if len(ps) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.State != "publishing" {
		t.Fatal("publication should remain uncertain", r)
	}
	r.Deadline = time.Now().Add(-time.Second)
	if err = m.save(t.Context(), r, "fixture"); err != nil {
		t.Fatal(err)
	}
	recovered, err := m.Get(t.Context(), r.ID)
	if err != nil || recovered.State != "ready" {
		t.Fatal(recovered, err)
	}
	ps, err := (projectconfig.Manager{Store: s}).List(t.Context())
	if err != nil || len(ps) != 1 {
		t.Fatal("duplicate projects", err, ps)
	}
}
