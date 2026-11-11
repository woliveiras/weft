package workspacejournal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetentionAnchorRejectsStaleCursor(t *testing.T) {
	anchor := &retentionAnchor{
		Version: 1, First: segmentRef{ID: "first", Bytes: 100}, Previous: segmentRef{ID: "pruned", Bytes: 90},
		Cursor: cursor{JournalID: "second", Offset: 40}, Revision: 9,
	}
	chain := []segment{
		{segmentRef: anchor.First, previous: &anchor.Previous},
		{segmentRef: segmentRef{ID: "second", Bytes: 200}, previous: &anchor.First},
		{segmentRef: segmentRef{ID: "third", Bytes: 210}},
	}
	if err := anchor.validateChain(chain); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		saved    cursor
		revision int64
		valid    bool
	}{
		{cursor{}, 0, false},
		{cursor{JournalID: "pruned", Offset: 90}, 10, false},
		{cursor{JournalID: "first", Offset: 100}, 10, false},
		{cursor{JournalID: "second", Offset: 39}, 10, false},
		{cursor{JournalID: "second", Offset: 40}, 8, false},
		{cursor{JournalID: "second", Offset: 40}, 9, true},
		{cursor{JournalID: "third", Offset: 1}, 10, true},
	} {
		if err := anchor.validateCursor(chain, tc.saved, tc.revision); (err == nil) != tc.valid {
			t.Errorf("cursor %+v at revision %d: %v", tc.saved, tc.revision, err)
		}
	}
	chain[0].previous = &segmentRef{ID: "pruned", Bytes: 89}
	if err := anchor.validateChain(chain); err == nil {
		t.Fatal("accepted anchor with a mismatched predecessor link")
	}
}

func TestReadAnchorRejectsCorruptMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events")
	for _, data := range []string{`{}`, `{"version":1`, strings.Repeat("x", maxRecordBytes+1)} {
		if err := os.WriteFile(anchorPath(path), []byte(data), 0640); err != nil {
			t.Fatal(err)
		}
		if _, err := readAnchor(path); err == nil {
			t.Fatal("accepted corrupt retention anchor")
		}
	}
}
