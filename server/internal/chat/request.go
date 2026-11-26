package chat

import (
	"context"
	"errors"
	"time"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

const maxUserMessages = 100

var ErrMessageLimit = errors.New("start a new conversation to continue")

func (s Sessions) Append(ctx context.Context, owner, id, requestID, text string) (*Session, error) {
	if !requestIDPattern.MatchString(requestID) {
		return nil, ErrInvalidID
	}
	if err := ValidateMessage(text); err != nil {
		return nil, err
	}
	return s.update(ctx, owner, id, func(session *Session) (bool, error) {
		n := 0
		for _, message := range session.Messages {
			if message.Role != "user" {
				continue
			}
			n++
			if message.ID == requestID {
				if message.Text != text {
					return false, storage.ErrConflict
				}
				return false, nil
			}
			if message.State == "pending" || message.State == "interpreting" {
				return false, storage.ErrConflict
			}
		}
		if n >= maxUserMessages {
			return false, ErrMessageLimit
		}
		now := time.Now().UTC()
		session.LastActivity = now
		session.Messages = append(session.Messages, Message{ID: requestID, Role: "user", Text: text, State: "pending", Created: now})
		return true, nil
	})
}

// ClaimInterpretation creates a fenced ownership interval before a provider
// call. An expired claim requires a fresh explicit retry. Stale callers cannot
// overwrite newer results even when the earlier model call returns late.
func (s Sessions) ClaimInterpretation(ctx context.Context, owner, id, requestID string, retry bool) (*Session, time.Time, error) {
	var claim time.Time
	session, err := s.update(ctx, owner, id, func(session *Session) (bool, error) {
		claim = time.Time{}
		now := time.Now().UTC()
		for i := range session.Messages {
			message := &session.Messages[i]
			if message.Role != "user" {
				continue
			}
			if message.ID != requestID {
				if message.State == "pending" || message.State == "interpreting" {
					return false, storage.ErrConflict
				}
				continue
			}
		}
		for i := range session.Messages {
			message := &session.Messages[i]
			if message.ID != requestID || message.Role != "user" {
				continue
			}
			canRetry := retry && (message.State == "unavailable" ||
				(message.State == "interpreting" && now.Sub(message.Attempt) > time.Minute))
			if message.State != "pending" && !canRetry {
				return false, nil
			}
			claim = now
			message.State, message.Attempt = "interpreting", claim
			return true, nil
		}
		return false, storage.ErrNotFound
	})
	return session, claim, err
}

func (s *Session) Context(requestID string) []llm.ChatMessage {
	var history []llm.ChatMessage
	for _, message := range s.Messages {
		if message.Role == "user" {
			history = append(history, llm.ChatMessage{Role: llm.RoleUser, Content: message.Text})
			if message.ID == requestID {
				break
			}
		} else if message.Text != "" && message.State == "clarification" {
			history = append(history, llm.ChatMessage{Role: llm.RoleAssistant, Content: message.Text})
		}
	}
	return history
}

type Interpretation struct {
	Plan        Plan
	Text        string
	Task        *TaskSummary
	Review      *AgentReview
	Destination string
	Retryable   bool
}

func (s Sessions) FinishInterpretation(ctx context.Context, owner, id, requestID string, claim time.Time, outcome Interpretation) (*Session, error) {
	return s.update(ctx, owner, id, func(session *Session) (bool, error) {
		for i := range session.Messages {
			message := &session.Messages[i]
			if message.ID != requestID || message.Role != "user" {
				continue
			}
			if !message.Attempt.Equal(claim) || message.State != "interpreting" {
				return false, nil
			}
			now := time.Now().UTC()
			message.State = "interpreted"
			state := ""
			if outcome.Retryable {
				message.State = "unavailable"
			} else if outcome.Plan.Kind == Clarify {
				state = "clarification"
			}
			if requestID == session.ID && (session.Title == "" || session.Title == "Untitled chat") {
				session.Title = outcome.Plan.Title
				if session.Title == "" {
					session.Title = "Untitled chat"
				}
			}
			reply := Message{ID: "reply-" + requestID, Role: "assistant", Text: outcome.Text, Task: outcome.Task, Review: outcome.Review, State: state, Created: now}
			reply.Destination = outcome.Destination
			if outcome.Plan.Kind == WatchTask && outcome.Task != nil && !outcome.Task.Terminal() {
				session.Watches = append(session.Watches, Watch{RequestID: requestID, Task: *outcome.Task, Observed: now})
			}
			// An explicit retry replaces the earlier retry diagnostic.
			for j := range session.Messages {
				if session.Messages[j].ID == reply.ID {
					session.Messages[j] = reply
					session.LastActivity = now
					return true, nil
				}
			}
			session.Messages = append(session.Messages, reply)
			session.LastActivity = now
			return true, nil
		}
		return false, storage.ErrNotFound
	})
}

func (t TaskSummary) Terminal() bool {
	switch t.State {
	case "succeeded", "failed", "interrupted", "cancelled":
		return true
	default:
		return t.SettledBlock
	}
}
