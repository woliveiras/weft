package workspacejournal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

func TestImporterResumesLegacyCursorAcrossRotations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	state := filepath.Join(dir, "state.db")
	w, err := openWriter(path, 1400)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	appendLargeObservation(t, w, "legacy")
	initial := inspectActive(t, path)
	t.Run("persist the cursor for the unrotated file", func(t *testing.T) {
		importer := journalTestImporter(path, openTestSQLite(t, state))
		if err := importer.Import(ctx); err != nil {
			t.Fatal(err)
		}
		saved, _ := readTestCursor(t, importer.Store)
		if saved != (cursor{JournalID: initial.ID, Offset: initial.Bytes}) {
			t.Fatalf("legacy cursor: %+v", saved)
		}
	})
	for _, id := range []string{"archived-tail", "middle", "latest"} {
		appendLargeObservation(t, w, id)
	}
	active := inspectActive(t, path)
	if active.previous == nil || active.previous.ID == initial.ID {
		t.Fatal("expected at least two rotations")
	}
	importer := journalTestImporter(path, openTestSQLite(t, state))
	for range 2 {
		if err := importer.Import(ctx); err != nil {
			t.Fatal(err)
		}
	}
	checkEventOrder(t, importer.Store, "legacy", "archived-tail", "middle", "latest")
	saved, _ := readTestCursor(t, importer.Store)
	if saved != (cursor{JournalID: active.ID, Offset: active.Bytes}) {
		t.Fatalf("rotated cursor: %+v", saved)
	}
}

func TestWriterReopensAfterRenameFailureAndImporterResumes(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	appendLargeObservation(t, w, "first")
	original := inspectActive(t, path)
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, store)
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, stagePrefix(path)+"123")
	w.rename = func(staged, active string) error {
		if active != path {
			t.Fatalf("unexpected publication path %q", active)
		}
		data, err := os.ReadFile(staged)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(orphan, data, 0640); err != nil {
			t.Fatal(err)
		}
		// Exercise a real failed rename after the archive link was created.
		return os.Rename(staged, dir)
	}
	second := Record{ID: "second", Path: strings.Repeat("b", 600), Operation: "created"}
	if err := w.Append(second); err == nil || !strings.Contains(err.Error(), "publish workspace journal segment") {
		t.Fatalf("rotation rename failure: %v", err)
	}
	if active := inspectActive(t, path); active.segmentRef != original.segmentRef {
		t.Fatal("failed rename changed the active segment")
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, store, "first")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("interrupted stage was not removed: %v", err)
	}
	if err := w.Append(second); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, store, "first", "second")
}

func TestImportSnapshotSurvivesConcurrentRotation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	appendLargeObservation(t, w, "original")
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, store)
	scopes := importer.Scopes
	importer.Scopes = func(ctx context.Context) ([]Scope, error) {
		// The importer already has the old active file open at this point.
		appendLargeObservation(t, w, "successor")
		return scopes(ctx)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, store, "original")
	importer.Scopes = scopes
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, store, "original", "successor")
}

func TestImporterFinishesArchiveBeforeAdvancingBatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	r := Record{ID: "r0000", Time: time.Now().UTC(), Path: "file", Operation: "created"}
	length := int64(len(recordLine(t, r)))
	w, err := openWriter(path, (importBatchSize+1)*length)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for n := range importBatchSize + 2 {
		r.ID = fmt.Sprintf("r%04d", n)
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	importer := journalTestImporter(path, openTestSQLite(t, filepath.Join(dir, "state.db")))
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ := readTestCursor(t, importer.Store)
	if saved != (cursor{JournalID: "r0000", Offset: importBatchSize * length}) {
		t.Fatalf("batch advanced past an undrained archive: %+v", saved)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	active := inspectActive(t, path)
	saved, revision := readTestCursor(t, importer.Store)
	if saved != (cursor{JournalID: active.ID, Offset: active.Bytes}) || revision != importBatchSize+3 {
		t.Fatalf("final cursor: %+v revision %d", saved, revision)
	}
}

func TestImporterRetriesCursorWriteAtSegmentBoundary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := journalTestImporter(path, store)
	appendLargeObservation(t, w, "full")
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	before, revision := readTestCursor(t, store)
	appendLargeObservation(t, w, "next")
	importer.Store = &cursorFailure{Store: store, fail: true}
	if err := importer.Import(ctx); err == nil || !strings.Contains(err.Error(), "disk failure") {
		t.Fatalf("expected cursor write failure: %v", err)
	}
	after, afterRevision := readTestCursor(t, store)
	if after != before || afterRevision != revision {
		t.Fatal("failed transition advanced the cursor")
	}
	checkEventOrder(t, store, "full")
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, store, "full", "next")
}

