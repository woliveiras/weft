// Package workspacejournal records kernel observations independently of weftd.
package workspacejournal

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const MaxJournalBytes int64 = 64 << 20

type Record struct {
	ID           string    `json:"id"`
	Time         time.Time `json:"time"`
	Path         string    `json:"path,omitempty"`
	Operation    string    `json:"operation,omitempty"`
	PID          int32     `json:"pid,omitempty"`
	ProcessStart string    `json:"processStart,omitempty"`
	Namespace    string    `json:"namespace,omitempty"`
	BootID       string    `json:"bootId,omitempty"`
	Gap          string    `json:"gap,omitempty"`
}

type Writer struct {
	f    *os.File
	size int64
}

func OpenWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0640)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, info.Size()-1); err != nil || last[0] != '\n' {
			f.Close()
			return nil, fmt.Errorf("journal ends with an incomplete record; preserve it for recovery")
		}
	}
	return &Writer{f, info.Size()}, nil
}
func (w *Writer) Close() error { return w.f.Close() }
func (w *Writer) Append(r Record) error {
	if r.ID == "" {
		r.ID = domain.NewID("wj")
	}
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 60<<10 {
		return fmt.Errorf("journal record exceeds limit")
	}
	if w.size+int64(len(data)) > MaxJournalBytes {
		return fmt.Errorf("workspace journal reached size limit; capture stopped")
	}
	n, err := w.f.Write(data)
	w.size += int64(n)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return w.f.Sync()
}

// Scope is installation-derived, not supplied by an agent or journal record.
type Scope struct {
	GraphID, NodeID, RelativeRoot string
	Replica                       int
}

type Importer struct {
	Store  storage.Store
	Path   string
	Scopes func(context.Context) ([]Scope, error)
}

func (i *Importer) Import(ctx context.Context) error {
	scopes, err := i.Scopes(ctx)
	if err != nil {
		return err
	}
	f, err := os.Open(i.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	// A cursor is committed only after event persistence. Stable IDs make replay
	// idempotent when the process stops between these two writes.
	key := fmt.Sprintf("workspace-journal/cursor/%x", sha256.Sum256([]byte(i.Path)))
	first, firstErr := bufio.NewReaderSize(f, 64<<10).ReadSlice('\n')
	if firstErr == io.EOF {
		if _, err := i.Store.GetSetting(ctx, key); err == nil {
			return fmt.Errorf("journal lost its initial record; refusing cursor reuse")
		} else if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		return nil
	}
	if firstErr != nil {
		return firstErr
	}
	var identity Record
	if err := json.Unmarshal(first, &identity); err != nil || identity.ID == "" {
		return fmt.Errorf("invalid journal identity")
	}
	type cursor struct {
		Offset    int64
		JournalID string
	}
	saved := cursor{JournalID: identity.ID}
	var offset int64
	var revision int64
	setting, err := i.Store.GetSetting(ctx, key)
	if err == nil {
		if err = json.Unmarshal(setting.Value, &saved); err != nil {
			return err
		}
		if saved.JournalID != identity.ID {
			return fmt.Errorf("journal identity changed; refusing cursor reuse")
		}
		offset = saved.Offset
		revision = setting.Revision
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if offset > info.Size() {
		return fmt.Errorf("workspace journal was truncated; refusing cursor reset")
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(f, 64<<10)
	for n := 0; n < 1000; n++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := reader.ReadSlice('\n')
		if err == io.EOF {
			break
		} // a partial last line is retried, never consumed
		if err != nil {
			return fmt.Errorf("workspace journal record: %w", err)
		}
		var r Record
		if err = json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("workspace journal record: %w", err)
		}
		if r.ID == "" || r.Time.IsZero() {
			return fmt.Errorf("workspace journal record missing identity")
		}
		if r.Gap == "" && (!validPath(r.Path) || !validOperation(r.Operation)) {
			return fmt.Errorf("invalid workspace journal operation")
		}
		for _, s := range scopes {
			rel, ok := scopePath(s.RelativeRoot, r.Path)
			if r.Gap == "" && !ok {
				continue
			}
			data := map[string]any{"source": "kernel-journal", "attribution": "kernel_pid", "journalId": r.ID, "process": map[string]any{"pid": r.PID, "start": r.ProcessStart, "namespace": r.Namespace, "bootId": r.BootID}, "status": "complete", "changes": []map[string]string{{"path": rel, "operation": r.Operation}}}
			if r.PID <= 0 {
				data["attribution"] = "unknown"
			}
			if r.Gap != "" {
				data["gap"] = true
				data["reason"] = r.Gap
				data["status"] = "partial"
				data["changes"] = []any{}
				data["attribution"] = "unknown"
			}
			ev := domain.NewEvent("workspace.observed", s.GraphID, data).WithActor("workspace-journal")
			ev.ID = r.ID + "_" + s.GraphID + "_" + s.NodeID + fmt.Sprint(s.Replica)
			ev.Time = r.Time
			if s.NodeID != "" {
				ev.WithNode(s.NodeID, s.Replica)
			}
			if err := i.Store.AppendEvent(ctx, ev); err != nil && !errors.Is(err, storage.ErrConflict) {
				return err
			}
		}
		offset += int64(len(line))
		saved.Offset = offset
		data, _ := json.Marshal(saved)
		revision, err = i.Store.PutSetting(ctx, key, data, revision, "workspace-journal")
		if err != nil {
			return err
		}
	}
	return nil
}

func validPath(p string) bool {
	return p != "" && !filepath.IsAbs(p) && filepath.ToSlash(filepath.Clean(p)) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}
func validOperation(op string) bool {
	return op == "created" || op == "modified" || op == "deleted" || op == "moved_from" || op == "moved_to"
}
func scopePath(root, path string) (string, bool) {
	root = strings.Trim(root, "/")
	if root == "" || root == "." {
		return path, validPath(path)
	}
	if !strings.HasPrefix(path, root+"/") {
		return "", false
	}
	rel := strings.TrimPrefix(path, root+"/")
	return rel, validPath(rel)
}
