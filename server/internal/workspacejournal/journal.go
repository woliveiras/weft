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
	"slices"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

type Record struct {
	ID              string      `json:"id"`
	Time            time.Time   `json:"time"`
	Path            string      `json:"path,omitempty"`
	Operation       string      `json:"operation,omitempty"`
	PID             int32       `json:"pid,omitempty"`
	ProcessStart    string      `json:"processStart,omitempty"`
	Namespace       string      `json:"namespace,omitempty"`
	BootID          string      `json:"bootId,omitempty"`
	Gap             string      `json:"gap,omitempty"`
	GapWindow       *gapWindow  `json:"gapWindow,omitempty"`
	PreviousSegment *segmentRef `json:"previousSegment,omitempty"`
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

type cursor struct {
	Offset    int64
	JournalID string
}

type importProgress struct {
	key       string
	revision  int64
	remaining int
}

const importBatchSize = 1000

func (i *Importer) Import(ctx context.Context) (err error) {
	started := time.Now()
	defer func() {
		observability.WorkspaceJournalImportSeconds.Observe(time.Since(started).Seconds())
		if err == nil {
			observability.WorkspaceJournalImports.WithLabelValues("ok").Inc()
		} else if ctx.Err() == nil {
			observability.WorkspaceJournalImports.WithLabelValues("error").Inc()
		}
	}()
	observability.WorkspaceJournalSegmentLimitBytes.Set(float64(MaxJournalBytes))
	key := fmt.Sprintf("workspace-journal/cursor/%x", sha256.Sum256([]byte(i.Path)))
	saved, revision, err := i.loadCursor(ctx, key)
	if err != nil {
		return err
	}
	f, err := os.Open(i.Path)
	if err != nil {
		return fmt.Errorf("open active workspace journal: %w", err)
	}
	defer f.Close()
	size, err := segmentSize(f)
	if err != nil {
		return err
	}
	observability.WorkspaceJournalActiveBytes.Set(float64(size))
	active, err := readSegment(f, size)
	if err == io.EOF {
		if saved.JournalID != "" {
			return fmt.Errorf("journal lost its initial record; refusing cursor reuse")
		}
		if err := requireNoArchives(i.Path); err != nil {
			return err
		}
		observability.WorkspaceJournalArchivedBytes.Set(0)
		return nil
	}
	if err != nil {
		return err
	}
	chain, err := readChain(ctx, i.Path, active)
	if err != nil {
		return err
	}
	var archivedBytes int64
	for _, archived := range chain[:len(chain)-1] {
		archivedBytes += archived.Bytes
	}
	observability.WorkspaceJournalArchivedBytes.Set(float64(archivedBytes))
	if saved.JournalID == "" {
		saved.JournalID = chain[0].ID
	}
	start := slices.IndexFunc(chain, func(s segment) bool { return s.ID == saved.JournalID })
	if start < 0 {
		return fmt.Errorf("journal identity changed; refusing cursor reuse: segment %q is absent from the active chain", saved.JournalID)
	}
	scopes, err := i.Scopes(ctx)
	if err != nil {
		return fmt.Errorf("discover workspace journal scopes: %w", err)
	}
	progress := importProgress{key: key, revision: revision, remaining: importBatchSize}
	for _, s := range chain[start:] {
		offset := int64(0)
		if s.ID == saved.JournalID {
			offset = saved.Offset
		}
		if s.ID == active.ID {
			return i.importSegment(ctx, f, s, offset, false, scopes, &progress)
		}
		if err := i.importArchive(ctx, s, offset, scopes, &progress); err != nil {
			return err
		}
		if progress.remaining == 0 {
			break
		}
	}
	return nil
}

func (i *Importer) loadCursor(ctx context.Context, key string) (cursor, int64, error) {
	setting, err := i.Store.GetSetting(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return cursor{}, 0, nil
	}
	if err != nil {
		return cursor{}, 0, fmt.Errorf("load workspace journal cursor: %w", err)
	}
	var saved cursor
	if err := json.Unmarshal(setting.Value, &saved); err != nil {
		return cursor{}, 0, fmt.Errorf("decode workspace journal cursor: %w", err)
	}
	if saved.Offset <= 0 || saved.JournalID == "" {
		return cursor{}, 0, fmt.Errorf("invalid workspace journal cursor")
	}
	return saved, setting.Revision, nil
}

func (i *Importer) importArchive(ctx context.Context, s segment, offset int64, scopes []Scope, progress *importProgress) error {
	f, current, err := openArchive(i.Path, s.segmentRef)
	if err != nil {
		return err
	}
	defer f.Close()
	if !sameLink(current.previous, s.previous) {
		return fmt.Errorf("workspace journal archive %q changed during import", s.ID)
	}
	return i.importSegment(ctx, f, s, offset, true, scopes, progress)
}

func sameLink(a, b *segmentRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (i *Importer) importSegment(ctx context.Context, f *os.File, s segment, offset int64, archived bool, scopes []Scope, progress *importProgress) error {
	if offset > s.Bytes {
		return fmt.Errorf("workspace journal was truncated; refusing cursor reset")
	}
	if offset > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], offset-1); err != nil {
			return fmt.Errorf("read workspace journal cursor boundary: %w", err)
		}
		if last[0] != '\n' {
			return fmt.Errorf("workspace journal cursor is not at a record boundary")
		}
	}
	if offset == s.Bytes {
		return nil
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(f, offset, s.Bytes-offset), recordBufferBytes)
	for offset < s.Bytes && progress.remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadSlice('\n')
		if err == io.EOF {
			if archived || offset+int64(len(line)) < s.Bytes {
				return fmt.Errorf("workspace journal segment %q ended before a complete record", s.ID)
			}
			return nil // an incomplete active record is retried
		}
		if err != nil {
			return fmt.Errorf("workspace journal record: %w", err)
		}
		r, err := decodeRecord(line)
		if err != nil {
			return fmt.Errorf("workspace journal record: %w", err)
		}
		if offset == 0 {
			if r.ID != s.ID || !sameLink(r.PreviousSegment, s.previous) {
				return fmt.Errorf("workspace journal segment %q changed during import", s.ID)
			}
		} else if r.PreviousSegment != nil {
			return fmt.Errorf("workspace journal segment link is not its first record")
		}
		if r.PreviousSegment == nil {
			if err := i.persistObservation(ctx, r, scopes); err != nil {
				return err
			}
		}
		// Commit the cursor after persisting all scopes. Stable event IDs make
		// replays idempotent if the setting write fails.
		offset += int64(len(line))
		data, _ := json.Marshal(cursor{Offset: offset, JournalID: s.ID})
		progress.revision, err = i.Store.PutSetting(ctx, progress.key, data, progress.revision, "workspace-journal")
		if err != nil {
			return fmt.Errorf("advance workspace journal cursor: %w", err)
		}
		observeImportedRecord(r)
		progress.remaining--
	}
	return nil
}

