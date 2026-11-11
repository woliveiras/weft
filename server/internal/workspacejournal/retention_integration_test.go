package workspacejournal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/storage"
)

type pausedCursorRead struct {
	storage.Store
	key     string
	read    chan struct{}
	proceed chan struct{}
}

func (s *pausedCursorRead) GetSetting(ctx context.Context, key string) (*storage.Setting, error) {
	setting, err := s.Store.GetSetting(ctx, key)
	if key == s.key && s.read != nil {
		close(s.read)
		s.read = nil
		select {
		case <-s.proceed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return setting, err
}

func TestImportRetriesAfterConcurrentPrune(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, store)
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	appendLargeObservation(t, w, "first")
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	appendLargeObservation(t, w, "second")
	appendLargeObservation(t, w, "third")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	read, proceed := make(chan struct{}), make(chan struct{})
	paused := journalTestImporter(path, &pausedCursorRead{
		Store: store, key: cursorKey(path), read: read, proceed: proceed,
	})
	result := make(chan error, 1)
	finished := make(chan struct{})
	importCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(finished)
		result <- paused.Import(importCtx)
	}()
	defer func() {
		cancel()
		<-finished
	}()
	select {
	case <-read:
	case err := <-result:
		t.Fatalf("import finished before reading its cursor: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	wantCursor, wantRevision := readTestCursor(t, store)
	plan, err := Prune(ctx, store, path, 0, true)
	if err != nil || len(plan.Archives) != 2 {
		t.Fatalf("concurrent prune: %+v %v", plan, err)
	}
	reopened, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Append(Record{ID: "after-prune", Path: "file", Operation: "created"}); err != nil {
		t.Fatal(err)
	}
	active := inspectActive(t, path)
	if active.ID != wantCursor.JournalID || active.Bytes <= wantCursor.Offset {
		t.Fatal("fixture must append a new record to the retained cursor segment")
	}
	close(proceed)
	if err := <-result; err != nil {
		t.Fatalf("import after concurrent prune: %v", err)
	}
	if saved, revision := readTestCursor(t, store); saved.JournalID != active.ID || saved.Offset != active.Bytes || revision != wantRevision+1 {
		t.Fatalf("retry did not advance exactly one record: %+v at revision %d", saved, revision)
	}
	checkEventOrder(t, store, "first", "second", "third", "after-prune")
}

func TestPruneKeepsNewestArchivesAndResumesAcrossRotations(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, store)
	appendLargeObservation(t, w, "one")
	for _, id := range []string{"two", "three", "four", "five"} {
		appendLargeObservation(t, w, id)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"six", "seven"} {
		appendLargeObservation(t, w, id)
	}
	if _, err := Prune(ctx, store, path, DefaultKeepArchives, true); err == nil || !strings.Contains(err.Error(), "lock workspace journal") {
		t.Fatalf("prune ran with an active writer: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	preview, err := Prune(ctx, store, path, DefaultKeepArchives, false)
	if err != nil || len(preview.Archives) != 2 {
		t.Fatalf("retention preview: %+v %v", preview, err)
	}
	for _, name := range preview.Archives {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("preview changed archive: %v", err)
		}
	}
	if _, err := os.Stat(anchorPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview changed anchor: %v", err)
	}
	applied, err := Prune(ctx, store, path, DefaultKeepArchives, true)
	if err != nil || len(applied.Archives) != len(preview.Archives) || applied.Bytes != preview.Bytes {
		t.Fatalf("retention apply: %+v %v", applied, err)
	}
	for _, name := range applied.Archives {
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("archive was not pruned: %v", err)
		}
	}
	// The warm importer must adopt the anchor and continue from the saved
	// cursor, including archives still ahead of it.
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, store, "one", "two", "three", "four", "five", "six", "seven")
	w, err = openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	appendLargeObservation(t, w, "eight")
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, store, "one", "two", "three", "four", "five", "six", "seven", "eight")
}

func TestPruneRequiresCursorInLaterSegmentAndCompletesInterruptedPrune(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	appendLargeObservation(t, w, "first")
	first := inspectActive(t, path)
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, store)
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	appendLargeObservation(t, w, "second")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stale, _ := readTestCursor(t, store)
	if stale.Offset != first.Bytes {
		t.Fatal("fixture cursor is not at the archive's end")
	}
	plan, err := Prune(ctx, store, path, 0, true)
	if err != nil || len(plan.Archives) != 0 {
		t.Fatalf("pruned the cursor segment at EOF: %+v %v", plan, err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	active := inspectActive(t, path)
	saved, revision := readTestCursor(t, store)
	// A crash after the anchor fsync can leave the now-retired predecessor
	// on disk. A later apply must finish that unlink, even with a larger keep.
	anchor := retentionAnchor{Version: 1, First: active.segmentRef, Previous: first.segmentRef, Cursor: saved, Revision: revision}
	if err := writeAnchor(path, anchor); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	plan, err = Prune(ctx, store, path, DefaultKeepArchives, true)
	if err != nil || len(plan.Archives) != 1 || plan.Archives[0] != archivePath(path, first.ID) {
		t.Fatalf("interrupted prune did not finish: %+v %v", plan, err)
	}
	old := openTestSQLite(t, filepath.Join(dir, "stale.db"))
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.PutSetting(ctx, cursorKey(path), data, 0, "test"); err != nil {
		t.Fatal(err)
	}
	oldImporter := journalTestImporter(path, old)
	if err := oldImporter.Import(ctx); err == nil || !strings.Contains(err.Error(), "predates the retention checkpoint") {
		t.Fatalf("restored stale store resumed pruned history: %v", err)
	}
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if w, err := OpenWriter(path); err == nil {
		w.Close()
		t.Fatal("writer recreated an active journal behind an existing anchor")
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(anchorPath(path), anchorPath(path)+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err == nil || !strings.Contains(err.Error(), "chain is missing segment") {
		t.Fatalf("importer ignored a lost retention anchor: %v", err)
	}
}

func TestPruneRefusesMissingOrInvalidCursor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	appendLargeObservation(t, w, "first")
	appendLargeObservation(t, w, "second")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	if _, err := Prune(t.Context(), store, path, 0, true); err == nil || !strings.Contains(err.Error(), "requires a durable cursor") {
		t.Fatalf("pruned without durable cursor: %v", err)
	}
	active := inspectActive(t, path)
	data, err := json.Marshal(cursor{JournalID: active.ID, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutSetting(t.Context(), cursorKey(path), data, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(t.Context(), store, path, 0, true); err == nil || !strings.Contains(err.Error(), "not at a record boundary") {
		t.Fatalf("pruned with a cursor inside a record: %v", err)
	}
	if _, err := os.Stat(archivePath(path, "first")); err != nil {
		t.Fatal(err)
	}
}

func TestImportCacheReusesArchiveLinksAndRejectsReplacement(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	appendLargeObservation(t, w, "first")
	appendLargeObservation(t, w, "second")
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, store)
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	cached := importer.cache.snapshot
	if err := w.Append(Record{ID: "tail", Path: "file", Operation: "modified"}); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	if cached == nil || len(cached.archives) != 1 || importer.cache.snapshot != cached {
		t.Fatal("archive links were reread after an append to the same active segment")
	}
	archive := archivePath(path, "first")
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	replacement := archive + ".replacement"
	data = []byte(strings.Replace(string(data), `"id":"first"`, `"id":"other"`, 1))
	if err := os.WriteFile(replacement, data, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, archive); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err == nil || !strings.Contains(err.Error(), "segment identity changed") {
		t.Fatalf("cached importer accepted a same-sized replacement: %v", err)
	}
}
