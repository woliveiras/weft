package workspacejournal

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func TestUnresolvedGapsFlushAtWindowBoundary(t *testing.T) {
	var gaps unresolvedGaps
	var records []Record
	appendRecord := func(r Record) error {
		records = append(records, r)
		return nil
	}
	start := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	end := start.Add(gapWindowDuration - time.Nanosecond)
	for _, observed := range []time.Time{start, start.Add(time.Second), end} {
		if err := gaps.observe(observed, appendRecord); err != nil {
			t.Fatal(err)
		}
	}
	if len(records) != 0 {
		t.Fatal("window flushed before its deadline")
	}
	next := start.Add(gapWindowDuration)
	if err := gaps.observe(next, appendRecord); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Gap != unresolvedKernelPath || records[0].GapWindow == nil ||
		*records[0].GapWindow != (gapWindow{Count: 3, Start: start, End: end}) {
		t.Fatalf("first window: %+v", records)
	}
	if err := gaps.flushExpired(next.Add(gapWindowDuration), appendRecord); err != nil {
		t.Fatal(err)
	}
	if err := gaps.flush(appendRecord); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[1].GapWindow == nil ||
		*records[1].GapWindow != (gapWindow{Count: 1, Start: next, End: next}) {
		t.Fatalf("second window: %+v", records)
	}
}

func TestUnresolvedGapsReportFlushFailure(t *testing.T) {
	var gaps unresolvedGaps
	now := time.Now()
	failure := errors.New("disk failure")
	appendRecord := func(Record) error { return failure }
	if err := gaps.observe(now, appendRecord); err != nil {
		t.Fatal(err)
	}
	if err := gaps.observe(now.Add(gapWindowDuration), appendRecord); !errors.Is(err, failure) {
		t.Fatalf("flush failure: %v", err)
	}
	if err := gaps.flush(func(r Record) error {
		if r.GapWindow.Count != 1 || !r.Time.Equal(now) {
			t.Fatalf("pending coverage changed on failure: %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestImporterPersistsOnePartialObservationPerGapWindow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	start := time.Now().UTC()
	var gaps unresolvedGaps
	for n := range 1000 {
		if err := gaps.observe(start.Add(time.Duration(n)*time.Millisecond), w.Append); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Append(Record{Gap: "kernel_queue_overflow"}); err != nil {
		t.Fatal(err)
	}
	if err := gaps.flush(w.Append); err != nil {
		t.Fatal(err)
	}
	store := openTestSQLite(t, filepath.Join(dir, "state.db"))
	importer := Importer{Store: store, Path: path, Scopes: func(context.Context) ([]Scope, error) {
		return []Scope{{GraphID: "g"}, {GraphID: "other"}}, nil
	}}
	for range 2 {
		if err := importer.Import(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, graph := range []string{"g", "other"} {
		events, err := store.ListEvents(ctx, storage.EventFilter{GraphID: graph, Ascending: true})
		if err != nil || len(events) != 2 {
			t.Fatalf("observations for %s: %v %v", graph, events, err)
		}
		var overflow, observed struct {
			Gap       bool
			Reason    string
			Status    string
			GapWindow *gapWindow
		}
		if err := json.Unmarshal(events[0].Data, &overflow); err != nil {
			t.Fatal(err)
		}
		if !overflow.Gap || overflow.Status != "partial" || overflow.Reason != "kernel_queue_overflow" || overflow.GapWindow != nil {
			t.Fatalf("overflow observation: %+v", overflow)
		}
		if err := json.Unmarshal(events[1].Data, &observed); err != nil {
			t.Fatal(err)
		}
		end := start.Add(999 * time.Millisecond)
		if !observed.Gap || observed.Status != "partial" || observed.Reason != unresolvedKernelPath ||
			observed.GapWindow == nil || *observed.GapWindow != (gapWindow{Count: 1000, Start: start, End: end}) ||
			!events[1].Time.Equal(end) {
			t.Fatalf("coalesced observation: %s", events[1].Data)
		}
	}
}

func openTestSQLite(t *testing.T, path string) *sqlstore.Store {
	t.Helper()
	ctx := context.Background()
	store, err := sqlstore.OpenSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}
