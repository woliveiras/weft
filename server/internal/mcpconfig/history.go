package mcpconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

type HistoryFilter struct {
	Connection, Graph, Node, Tool, Outcome, Type string
	Since, Until                                 time.Time
	Before                                       int64
	Limit                                        int
}
type HistoryEntry struct {
	Seq        int64            `json:"seq"`
	Time       time.Time        `json:"time"`
	Type       domain.EventType `json:"type"`
	Connection string           `json:"connection"`
	Graph      string           `json:"graph"`
	Node       string           `json:"node"`
	Execution  string           `json:"execution"`
	Tool       string           `json:"tool"`
	Outcome    string           `json:"outcome"`
	DurationMS int64            `json:"durationMs"`
	Approval   string           `json:"approval,omitempty"`
	Summary    string           `json:"summary"`
}
type HistoryPage struct {
	Entries    []HistoryEntry `json:"entries"`
	NextBefore int64          `json:"nextBefore,omitempty"`
	HasMore    bool           `json:"hasMore"`
}

var historyTypes = []domain.EventType{domain.EvToolInvoked, domain.EvToolCompleted, domain.EvToolDenied, domain.EvApprovalReq, domain.EvApprovalDone}

func historyEntry(e *domain.Event) (HistoryEntry, bool) {
	var data struct {
		Tool, Outcome string
		DurationMS    int64 `json:"durationMs"`
		ID, Status    string
		Detail        json.RawMessage
	}
	if json.Unmarshal(e.Data, &data) != nil {
		return HistoryEntry{}, false
	}
	switch e.Type {
	case domain.EvApprovalReq, domain.EvApprovalDone:
		var detail struct{ Tool string }
		if json.Unmarshal(data.Detail, &detail) != nil {
			return HistoryEntry{}, false
		}
		data.Tool = detail.Tool
		data.Outcome = data.Status
	case domain.EvToolInvoked, domain.EvToolCompleted, domain.EvToolDenied:
	default:
		return HistoryEntry{}, false
	}
	parts := strings.Split(data.Tool, ".")
	if len(parts) != 3 || parts[0] != "mcp" || !identifier.MatchString(parts[1]) || !toolName.MatchString(parts[2]) {
		return HistoryEntry{}, false
	}
	summary := map[string]string{"started": "Call started", "ok": "Call completed successfully", "error": "Call failed; remote outcome may be unknown", "denied": "Call denied", "unknown": "Tool unavailable", "rate_limited": "Call quota exceeded", "rejected": "Approval rejected or unavailable", "pending": "Approval required", "approved": "Approval granted", "expired": "Approval expired"}[data.Outcome]
	if summary == "" {
		summary = "Operation finished"
		data.Outcome = "unknown"
	}
	return HistoryEntry{Seq: e.Seq, Time: e.Time, Type: e.Type, Connection: parts[1], Graph: e.GraphID, Node: e.NodeID, Execution: e.ExecutionID, Tool: data.Tool, Outcome: data.Outcome, DurationMS: data.DurationMS, Approval: data.ID, Summary: summary}, true
}
func (m *Manager) History(ctx context.Context, f HistoryFilter) (HistoryPage, error) {
	out := HistoryPage{Entries: []HistoryEntry{}}
	if f.Limit < 1 || f.Limit > 100 || f.Before < 0 || (f.Connection != "" && !identifier.MatchString(f.Connection)) || len(f.Graph) > 128 || len(f.Node) > 128 || len(f.Tool) > 200 || (!f.Since.IsZero() && !f.Until.IsZero() && f.Since.After(f.Until)) {
		return out, fmt.Errorf("%w: invalid history filter", ErrInvalid)
	}
	kinds := historyTypes
	if f.Type != "" {
		found := false
		for _, kind := range kinds {
			if string(kind) == f.Type {
				found = true
			}
		}
		if !found {
			return out, fmt.Errorf("%w: invalid history event type", ErrInvalid)
		}
		kinds = []domain.EventType{domain.EventType(f.Type)}
	}
	if f.Outcome != "" && !strings.Contains("|started|ok|error|denied|unknown|rate_limited|rejected|pending|approved|expired|", "|"+f.Outcome+"|") {
		return out, fmt.Errorf("%w: invalid history outcome", ErrInvalid)
	}
	// A bounded scan avoids unbounded work for sparse filters. The cursor advances
	// even on empty pages; clients must honor hasMore rather than infer exhaustion.
	events, err := m.Store.ListEvents(ctx, storage.EventFilter{GraphID: f.Graph, NodeID: f.Node, Types: kinds, BeforeSeq: f.Before, Limit: 501})
	if err != nil {
		return out, err
	}
	for i, e := range events {
		if i == 500 {
			out.HasMore = true
			break
		}
		out.NextBefore = e.Seq
		entry, ok := historyEntry(e)
		if ok && (f.Connection == "" || entry.Connection == f.Connection) && (f.Tool == "" || entry.Tool == f.Tool) && (f.Outcome == "" || entry.Outcome == f.Outcome) && (f.Since.IsZero() || !e.Time.Before(f.Since)) && (f.Until.IsZero() || !e.Time.After(f.Until)) {
			out.Entries = append(out.Entries, entry)
		}
		if len(out.Entries) == f.Limit {
			out.HasMore = i+1 < len(events)
			break
		}
	}
	if !out.HasMore {
		out.NextBefore = 0
	}
	return out, nil
}
