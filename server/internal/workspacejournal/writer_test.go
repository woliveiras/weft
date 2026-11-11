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
