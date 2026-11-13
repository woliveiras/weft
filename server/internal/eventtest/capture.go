// Package eventtest captures events from integration tests for schema validation.
package eventtest

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

// Capture checks the expected event types and validates all stored wire
// events, including dynamically named events. It optionally exports them.
func Capture(t *testing.T, store storage.Store, expected ...domain.EventType) {
	t.Helper()
	events := validateStored(t, t.Context(), store)
	for _, kind := range expected {
		found := false
		for _, event := range events {
			if event.Type == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a stored %s event", kind)
		}
	}
	export(t, events)
}

// ValidateStored checks the complete persisted log after its producers stop.
func ValidateStored(t *testing.T, store storage.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
	defer cancel()
	validateStored(t, ctx, store)
}

func validateStored(t *testing.T, ctx context.Context, store storage.Store) []*domain.Event {
	t.Helper()
	filter := storage.EventFilter{Ascending: true, Limit: 500}
	var events []*domain.Event
	for {
		page, err := store.ListEvents(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, page...)
		if len(page) < filter.Limit {
			break
		}
		filter.AfterSeq = page[len(page)-1].Seq
	}
	schema, err := eventSchema()
	if err != nil {
		t.Fatalf("compile event schema: %v", err)
	}
	for _, event := range events {
		if err := validateEvent(schema, event); err != nil {
			t.Errorf("event %s (sequence %d): %v", event.Type, event.Seq, err)
		}
	}
	return events
}

func export(t *testing.T, events []*domain.Event) {
	t.Helper()
	dir := os.Getenv("WEFT_TEST_EVENT_CAPTURE")
	if dir == "" {
		return
	}
	file, err := os.CreateTemp(dir, "events-*.json")
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := json.NewEncoder(file).Encode(struct {
		Test   string          `json:"test"`
		Events []*domain.Event `json:"events"`
	}{t.Name(), events})
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		t.Fatalf("capture events: %v %v", encodeErr, closeErr)
	}
}
