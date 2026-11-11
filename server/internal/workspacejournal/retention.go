package workspacejournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/storage"
)

const DefaultKeepArchives = 4

// The boundary is committed before removing its immediate predecessor. If
// that archive still exists, the next apply can finish the interrupted prune.
type retentionAnchor struct {
	Version  int        `json:"version"`
	First    segmentRef `json:"first"`
	Previous segmentRef `json:"previous"`
	Cursor   cursor     `json:"cursor"`
	Revision int64      `json:"revision"`
}

func anchorPath(path string) string { return path + ".anchor.json" }

func readAnchor(path string) (*retentionAnchor, error) {
	name := anchorPath(path)
	info, err := regularFileInfo(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workspace journal retention anchor: %w", err)
	}
	if info.Size() > maxRecordBytes {
		return nil, fmt.Errorf("workspace journal retention anchor exceeds size limit")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open workspace journal retention anchor: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRecordBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read workspace journal retention anchor: %w", err)
	}
	if len(data) > maxRecordBytes {
		return nil, fmt.Errorf("workspace journal retention anchor exceeds size limit")
	}
	var a retentionAnchor
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("decode workspace journal retention anchor: %w", err)
	}
	if a.Version != 1 || a.First.ID == "" || a.First.Bytes <= 0 || a.Previous.ID == "" || a.Previous.Bytes <= 0 ||
		a.First.ID == a.Previous.ID || a.Cursor.JournalID == "" || a.Cursor.Offset <= 0 || a.Revision <= 0 {
		return nil, fmt.Errorf("invalid workspace journal retention anchor")
	}
	return &a, nil
}

func (a *retentionAnchor) validateChain(chain []segment) error {
	if a == nil {
		return nil
	}
	first := chain[0]
	if first.ID != a.First.ID || first.Bytes < a.First.Bytes || !sameLink(first.previous, &a.Previous) ||
		segmentIndex(chain, a.Previous.ID) >= 0 {
		return fmt.Errorf("workspace journal retention anchor does not match the oldest retained segment")
	}
	floor := segmentIndex(chain, a.Cursor.JournalID)
	if floor < 0 || a.Cursor.Offset > chain[floor].Bytes {
		return fmt.Errorf("workspace journal retention checkpoint is outside the retained chain")
	}
	return nil
}

func (a *retentionAnchor) validateCursor(chain []segment, saved cursor, revision int64) error {
	if a == nil {
		return nil
	}
	position := segmentIndex(chain, saved.JournalID)
	floor := segmentIndex(chain, a.Cursor.JournalID)
	if revision < a.Revision || position < floor || position == floor && saved.Offset < a.Cursor.Offset {
		return fmt.Errorf("workspace journal cursor predates the retention checkpoint; restore a matching store and journal")
	}
	return nil
}

type PrunePlan struct {
	Archives []string
	Bytes    int64
}

// Prune previews or removes a prefix strictly before the durable cursor. An
// apply takes the writer lock and retains the requested number of newest
// archives in addition to any segment still needed by the cursor.
func Prune(ctx context.Context, store storage.Store, path string, keep int, apply bool) (_ PrunePlan, err error) {
	if path == "" {
		return PrunePlan{}, fmt.Errorf("workspace journal path is required")
	}
	if keep < 0 {
		return PrunePlan{}, fmt.Errorf("journal archive retention must be nonnegative")
	}
	if apply {
		unlock, err := lockWriter(path + ".lock")
		if err != nil {
			return PrunePlan{}, fmt.Errorf("lock workspace journal for pruning: %w", err)
		}
		defer func() { err = errors.Join(err, unlock()) }()
	}
	if _, err := regularFileInfo(path); err != nil {
		return PrunePlan{}, fmt.Errorf("inspect active workspace journal: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return PrunePlan{}, fmt.Errorf("open active workspace journal: %w", err)
	}
	defer f.Close()
	size, err := segmentSize(f)
	if err != nil {
		return PrunePlan{}, err
	}
	if size == 0 {
		return PrunePlan{}, fmt.Errorf("workspace journal has no active segment identity")
	}
	if err := requireCompleteTail(f, size); err != nil {
		return PrunePlan{}, err
	}
	active, err := readSegment(f, size)
	if err != nil {
		return PrunePlan{}, err
	}
	anchor, err := readAnchor(path)
	if err != nil {
		return PrunePlan{}, err
	}
	chain, err := readChain(ctx, path, active, anchor)
	if err != nil {
		return PrunePlan{}, err
	}
	saved, revision, err := loadCursor(ctx, store, cursorKey(path))
	if err != nil {
		return PrunePlan{}, err
	}
	if err := anchor.validateCursor(chain, saved, revision); err != nil {
		return PrunePlan{}, err
	}
	position := segmentIndex(chain, saved.JournalID)
	if position < 0 {
		return PrunePlan{}, fmt.Errorf("workspace journal pruning requires a durable cursor in the retained chain")
	}
	cursorFile := f
	if saved.JournalID != active.ID {
		cursorFile, _, err = openArchive(path, chain[position].segmentRef)
		if err != nil {
			return PrunePlan{}, err
		}
		defer cursorFile.Close()
	}
	if err := validateCursorOffset(cursorFile, chain[position].Bytes, saved.Offset); err != nil {
		return PrunePlan{}, err
	}
	var plan PrunePlan
	pending, err := pendingPrune(path, anchor)
	if err != nil {
		return plan, err
	}
	if pending {
		plan.Archives = append(plan.Archives, archivePath(path, anchor.Previous.ID))
		plan.Bytes += anchor.Previous.Bytes
	}
	count := min(position, max(0, len(chain)-1-keep))
	for _, s := range chain[:count] {
		plan.Archives = append(plan.Archives, archivePath(path, s.ID))
		plan.Bytes += s.Bytes
	}
	if !apply {
		return plan, nil
	}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if pending {
		// A previous command may have returned after rename but before the
		// directory sync succeeded. Establish durability before the unlink.
		if err := syncJournalDirectory(path); err != nil {
			return plan, err
		}
		if err := removeArchive(path, anchor.Previous); err != nil {
			return plan, err
		}
	}
	for index, s := range chain[:count] {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		boundary := retentionAnchor{Version: 1, First: chain[index+1].segmentRef, Previous: s.segmentRef, Cursor: saved, Revision: revision}
		if err := writeAnchor(path, boundary); err != nil {
			return plan, fmt.Errorf("commit workspace journal retention anchor: %w", err)
		}
		if err := removeArchive(path, s.segmentRef); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

func pendingPrune(path string, a *retentionAnchor) (bool, error) {
	if a == nil {
		return false, nil
	}
	f, _, err := openArchive(path, a.Previous)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, f.Close()
}

func writeAnchor(path string, a retentionAnchor) (err error) {
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if len(data) > maxRecordBytes {
		return fmt.Errorf("workspace journal retention anchor exceeds size limit")
	}
	temp, err := os.CreateTemp(filepath.Dir(path), stagePrefix(path)+"anchor-*")
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			err = errors.Join(err, os.Remove(temp.Name()))
		}
	}()
	if err := temp.Chmod(0640); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := errors.Join(writeAndSync(temp, data), temp.Close()); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), anchorPath(path)); err != nil {
		return err
	}
	published = true
	return syncJournalDirectory(path)
}

func removeArchive(path string, ref segmentRef) error {
	f, _, err := openArchive(path, ref)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Remove(archivePath(path, ref.ID)); err != nil {
		return fmt.Errorf("remove retired workspace journal segment %q: %w", ref.ID, err)
	}
	return syncJournalDirectory(path)
}
