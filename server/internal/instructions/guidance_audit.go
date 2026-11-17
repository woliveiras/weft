package instructions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var errGuidanceAuditUnavailable = errors.New("guidance audit persistence unavailable")

type guidanceEvent struct {
	Read     *Entry `json:"read,omitempty"`
	Records  []int  `json:"records,omitempty"`
	Delivery string `json:"delivery,omitempty"`
	Budget   *int   `json:"budget,omitempty"`
}

func readPrefix(id string) string { return "instructions/reads/" + id + "/" }

func (s *Session) journal(ctx context.Context, event guidanceEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%08d", readPrefix(s.snapshot.ExecutionID), s.sequence+1)
	if _, err := s.manager.Store.PutSetting(ctx, key, data, 0, "runtime"); err != nil {
		// Store errors can include physical paths. The turn persists this
		// message in execution history and also uses it in traces.
		return errGuidanceAuditUnavailable
	}
	s.sequence++
	return nil
}

func (s *Session) recordRead(ctx context.Context, entry Entry, content string) error {
	if entry.Record == 0 {
		entry.Record = len(s.snapshot.Entries) + s.sequence + 1
	}
	budget := s.snapshot.Budget
	if entry.Included {
		budget -= len(renderEntry(entry, content))
	}
	if err := s.journal(ctx, guidanceEvent{Read: &entry, Budget: &budget}); err != nil {
		return err
	}
	s.reads = append(s.reads, progressiveEntry{entry: entry, content: content})
	if entry.Included {
		s.pending = append(s.pending, entry.Record)
	}
	return nil
}

func (s *Session) omitRequest(ctx context.Context, entry Entry, reason string) (string, error) {
	entry.Reason = reason
	if entry.Record == 0 {
		entry.Record = len(s.snapshot.Entries) + s.sequence + 1
	}
	if err := s.journal(ctx, guidanceEvent{Read: &entry}); err != nil {
		return "", err
	}
	return "omitted: " + reason, nil
}

// Dispatch records the intent before the provider call, so a crash leaves an
// indeterminate delivery rather than claiming the prepared bytes were sent.
func (s *Session) Dispatch(ctx context.Context) error {
	if err := s.delivery(ctx, "indeterminate", false); err != nil {
		return err
	}
	s.dispatched = true
	return nil
}

func (s *Session) Confirm(ctx context.Context) error {
	if err := s.delivery(ctx, "confirmed", true); err != nil {
		return err
	}
	s.dispatched = false
	return nil
}

func (s *Session) Finish(ctx context.Context) error {
	if s.dispatched {
		return nil
	}
	return s.delivery(ctx, "interrupted", true)
}

func (s *Session) delivery(ctx context.Context, state string, clear bool) error {
	if len(s.pending) == 0 {
		return nil
	}
	if err := s.journal(ctx, guidanceEvent{Records: s.pending, Delivery: state}); err != nil {
		return err
	}
	if clear {
		s.pending = nil
	}
	return nil
}

func (m Manager) readOutcomes(ctx context.Context, s *Snapshot) error {
	events, err := m.Store.ListSettings(ctx, readPrefix(s.ExecutionID))
	if err != nil {
		return err
	}
	index := map[int]int{}
	for _, stored := range events {
		var e guidanceEvent
		if json.Unmarshal(stored.Value, &e) != nil {
			return errors.New("invalid guidance outcome")
		}
		if e.Budget != nil {
			s.Budget = *e.Budget
		}
		if e.Read != nil {
			if i, exists := index[e.Read.Record]; exists {
				s.Reads[i] = *e.Read
			} else {
				index[e.Read.Record] = len(s.Reads)
				s.Reads = append(s.Reads, *e.Read)
			}
		}
		for _, record := range e.Records {
			if record > 0 && record <= len(s.Entries) {
				s.Entries[record-1].Delivery = e.Delivery
			} else if i, ok := index[record]; ok {
				s.Reads[i].Delivery = e.Delivery
			}
		}
	}
	return nil
}
