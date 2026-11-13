package eventtest

import (
	"encoding/json"
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
