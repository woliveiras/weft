package workspacejournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

const (
	MaxJournalBytes int64 = 64 << 20
	maxRecordBytes        = 60 << 10
)

// Writer belongs to one capture loop. A filesystem lock prevents a second
// writer from opening the same journal while the first is active.
type Writer struct {
	path     string
	f        *os.File
	unlock   func() error
	id       string
	size     int64
	maxBytes int64
	err      error
}

func OpenWriter(path string) (*Writer, error) {
	return openWriter(path, MaxJournalBytes)
}

func openWriter(path string, maxBytes int64) (_ *Writer, err error) {
	unlock, err := lockWriter(path + ".lock")
	if err != nil {
		return nil, fmt.Errorf("lock workspace journal: %w", err)
	}
	w := &Writer{path: path, maxBytes: maxBytes, unlock: unlock}
	defer func() {
		if err != nil {
			err = errors.Join(err, w.Close())
		}
	}()
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := requireNoArchives(path); err != nil {
			return nil, err
		}
		w.f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_APPEND|os.O_RDWR, 0640)
		if err != nil {
			return nil, err
		}
		if err := syncJournalDirectory(path); err != nil {
			return nil, err
		}
		return w, nil
	case err != nil:
		return nil, err
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("active workspace journal must be a regular file")
	}
	w.f, err = os.OpenFile(path, os.O_APPEND|os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	w.size, err = segmentSize(w.f)
	if err != nil {
		return nil, err
	}
	if w.size == 0 {
		if err := requireNoArchives(path); err != nil {
			return nil, err
		}
		return w, nil
	}
	if err := requireCompleteTail(w.f, w.size); err != nil {
		return nil, err
	}
	s, err := readSegment(w.f, w.size)
	if err != nil {
		return nil, err
	}
	if _, err := readChain(context.Background(), path, s); err != nil {
		return nil, err
	}
	w.id = s.ID
	return w, nil
}

func (w *Writer) Close() error {
	if w.unlock == nil {
		return os.ErrClosed
	}
	var err error
	if w.f != nil {
		err = w.f.Close()
	}
	unlock := w.unlock
	w.unlock = nil
	return errors.Join(err, unlock())
}

func (w *Writer) Append(r Record) error {
	if w.unlock == nil {
		return os.ErrClosed
	}
	if w.err != nil {
		return w.err
	}
	if r.PreviousSegment != nil {
		return fmt.Errorf("only journal rotation can write a segment link")
	}
	if r.ID == "" {
		r.ID = domain.NewID("wj")
	}
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	data, err := encodeRecord(r)
	if err != nil {
		return err
	}
	if w.size+int64(len(data)) > w.maxBytes {
		if err := w.rotate(data); err != nil {
			w.err = fmt.Errorf("rotate workspace journal: %w", err)
		}
		return w.err
	}
	if err := writeAndSync(w.f, data); err != nil {
		// A failed append may have left a partial record on disk.
		w.err = fmt.Errorf("persist workspace journal record: %w", err)
		return w.err
	}
	if w.size == 0 {
		w.id = r.ID
	}
	w.size += int64(len(data))
	return nil
}

func encodeRecord(r Record) ([]byte, error) {
	if err := validateRecord(r); err != nil {
		return nil, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(data)+1 > maxRecordBytes {
		return nil, fmt.Errorf("journal record exceeds limit")
	}
	return append(data, '\n'), nil
}

func (w *Writer) rotate(data []byte) (err error) {
	if w.id == "" {
		return fmt.Errorf("record does not fit in an empty journal segment")
	}
	link := Record{ID: domain.NewID("wj"), Time: time.Now().UTC(), PreviousSegment: &segmentRef{ID: w.id, Bytes: w.size}}
	header, err := encodeRecord(link)
	if err != nil {
		return err
	}
	newSize := int64(len(header) + len(data))
	if newSize > w.maxBytes {
		return fmt.Errorf("record and segment link exceed journal segment limit")
	}
	next, err := os.CreateTemp(filepath.Dir(w.path), "."+filepath.Base(w.path)+".next-*")
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			err = errors.Join(err, next.Close(), os.Remove(next.Name()))
		}
	}()
	if err := next.Chmod(0640); err != nil {
		return err
	}
	if err := writeAndSync(next, append(header, data...)); err != nil {
		return err
	}
	if err := w.linkArchive(); err != nil {
		return err
	}
	// Persist the archive before replacing the active name. The active file
	// remains readable throughout, including across a crash before rename.
	if err := syncJournalDirectory(w.path); err != nil {
		return err
	}
	if err := os.Rename(next.Name(), w.path); err != nil {
		return err
	}
	published = true
	old, previous := w.f, *link.PreviousSegment
	w.f, w.id, w.size = next, link.ID, newSize
	if err := errors.Join(syncJournalDirectory(w.path), old.Close()); err != nil {
		return err
	}
	slog.Info("workspace journal rotated", "previous_segment", previous.ID, "segment", link.ID, "archived_bytes", previous.Bytes)
	return nil
}

func (w *Writer) linkArchive() error {
	old, err := w.f.Stat()
	if err != nil {
		return err
	}
	active, err := os.Lstat(w.path)
	if err != nil {
		return err
	}
	if old.Size() != w.size || !os.SameFile(active, old) {
		return fmt.Errorf("active journal changed before rotation")
	}
	archive := archivePath(w.path, w.id)
	if err := os.Link(w.path, archive); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("archive workspace journal segment: %w", err)
	}
	saved, err := os.Lstat(archive)
	if err != nil {
		return err
	}
	// A crash before rename may leave the archive as an alias of the active
	// file. Reuse that link, but never replace an unrelated archive.
	if !os.SameFile(saved, old) {
		return fmt.Errorf("archive already exists for a different journal file")
	}
	return nil
}

func syncJournalDirectory(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
		return fmt.Errorf("sync workspace journal directory: %w", err)
	}
	return nil
}

func writeAndSync(f *os.File, data []byte) error {
	n, err := f.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return f.Sync()
}
