package eventtest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestValidateEvent(t *testing.T) {
	schema, err := eventSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data string
		ok   bool
	}{
		{"valid", `{"scope":"run","id":"task-1","action":"hold","outcome":"paused"}`, true},
		{"unknown data field", `{"scope":"run","id":"task-1","action":"hold","outcome":"paused","unexpected":true}`, false},
		{"missing blocker", `{"scope":"run","id":"task-1","action":"resume","outcome":"budget_reached"}`, false},
		{"wrong type", `{"scope":"run","id":42,"action":"hold","outcome":"paused"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := domain.NewEvent(domain.EvBudgetGraphTransition, "task-1", json.RawMessage(tc.data))
			ev.Seq = 1
			if err := validateEvent(schema, ev); (err == nil) != tc.ok {
				t.Fatalf("valid = %t, want %t: %v", err == nil, tc.ok, err)
			}
		})
	}
}

func TestTaskAuthorizationDecisionEvent(t *testing.T) {
	schema, err := eventSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data  string
		valid bool
	}{
		{`{"tool":"scm.security.alerts","outcome":"tsk_fixture:1:full","durationMs":0}`, true},
		{`{"tool":"scm.security.alerts","outcome":42,"durationMs":0}`, false},
		{`{"tool":"scm.security.alerts","outcome":"legacy"}`, false},
		{`{"tool":"scm.security.alerts","outcome":"legacy","durationMs":0,"args":{"secret":"forbidden"}}`, false},
	} {
		e := domain.NewEvent("task.authorization_decision", "fixture", json.RawMessage(tc.data))
		e.Seq = 1
		if err := validateEvent(schema, e); (err == nil) != tc.valid {
			t.Fatalf("valid=%t: %v", tc.valid, err)
		}
	}
}

func TestEventSchemaClosesEnvelopeAndProposalBlock(t *testing.T) {
	schema, err := eventSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"seq":1,"id":"evt_example","type":"graph.deleted","time":"2026-11-13T12:00:00Z","extra":true}`,
		`{"seq":1,"id":"evt_example","type":"environment.proposal","time":"2026-11-13T12:00:00Z","data":{"proposal":"apr","state":"ready","resume":false,"startBlocked":{"scope":"run","id":"graph","extra":true}}}`,
	} {
		var wire any
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(wire); err == nil {
			t.Fatalf("accepted unexpected field: %s", raw)
		}
	}
}

func TestOpenEventSchemaFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "server", "internal", "test", "nested")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := openEventSchema(deep); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing schema: %v", err)
	}
	dir := filepath.Join(root, "schemas")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "event.schema.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openEventSchema(deep)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if file.Name() != filepath.Join(dir, "event.schema.json") {
		t.Fatalf("opened %s", file.Name())
	}
}

func TestBlockedExecutionRequiresFailureAndSummary(t *testing.T) {
	schema, err := eventSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, data string
		ok         bool
	}{
		{"blocked", `{"signal":"blocked","status":"failed","summary":"Registry unavailable"}`, true},
		{"not successful", `{"signal":"blocked","status":"succeeded","summary":"Registry unavailable"}`, false},
		{"reason required", `{"signal":"blocked","status":"failed"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := domain.NewEvent("execution.failed", "task-run-fixture", json.RawMessage(tc.data))
			ev.Seq = 1
			if err := validateEvent(schema, ev); (err == nil) != tc.ok {
				t.Fatalf("valid = %t, want %t: %v", err == nil, tc.ok, err)
			}
		})
	}
}
