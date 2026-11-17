package workspacejournal

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestJournalValidationAndSizeLimit(t *testing.T) {
	for _, path := range []string{"../secret", "/root", "a/../b", "."} {
		if validPath(path) {
			t.Errorf("accepted %q", path)
		}
	}
	w, err := OpenWriter(filepath.Join(t.TempDir(), "events"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Append(Record{Path: strings.Repeat("x", maxRecordBytes), Operation: "created"}); err == nil {
		t.Fatal("record size limit ignored")
	}
	if err := w.Append(Record{Path: "../secret", Operation: "created"}); err == nil {
		t.Fatal("invalid record written")
	}
	if w.size != 0 {
		t.Fatal("invalid record changed the journal")
	}
	if validOperation("execute") {
		t.Fatal("invalid operation accepted")
	}
	if rel, ok := scopePath("project", "project-two/file"); ok {
		t.Fatal(rel)
	}
}

func TestImporterDurableReplayAndTransientChanges(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"created", "modified", "deleted"} {
		if err := w.Append(Record{Path: "project/transient", Operation: op, PID: 42, ProcessStart: "100", Namespace: "pid:[fixture]"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "store")
	store, err := fsstore.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	scopes := func(context.Context) ([]Scope, error) {
		return []Scope{{GraphID: "project", RelativeRoot: "project"}, {GraphID: "other", RelativeRoot: "other"}}, nil
	}
	importer := Importer{Store: store, Path: path, Scopes: scopes}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = fsstore.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	importer.Store = store
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, storage.EventFilter{GraphID: "project", Ascending: true})
	if err != nil || len(events) != 3 {
		t.Fatalf("%+v %v", events, err)
	}
	for index, ev := range events {
		var data map[string]any
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatal(err)
		}
		if ev.ExecutionID != "" || data["source"] != "kernel-journal" {
			t.Fatal("incorrect attribution")
		}
		if data["changes"].([]any)[0].(map[string]any)["operation"] != []string{"created", "modified", "deleted"}[index] {
			t.Fatal("lost transient order")
		}
	}
	other, err := store.ListEvents(ctx, storage.EventFilter{GraphID: "other"})
	if err != nil || len(other) != 0 {
		t.Fatal("cross-graph leak")
	}
}

type cursorFailure struct {
	storage.Store
	fail bool
}

func (s *cursorFailure) PutSetting(ctx context.Context, key string, value []byte, revision int64, by string) (int64, error) {
	if s.fail {
		s.fail = false
		return 0, errors.New("disk failure")
	}
	return s.Store.PutSetting(ctx, key, value, revision, by)
}

func TestImporterCrashBetweenEventAndCursorIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(Record{Path: "file", Operation: "created"}); err != nil {
		t.Fatal(err)
	}
	w.Close()
	store, err := fsstore.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	importer := Importer{Store: &cursorFailure{Store: store, fail: true}, Path: path, Scopes: func(context.Context) ([]Scope, error) { return []Scope{{GraphID: "g"}}, nil }}
	if importer.Import(ctx) == nil {
		t.Fatal("cursor error swallowed")
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, storage.EventFilter{GraphID: "g"})
	if err != nil || len(events) != 1 {
		t.Fatalf("duplicate events: %v %v", events, err)
	}
}

