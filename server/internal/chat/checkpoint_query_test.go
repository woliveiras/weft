package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/workspace"
)

func TestCheckpointQueryScopeAndArguments(t *testing.T) {
	for raw, valid := range map[string]bool{
		`{"target":"docs-review"}`: true, `{"target":"tsk_example","repository":"docs"}`: true,
		`{"target":"tsk_!"}`: false, `{"target":"../elsewhere"}`: false, `{"target":"docs","repository":"../../repo"}`: false,
		`{"target":"docs","restore":true}`: false, `{"target":"docs","target":"other"}`: false,
	} {
		_, err := ParseCheckpointQuery([]byte(raw))
		if (err == nil) != valid {
			t.Errorf("parse %s: %v", raw, err)
		}
	}
}

func TestCheckpointRowsBoundAndProjectWorkspaceMetadata(t *testing.T) {
	var items []workspace.Checkpoint
	for i := 10; i > 0; i-- {
		items = append(items, workspace.Checkpoint{ID: fmt.Sprintf("worker/r0/%d", i), NodeID: "worker", Reason: "before_edit", Ref: "internal-ref", Tree: "internal-tree"})
	}
	rows, err := RecentCheckpointRows(items)
	if err != nil || len(rows) != InspectionLimit || rows[0].ID != "worker/r0/10" || rows[7].ID != "worker/r0/3" {
		t.Fatalf("latest checkpoints: %+v %v", rows, err)
	}
	b, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(b), "internal-") {
		t.Fatalf("Git internals in observation: %s %v", b, err)
	}
	items[0].Tool = "bad\nfield"
	if _, err = RecentCheckpointRows(items); err != ErrCheckpointsUnavailable {
		t.Fatalf("unsafe recording: %v", err)
	}
}