func observeImportedRecord(r Record) {
	if r.PreviousSegment != nil {
		observability.WorkspaceJournalRotations.Inc()
		return
	}
	observability.WorkspaceJournalRecordsImported.Inc()
	if r.GapWindow != nil {
		observability.WorkspaceJournalGapWindows.Inc()
		observability.WorkspaceJournalGapsCoalesced.Add(float64(r.GapWindow.Count - 1))
	}
}

func decodeRecord(line []byte) (Record, error) {
	var r Record
	if err := json.Unmarshal(line, &r); err != nil {
		return Record{}, err
	}
	return r, validateRecord(r)
}

func validateRecord(r Record) error {
	if r.ID == "" || r.Time.IsZero() {
		return fmt.Errorf("workspace journal record missing identity")
	}
	if link := r.PreviousSegment; link != nil {
		if link.ID == "" || link.Bytes <= 0 || r.Gap != "" || r.GapWindow != nil || r.Path != "" || r.Operation != "" {
			return fmt.Errorf("invalid workspace journal segment link")
		}
		return nil
	}
	if r.Gap == "" && (!validPath(r.Path) || !validOperation(r.Operation)) {
		return fmt.Errorf("invalid workspace journal operation")
	}
	if window := r.GapWindow; window != nil && (r.Gap != unresolvedKernelPath || window.Count == 0 ||
		window.Start.IsZero() || window.End.IsZero() || window.End.Before(window.Start)) {
		return fmt.Errorf("invalid workspace journal gap window")
	}
	return nil
}

func (i *Importer) persistObservation(ctx context.Context, r Record, scopes []Scope) error {
	for _, s := range scopes {
		rel, ok := scopePath(s.RelativeRoot, r.Path)
		if r.Gap == "" && !ok {
			continue
		}
		data := map[string]any{
			"source": "kernel-journal", "attribution": "kernel_pid", "journalId": r.ID,
			"process": map[string]any{"pid": r.PID, "start": r.ProcessStart, "namespace": r.Namespace, "bootId": r.BootID},
			"status":  "complete", "changes": []map[string]string{{"path": rel, "operation": r.Operation}},
		}
		if r.PID <= 0 {
			data["attribution"] = "unknown"
		}
		if r.Gap != "" {
			data["gap"] = true
			data["reason"] = r.Gap
			data["status"] = "partial"
			data["changes"] = []any{}
			data["attribution"] = "unknown"
			if r.GapWindow != nil {
				data["gapWindow"] = r.GapWindow
			}
		}
		ev := domain.NewEvent("workspace.observed", s.GraphID, data).WithActor("workspace-journal")
		ev.ID = r.ID + "_" + s.GraphID + "_" + s.NodeID + fmt.Sprint(s.Replica)
		ev.Time = r.Time
		if s.NodeID != "" {
			ev.WithNode(s.NodeID, s.Replica)
		}
		if err := i.Store.AppendEvent(ctx, ev); err != nil && !errors.Is(err, storage.ErrConflict) {
			return fmt.Errorf("persist workspace journal observation: %w", err)
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