func TestImporterRejectsCorruptionAndWaitsForPartialRecord(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	store, err := fsstore.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	importer := Importer{Store: store, Path: path, Scopes: func(context.Context) ([]Scope, error) { return []Scope{{GraphID: "g"}}, nil }}
	if importer.Import(ctx) == nil {
		t.Fatal("missing journal accepted")
	}
	record, _ := json.Marshal(Record{ID: "record", Time: time.Now(), Path: "file", Operation: "created"})
	if err := os.WriteFile(path, record, 0600); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListEvents(ctx, storage.EventFilter{})
	if len(events) != 0 {
		t.Fatal("partial record consumed")
	}
	if err := os.WriteFile(path, append(record, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bad\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if importer.Import(ctx) == nil {
		t.Fatal("truncation accepted")
	}
}

func TestWriterRefusesIncompleteTailAndImporterRejectsSourceReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	if err := os.WriteFile(path, []byte(`{"id":`), 0600); err != nil {
		t.Fatal(err)
	}
	if w, err := OpenWriter(path); err == nil {
		w.Close()
		t.Fatal("incomplete tail accepted")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(Record{ID: "first", Path: "file", Operation: "created"}); err != nil {
		t.Fatal(err)
	}
	w.Close()
	store, err := fsstore.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	importer := Importer{Store: store, Path: path, Scopes: func(context.Context) ([]Scope, error) { return []Scope{{GraphID: "g"}}, nil }}
	if err := importer.Import(context.Background()); err != nil {
		t.Fatal(err)
	}
	replacement, _ := json.Marshal(Record{ID: "replacement-with-larger-record", Time: time.Now(), Path: "file", Operation: "created"})
	if err := os.WriteFile(path, append(replacement, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if importer.Import(context.Background()) == nil {
		t.Fatal("source replacement accepted")
	}
}

type eventFailure struct {
	storage.Store
	fail bool
}

func (s *eventFailure) AppendEvent(ctx context.Context, event *domain.Event) error {
	if s.fail {
		s.fail = false
		return errors.New("event persistence failed")
	}
	return s.Store.AppendEvent(ctx, event)
}

func TestImporterDoesNotAdvancePastEventFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	ctx := context.Background()
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(Record{Path: "file", Operation: "created"}); err != nil {
		t.Fatal(err)
	}
	w.Close()
	store, err := fsstore.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	importer := Importer{Store: &eventFailure{Store: store, fail: true}, Path: path, Scopes: func(context.Context) ([]Scope, error) { return []Scope{{GraphID: "g"}}, nil }}
	if importer.Import(ctx) == nil {
		t.Fatal("event failure swallowed")
	}
	settings, err := store.ListSettings(ctx, "workspace-journal/")
	if err != nil || len(settings) != 0 {
		t.Fatal("cursor advanced past failed event")
	}
	if err := importer.Import(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, storage.EventFilter{GraphID: "g"})
	if err != nil || len(events) != 1 {
		t.Fatal("event was lost")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if importer.Import(ctx) == nil {
		t.Fatal("empty replacement accepted")
	}
}

func TestImporterRefusesInvalidCursor(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	line := recordLine(t, Record{ID: "first", Time: time.Now().UTC(), Path: "file", Operation: "created"})
	if err := os.WriteFile(path, line, 0640); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		saved     cursor
		errorText string
	}{
		{cursor{Offset: -1, JournalID: "first"}, "invalid workspace journal cursor"},
		{cursor{JournalID: "first"}, "invalid workspace journal cursor"},
		{cursor{Offset: 1, JournalID: "first"}, "not at a record boundary"},
		{cursor{Offset: int64(len(line)) + 1, JournalID: "first"}, "journal was truncated"},
		{cursor{Offset: int64(len(line)), JournalID: "different"}, "journal identity changed"},
	} {
		t.Run(invalid.errorText, func(t *testing.T) {
			store, err := fsstore.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			data, err := json.Marshal(invalid.saved)
			if err != nil {
				t.Fatal(err)
			}
			key := fmt.Sprintf("workspace-journal/cursor/%x", sha256.Sum256([]byte(path)))
			if _, err := store.PutSetting(ctx, key, data, 0, "test"); err != nil {
				t.Fatal(err)
			}
			importer := journalTestImporter(path, store)
			if err := importer.Import(ctx); err == nil || !strings.Contains(err.Error(), invalid.errorText) {
				t.Fatalf("invalid cursor %+v: %v", invalid.saved, err)
			}
			saved, revision := readTestCursor(t, store)
			if saved != invalid.saved || revision != 1 {
				t.Fatal("invalid cursor changed")
			}
			checkEventOrder(t, store)
		})
	}
}
