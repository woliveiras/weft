package workspacejournal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriterRotatesAfterExactLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	r := Record{ID: "first", Time: time.Now().UTC(), Path: "file", Operation: "created"}
	data := recordLine(t, r)
	// A second record exactly fills the segment. The link and a small
	// triggering record fit in the successor.
	second := r
	second.ID = "second"
	second.Path = strings.Repeat("b", 400)
	limit := int64(len(data) + len(recordLine(t, second)))
	w, err := openWriter(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Append(r); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(second); err != nil {
		t.Fatal(err)
	}
	before, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	if size, err := segmentSize(before); err != nil || size != limit {
		t.Fatalf("full active segment: size %d, err %v", size, err)
	}
	archive := archivePath(path, r.ID)
	if _, err := os.Stat(archive); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rotation before limit: %v", err)
	}
	if err := w.Append(Record{ID: "third", Path: "next", Operation: "created"}); err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(archived, append(data, recordLine(t, second)...)) {
		t.Fatalf("archive changed: %v", err)
	}
	original, err := before.Stat()
	if err != nil {
		t.Fatal(err)
	}
	linked, err := os.Stat(archive)
	if err != nil || !os.SameFile(original, linked) {
		t.Fatalf("archive did not preserve original inode: %v", err)
	}
	active := inspectActive(t, path)
	if active.previous == nil || *active.previous != (segmentRef{ID: r.ID, Bytes: limit}) || active.Bytes > limit {
		t.Fatalf("successor link: %+v", active)
	}
}

func TestWriterRetriesInterruptedRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	r := Record{ID: "original", Path: strings.Repeat("a", 600), Operation: "created"}
	if err := w.Append(r); err != nil {
		t.Fatal(err)
	}
	original := inspectActive(t, path)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// Emulate interruption after archiving the inode and before publishing
	// the successor at the active name.
	if err := os.Link(path, archivePath(path, original.ID)); err != nil {
		t.Fatal(err)
	}
	w, err = openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r.ID = "after-restart"
	if err := w.Append(r); err != nil {
		t.Fatal(err)
	}
	active := inspectActive(t, path)
	if active.previous == nil || *active.previous != original.segmentRef {
		t.Fatalf("interrupted rotation did not resume: %+v", active)
	}
}

func TestWriterPreservesArchiveOnCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := Record{ID: "original", Path: strings.Repeat("a", 600), Operation: "created"}
	if err := w.Append(r); err != nil {
		t.Fatal(err)
	}
	original := inspectActive(t, path)
	archive := archivePath(path, original.ID)
	if err := os.WriteFile(archive, []byte("other evidence\n"), 0640); err != nil {
		t.Fatal(err)
	}
	r.ID = "another"
	failure := w.Append(r)
	if failure == nil || !strings.Contains(failure.Error(), "archive already exists") {
		t.Fatalf("archive collision: %v", failure)
	}
	if err := w.Append(Record{Gap: "collector_started"}); !errors.Is(err, failure) {
		t.Fatalf("capture continued after rotation failure: %v", err)
	}
	if active := inspectActive(t, path); active.segmentRef != original.segmentRef {
		t.Fatalf("active segment changed: %+v", active)
	}
	if data, err := os.ReadFile(archive); err != nil || string(data) != "other evidence\n" {
		t.Fatalf("archive overwritten: %q, %v", data, err)
	}
}

func TestWriterRefusesConcurrentOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if another, err := OpenWriter(path); err == nil {
		another.Close()
		t.Fatal("journal admitted a second writer")
	}
}

func TestWriterRefusesMissingOrCorruptActiveSegment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active []byte
		want   string
	}{
		{"missing", nil, "no active segment identity"},
		{"empty", []byte{}, "no active segment identity"},
		{"corrupt", []byte("{broken}\n"), "invalid journal identity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "events.jsonl")
			old := recordLine(t, Record{ID: "old", Time: time.Now().UTC(), Gap: "collector_started"})
			archive := archivePath(path, "old")
			if err := os.WriteFile(archive, old, 0640); err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(dir, stagePrefix(path)+"123")
			if err := os.WriteFile(stage, []byte("staged evidence"), 0640); err != nil {
				t.Fatal(err)
			}
			if tc.active != nil {
				if err := os.WriteFile(path, tc.active, 0640); err != nil {
					t.Fatal(err)
				}
			}
			if w, err := OpenWriter(path); err == nil {
				w.Close()
				t.Fatal("opened damaged journal")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if tc.active == nil && !errors.Is(err, os.ErrNotExist) ||
				tc.active != nil && (err != nil || !bytes.Equal(got, tc.active)) {
				t.Fatalf("active segment changed: %q, %v", got, err)
			}
			if got, err := os.ReadFile(archive); err != nil || !bytes.Equal(got, old) {
				t.Fatalf("archive changed: %v", err)
			}
			if _, err := os.Stat(stage); err != nil {
				t.Fatalf("staged evidence removed during failed open: %v", err)
			}
		})
	}
}

func TestWriterRemovesOnlyOrphanStagesForItsJournal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(Record{Gap: "collector_started"}); err != nil {
		w.Close()
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(dir, stagePrefix(path)+"123")
	other := filepath.Join(dir, ".other.jsonl.next-123")
	for _, name := range []string{stage, other} {
		if err := os.WriteFile(name, []byte("fixture"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	w, err = OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan stage was not cleaned: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated stage was removed: %v", err)
	}
}

func TestOpenWriterRejectsSameSizeArchiveWithDifferentIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	w, err := openWriter(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	appendLargeObservation(t, w, "first")
	appendLargeObservation(t, w, "second")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	archive := archivePath(path, "first")
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	replacement := bytes.Replace(data, []byte(`"id":"first"`), []byte(`"id":"other"`), 1)
	if bytes.Equal(data, replacement) || len(replacement) != len(data) {
		t.Fatal("replacement must change the ID without changing the size")
	}
	if err := os.WriteFile(archive+".replacement", replacement, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(archive+".replacement", archive); err != nil {
		t.Fatal(err)
	}
	if reopened, err := openWriter(path, 1024); err == nil {
		reopened.Close()
		t.Fatal("opened a journal with a replaced archive")
	} else if !strings.Contains(err.Error(), "segment identity changed") {
		t.Fatal(err)
	}
}

func recordLine(t *testing.T, r Record) []byte {
	t.Helper()
	line, err := encodeRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func inspectActive(t *testing.T, path string) segment {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	size, err := segmentSize(f)
	if err != nil {
		t.Fatal(err)
	}
	s, err := readSegment(f, size)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