func TestImporterRefusesBrokenChainAfterCursorAdvanced(t *testing.T) {
	ctx := context.Background()
	for _, broken := range []struct {
		name, errorText string
		change          func(string) error
	}{
		{"missing", "chain is missing segment", func(path string) error { return os.Rename(path, path+".preserved") }},
		{"truncated", "segment size changed", func(path string) error { return os.Truncate(path, 1) }},
	} {
		t.Run(broken.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "events")
			w, err := openWriter(path, 1024)
			if err != nil {
				t.Fatal(err)
			}
			appendLargeObservation(t, w, "original")
			appendLargeObservation(t, w, "successor")
			importer := journalTestImporter(path, openTestSQLite(t, filepath.Join(dir, "state.db")))
			if err := importer.Import(ctx); err != nil {
				t.Fatal(err)
			}
			before, revision := readTestCursor(t, importer.Store)
			if err := w.Append(Record{ID: "after", Path: "file", Operation: "modified"}); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			archive := archivePath(path, "original")
			if err := broken.change(archive); err != nil {
				t.Fatal(err)
			}
			if err := importer.Import(ctx); err == nil || !strings.Contains(err.Error(), broken.errorText) {
				t.Fatalf("broken chain import: %v", err)
			}
			after, afterRevision := readTestCursor(t, importer.Store)
			if after != before || afterRevision != revision {
				t.Fatal("cursor advanced past a broken chain")
			}
			checkEventOrder(t, importer.Store, "original", "successor")
			if writer, err := OpenWriter(path); err == nil {
				writer.Close()
				t.Fatal("writer reopened a broken chain")
			}
			if broken.name == "missing" {
				if err := os.Rename(archive+".preserved", archive); err != nil {
					t.Fatal(err)
				}
				if err := importer.Import(ctx); err != nil {
					t.Fatal(err)
				}
				checkEventOrder(t, importer.Store, "original", "successor", "after")
			}
		})
	}
}

func TestImporterRetriesPartialActiveRecordAfterRotation(t *testing.T) {
	ctx := context.Background()
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
	importer := journalTestImporter(path, openTestSQLite(t, filepath.Join(dir, "state.db")))
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	before, revision := readTestCursor(t, importer.Store)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := recordLine(t, Record{ID: "third", Time: time.Now().UTC(), Path: "file", Operation: "created"})
	if _, err := f.Write(line[:len(line)-1]); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	after, afterRevision := readTestCursor(t, importer.Store)
	if after != before || afterRevision != revision {
		t.Fatal("partial record advanced the cursor")
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	checkEventOrder(t, importer.Store, "first", "second", "third")
}

func journalTestImporter(path string, store storage.Store) Importer {
	return Importer{Store: store, Path: path, Scopes: func(context.Context) ([]Scope, error) {
		return []Scope{{GraphID: "g"}}, nil
	}}
}

func appendLargeObservation(t *testing.T, w *Writer, id string) {
	t.Helper()
	if err := w.Append(Record{ID: id, Path: strings.Repeat("a", 600), Operation: "created"}); err != nil {
		t.Fatal(err)
	}
}

func checkEventOrder(t *testing.T, store storage.Store, want ...string) {
	t.Helper()
	events, err := store.ListEvents(context.Background(), storage.EventFilter{GraphID: "g", Ascending: true})
	if err != nil || len(events) != len(want) {
		t.Fatalf("want %d observations, got %d (%v)", len(want), len(events), err)
	}
	for n, id := range want {
		if events[n].ID != id+"_g_0" {
			t.Fatalf("observation %d: got %q, expected record %q", n, events[n].ID, id)
		}
	}
}

func readTestCursor(t *testing.T, store storage.Store) (cursor, int64) {
	t.Helper()
	settings, err := store.ListSettings(context.Background(), "workspace-journal/cursor/")
	if err != nil || len(settings) != 1 {
		t.Fatalf("expected a single cursor, got %d (%v)", len(settings), err)
	}
	var saved cursor
	if err := json.Unmarshal(settings[0].Value, &saved); err != nil {
		t.Fatal(err)
	}
	return saved, settings[0].Revision
}
