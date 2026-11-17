package workspacejournal

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const (
	recordBufferBytes = 64 << 10
	archiveSuffix     = ".segment-"
)

type segmentRef struct {
	ID    string `json:"id"`
	Bytes int64  `json:"bytes"`
}

type segment struct {
	segmentRef
	previous *segmentRef
	info     os.FileInfo // archives only
}

func archivePath(path, id string) string {
	return fmt.Sprintf("%s%s%x", path, archiveSuffix, sha256.Sum256([]byte(id)))
}

func stagePrefix(path string) string {
	return "." + filepath.Base(path) + ".next-"
}

func requireNoArchives(path string) error {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	prefix := filepath.Base(path) + archiveSuffix
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) || strings.HasPrefix(entry.Name(), stagePrefix(path)) || entry.Name() == filepath.Base(anchorPath(path)) {
			return fmt.Errorf("workspace journal has rotation files but no active segment identity; preserve the chain for recovery")
		}
	}
	return nil
}

func segmentSize(f *os.File) (int64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("workspace journal segment must be a regular file")
	}
	return info.Size(), nil
}

func readSegment(f *os.File, size int64) (segment, error) {
	// Most identities fit in the initial buffer. Bound the read even if a
	// corrupt file lacks a newline.
	reader := bufio.NewReaderSize(io.NewSectionReader(f, 0, min(size, recordBufferBytes)), 512)
	first, err := reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF && size >= recordBufferBytes {
			return segment{}, fmt.Errorf("workspace journal initial record exceeds limit")
		}
		return segment{}, err
	}
	r, err := decodeRecord(first)
	if err != nil {
		return segment{}, fmt.Errorf("invalid journal identity: %w", err)
	}
	return segment{segmentRef: segmentRef{ID: r.ID, Bytes: size}, previous: r.PreviousSegment}, nil
}

func requireCompleteTail(f *os.File, size int64) error {
	var last [1]byte
	if _, err := f.ReadAt(last[:], size-1); err != nil {
		return fmt.Errorf("read workspace journal tail: %w", err)
	}
	if last[0] != '\n' {
		return fmt.Errorf("journal ends with an incomplete record; preserve it for recovery")
	}
	return nil
}

func openArchive(path string, want segmentRef) (*os.File, segment, error) {
	name := archivePath(path, want.ID)
	if _, err := regularFileInfo(name); err != nil {
		return nil, segment{}, archiveError(want.ID, err)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, segment{}, archiveError(want.ID, err)
	}
	s, err := inspectArchive(f, want)
	if err != nil {
		f.Close()
		return nil, segment{}, fmt.Errorf("invalid workspace journal archive %q: %w", want.ID, err)
	}
	return f, s, nil
}

func archiveError(id string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("workspace journal chain is missing segment %q: %w", id, err)
	}
	return fmt.Errorf("inspect workspace journal archive %q: %w", id, err)
}

func inspectArchive(f *os.File, want segmentRef) (segment, error) {
	info, err := f.Stat()
	if err != nil {
		return segment{}, err
	}
	if err := checkArchiveSize(info, want.Bytes); err != nil {
		return segment{}, err
	}
	if err := requireCompleteTail(f, info.Size()); err != nil {
		return segment{}, err
	}
	s, err := readSegment(f, info.Size())
	if err != nil {
		return segment{}, err
	}
	if s.ID != want.ID {
		return segment{}, fmt.Errorf("segment identity changed")
	}
	s.info = info
	return s, nil
}

func checkArchiveSize(info os.FileInfo, expected int64) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("workspace journal archive must be a regular file")
	}
	if size := info.Size(); size != expected {
		return fmt.Errorf("segment size changed: got %d bytes, expected %d", size, expected)
	}
	return nil
}

// readChain follows the links from a snapshot of the active segment. It keeps
// only metadata for the archives, so the number of open files stays bounded.
func readChain(ctx context.Context, path string, active segment, anchor *retentionAnchor) ([]segment, error) {
	chain := []segment{active}
	seen := map[string]bool{active.ID: true}
	for current := active; current.previous != nil; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if anchor != nil && current.ID == anchor.First.ID {
			break
		}
		link := current.previous
		if seen[link.ID] {
			return nil, fmt.Errorf("workspace journal chain has a cycle at segment %q", link.ID)
		}
		f, s, err := openArchive(path, *link)
		if err != nil {
			return nil, err
		}
		f.Close()
		seen[s.ID] = true
		chain = append(chain, s)
		current = s
	}
	slices.Reverse(chain)
	return chain, anchor.validateChain(chain)
}

func segmentIndex(chain []segment, id string) int {
	return slices.IndexFunc(chain, func(s segment) bool { return s.ID == id })
}

type chainCache struct {
	mu       sync.Mutex
	snapshot *chainSnapshot
}

type chainSnapshot struct {
	path     string
	active   segment
	anchor   *retentionAnchor
	archives []segment
}

// load reuses parsed archive links across appends to the same active file.
// Metadata checks still detect missing, replaced or truncated archives on
// every pass, even for segments the cursor has already passed.
func (c *chainCache) load(ctx context.Context, path string, active segment, anchor *retentionAnchor) ([]segment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached := c.snapshot
	sameActive := cached != nil && cached.path == path && cached.active.ID == active.ID && sameLink(cached.active.previous, active.previous)
	if sameActive && sameAnchor(cached.anchor, anchor) {
		unchanged, err := archivesUnchanged(ctx, path, cached.archives)
		if err != nil {
			return nil, err
		}
		if unchanged {
			chain := append(slices.Clone(cached.archives), active)
			return chain, anchor.validateChain(chain)
		}
	}
	chain, err := readChain(ctx, path, active, anchor)
	if err != nil {
		return nil, err
	}
	// The active name may become visible before the writer's directory
	// fsync. Persist it before the importer can commit a successor cursor.
	if !sameActive && active.previous != nil {
		if err := syncJournalDirectory(path); err != nil {
			return nil, err
		}
	}
	c.snapshot = &chainSnapshot{path: path, active: active, anchor: anchor, archives: chain[:len(chain)-1]}
	return chain, nil
}

func archivesUnchanged(ctx context.Context, path string, archives []segment) (bool, error) {
	for _, s := range archives {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		info, err := regularFileInfo(archivePath(path, s.ID))
		if err != nil {
			return false, archiveError(s.ID, err)
		}
		if err := checkArchiveSize(info, s.Bytes); err != nil {
			return false, fmt.Errorf("invalid workspace journal archive %q: %w", s.ID, err)
		}
		if !os.SameFile(info, s.info) || !info.ModTime().Equal(s.info.ModTime()) {
			return false, nil
		}
	}
	return true, nil
}

func sameAnchor(a, b *retentionAnchor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
