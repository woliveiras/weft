package chat

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

type ObserveTask func(context.Context, string) (TaskSummary, error)

// Reconcile advances each watch using the authoritative task view. An event
// hint or failed lookup cannot produce a successful or terminal notification.
func (s Sessions) Reconcile(ctx context.Context, owner, id string, observe ObserveTask) (*Session, error) {
	session, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	for _, watch := range session.Watches {
		if watch.Complete {
			continue
		}
		state, err := observe(ctx, watch.Task.ID)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil && state.ID != watch.Task.ID {
			return nil, errors.New("chat task observation did not match watch")
		}
		session, err = s.recordObservation(ctx, owner, id, watch, state, err != nil)
		if err != nil {
			return nil, err
		}
	}
	return session, nil
}

func (s Sessions) recordObservation(ctx context.Context, owner, id string, prior Watch, state TaskSummary, unknown bool) (*Session, error) {
	return s.update(ctx, owner, id, func(session *Session) (bool, error) {
		for i := range session.Watches {
			watch := &session.Watches[i]
			// A later observer or overlapping browser query may already
			// have committed a transition. Discard the older read.
			if watch.RequestID != prior.RequestID || !reflect.DeepEqual(*watch, prior) || watch.Complete {
				continue
			}
			if unknown == watch.Unknown && (unknown || reflect.DeepEqual(state, watch.Task)) {
				return false, nil
			}
			watch.Unknown = unknown
			if unknown {
				return true, nil
			}
			now := time.Now().UTC()
			watch.Observed = now
			watch.Task = state
			if state.DecisionNeeded && !prior.Task.DecisionNeeded {
				session.LastActivity = now
			}
			if state.Terminal() {
				watch.Complete = true
				session.Messages = append(session.Messages, Message{
					ID: "result-" + watch.RequestID, Role: "assistant", Text: fmt.Sprintf("Task %s: %s.", state.ID, state.State),
					Task: &state, Unread: true, Created: now,
				})
				session.LastActivity = now
			}
			return true, nil
		}
		return false, nil
	})
}

// ReconcilePage bounds the number of persisted sessions visited per tick.
// Only eligible owners get detached observations. An absent cursor resumes
// from the beginning. Reads by an authorized owner may reconcile immediately.
func (s Sessions) ReconcilePage(ctx context.Context, cursor string, allowed func(string) bool, observe ObserveTask, review func(context.Context, string, *Session) (*Session, error)) (string, error) {
	const pageSize = 32
	index, err := s.index()
	if err != nil {
		return cursor, err
	}
	page, err := index.ListWatchedChats(ctx, cursor, pageSize)
	if err != nil {
		return cursor, err
	}
	var passErr error
	for _, summary := range page {
		cursor = summary.Key
		if !allowed(summary.Owner) {
			continue
		}
		session, err := s.Reconcile(ctx, summary.Owner, summary.ID, observe)
		if err == nil && review != nil {
			_, err = review(ctx, summary.Owner, session)
		}
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			passErr = errors.Join(passErr, err)
		}
		if ctx.Err() != nil {
			return cursor, ctx.Err()
		}
	}
	if len(page) < pageSize {
		cursor = ""
	}
	return cursor, passErr
}
