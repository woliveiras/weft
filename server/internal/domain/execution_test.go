package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExecutionRuntimeStartedAtPreservesLegacyAndStartedRecords(t *testing.T) {
	var legacy Execution
	if err := json.Unmarshal([]byte(`{"id":"legacy","startedAt":"2026-11-20T09:00:00Z"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.RuntimeStartedAt != nil {
		t.Fatal("legacy execution acquired a runtime start")
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["runtimeStartedAt"]; exists {
		t.Fatal("an unstarted runtime emitted the optional field")
	}
	started := time.Date(2026, 11, 20, 9, 1, 2, 345, time.UTC)
	legacy.RuntimeStartedAt = &started
	encoded, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var restored Execution
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.RuntimeStartedAt == nil || !restored.RuntimeStartedAt.Equal(started) {
		t.Fatalf("runtime start lost in the execution record: %+v", restored)
	}
}
