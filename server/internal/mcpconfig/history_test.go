package mcpconfig

import (
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/domain"
	"testing"
	"time"
)

func TestHistoryProjectionRedactsPayloads(t *testing.T) {
	for _, kind := range []domain.EventType{domain.EvToolCompleted, domain.EvApprovalReq, domain.EvApprovalDone} {
		event := domain.NewEvent(kind, "g", map[string]any{"tool": "mcp.team.search", "outcome": "error", "error": "synthetic-secret", "args": "synthetic-secret", "content": "synthetic-secret", "status": "approved", "id": "approval", "detail": map[string]any{"tool": "mcp.team.search", "args": "synthetic-secret"}, "comment": "synthetic-secret"})
		entry, ok := historyEntry(event)
		if !ok {
			t.Fatal(kind)
		}
		raw, _ := json.Marshal(entry)
		if containsCredential(raw, "synthetic-secret") {
			t.Fatal(string(raw))
		}
	}
	for _, data := range []string{`bad`, `{"tool":"filesystem.read"}`, `{"tool":"mcp.*.read"}`} {
		if _, ok := historyEntry(&domain.Event{Type: domain.EvToolCompleted, Data: json.RawMessage(data)}); ok {
			t.Fatal(data)
		}
	}
}
func TestHistoryFiltersPaginationAndSparseScan(t *testing.T) {
	m := testManager(t)
	now := time.Now().UTC()
	for i := 0; i < 505; i++ {
		name := "mcp.other.search"
		if i < 2 {
			name = "mcp.team.search"
		}
		event := domain.NewEvent(domain.EvToolCompleted, "g", map[string]any{"tool": name, "outcome": "ok"})
		event.Time = now
		event.NodeID = "worker"
		if err := m.Store.AppendEvent(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	f := HistoryFilter{Connection: "team", Limit: 1, Graph: "g", Node: "worker", Tool: "mcp.team.search", Outcome: "ok", Since: now, Until: now}
	page, err := m.History(t.Context(), f)
	if err != nil || len(page.Entries) != 0 || !page.HasMore || page.NextBefore == 0 {
		t.Fatal(page, err)
	}
	f.Before = page.NextBefore
	page, err = m.History(t.Context(), f)
	if err != nil || len(page.Entries) != 1 || !page.HasMore {
		t.Fatal(page, err)
	}
	f.Before = page.NextBefore
	page, err = m.History(t.Context(), f)
	if err != nil || len(page.Entries) != 1 || page.HasMore {
		t.Fatal(page, err)
	}
	for _, bad := range []HistoryFilter{{Limit: 0}, {Limit: 101}, {Limit: 1, Before: -1}, {Limit: 1, Connection: "*"}, {Limit: 1, Outcome: "bad"}, {Limit: 1, Type: "log"}, {Limit: 1, Since: now.Add(time.Hour), Until: now}} {
		if _, err = m.History(t.Context(), bad); !errors.Is(err, ErrInvalid) {
			t.Fatal(bad, err)
		}
	}
	if err = m.Store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = m.History(t.Context(), HistoryFilter{Limit: 10}); err == nil {
		t.Fatal("storage failure hidden")
	}
}
