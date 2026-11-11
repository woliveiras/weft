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
}

func archivePath(path, id string) string {
	return fmt.Sprintf("%s%s%x", path, archiveSuffix, sha256.Sum256([]byte(id)))
}

func requireNoArchives(path string) error {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	prefix := filepath.Base(path) + archiveSuffix
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return fmt.Errorf("workspace journal has archives but no active segment identity; preserve the chain for recovery")
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
	return segment{segmentRef{ID: r.ID, Bytes: size}, r.PreviousSegment}, nil
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
	f, err := os.Open(archivePath(path, want.ID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, segment{}, fmt.Errorf("workspace journal chain is missing segment %q: %w", want.ID, err)
	}
	if err != nil {
		return nil, segment{}, fmt.Errorf("open workspace journal segment %q: %w", want.ID, err)
	}
	s, err := inspectArchive(f, want)
	if err != nil {
		f.Close()
		return nil, segment{}, fmt.Errorf("invalid workspace journal archive %q: %w", want.ID, err)
	}
	return f, s, nil
}

func inspectArchive(f *os.File, want segmentRef) (segment, error) {
	size, err := segmentSize(f)
	if err != nil {
		return segment{}, err
	}
	if size != want.Bytes {
		return segment{}, fmt.Errorf("segment size changed: got %d bytes, expected %d", size, want.Bytes)
	}
	if err := requireCompleteTail(f, size); err != nil {
		return segment{}, err
	}
	s, err := readSegment(f, size)
	if err != nil {
		return segment{}, err
	}
	if s.ID != want.ID {
		return segment{}, fmt.Errorf("segment identity changed")
	}
	return s, nil
}

// readChain follows the links from a snapshot of the active segment. It keeps
// only metadata for the archives, so the number of open files stays bounded.
func readChain(ctx context.Context, path string, active segment) ([]segment, error) {
	chain := []segment{active}
	seen := map[string]bool{active.ID: true}
	for link := active.previous; link != nil; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
		link = s.previous
	}
	slices.Reverse(chain)
	return chain, nil
}
