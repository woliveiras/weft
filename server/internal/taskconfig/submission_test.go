package taskconfig

import (
	"errors"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSubmissionKeyValidation(t *testing.T) {
	for _, key := range []string{"", "short", "../../unsafe-key-value", "has spaces in key"} {
		if submissionKey.MatchString(key) {
			t.Fatal(key)
		}
	}
	if !submissionKey.MatchString("fixture-submission-key") {
		t.Fatal("valid key")
	}
}
func TestSubmissionPersistenceAndConcurrentReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := sqlstore.OpenSQLite(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	m := Manager{Store: s}
	in := PromptInput{Request: "Explain the repository"}
	var fresh atomic.Int32
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := m.SaveSubmission(t.Context(), in, "fixture-submission-key", "admin")
			if err != nil && !errors.Is(err, storage.ErrConflict) {
				t.Error(err)
			}
			if created {
				fresh.Add(1)
			}
		}()
	}
	wg.Wait()
	if fresh.Load() != 1 {
		t.Fatal("duplicate submission", fresh.Load())
	}
	d, created, e := m.SaveSubmission(t.Context(), in, "fixture-submission-key", "admin")
	if e != nil || created || !d.Submitted {
		t.Fatal(d, created, e)
	}
	if _, _, e = m.SaveSubmission(t.Context(), PromptInput{Request: "Different request"}, "fixture-submission-key", "admin"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("key reused for different input", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = sqlstore.OpenSQLite(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m.Store = s
	replay, created, e := m.SaveSubmission(t.Context(), in, "fixture-submission-key", "admin")
	if e != nil || created || replay.ID != d.ID {
		t.Fatal("restart replay", e)
	}
	page, e := m.List(t.Context(), "", 100)
	if e != nil || len(page.Items) != 1 {
		t.Fatal("duplicate drafts", e)
	}
	// A failed initial save leaves its reservation uncertain rather than retrying effects.
	bad := PromptInput{Request: ""}
	if _, _, e = m.SaveSubmission(t.Context(), bad, "failed-submission-key", "admin"); e == nil {
		t.Fatal("invalid accepted")
	}
	if _, _, e = m.SaveSubmission(t.Context(), bad, "failed-submission-key", "admin"); !errors.Is(e, storage.ErrConflict) {
		t.Fatal("uncertain reservation replayed", e)
	}
}
