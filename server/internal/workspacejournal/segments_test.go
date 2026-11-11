package workspacejournal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReadChainRejectsCycle(t *testing.T) {
	ref := segmentRef{ID: "loop", Bytes: 128}
	active := segment{segmentRef: ref, previous: &ref}
	if _, err := readChain(context.Background(), "events", active); err == nil || !strings.Contains(err.Error(), "chain has a cycle") {
		t.Fatalf("cyclic chain: %v", err)
	}
}

func TestDecodeRecordRejectsInvalidGapWindowsAndLinks(t *testing.T) {
	now := time.Now().UTC()
	for _, malformed := range []Record{
		{Gap: unresolvedKernelPath, GapWindow: &gapWindow{Start: now, End: now}},
		{Gap: unresolvedKernelPath, GapWindow: &gapWindow{Count: 2, Start: now.Add(time.Second), End: now}},
		{Gap: "kernel_queue_overflow", GapWindow: &gapWindow{Count: 2, Start: now, End: now}},
		{PreviousSegment: &segmentRef{ID: "previous"}},
		{PreviousSegment: &segmentRef{ID: "previous", Bytes: 100}, Gap: unresolvedKernelPath},
	} {
		malformed.ID, malformed.Time = "record", now
		data, err := json.Marshal(malformed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeRecord(data); err == nil {
			t.Fatalf("accepted malformed record %s", data)
		}
	}
}
