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
		if session.Archived {
			return false, storage.ErrConflict
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
		if retry && session.Archived {
			return false, storage.ErrConflict
		}
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
		} else if ref := conversationReply(message); ref != "" {
			history = append(history, llm.ChatMessage{Role: llm.RoleAssistant, Content: ref})
		} else if message.Role == "assistant" && message.Text != "" && message.State == "clarification" && message.Discovery == nil {
			history = append(history, llm.ChatMessage{Role: llm.RoleAssistant, Content: message.Text})
		}
	}
	return history
}

// Give the model the visible answer when the operator refers to its format.
// Domain reviews and tool payloads continue to contribute safe references only.
func conversationReply(message Message) string {
	ref := observedReference(message)
	if message.Role != "assistant" || message.Destination != "tasks" || message.Review != nil ||
		message.Task != nil || message.Execution != nil || message.Selection != nil || message.Discovery != nil {
		return ref
	}
	// Earlier single-read answers have a query snapshot but no explicit state.
	singleQuery := message.Inventory != nil && message.Inventory.Area == "tasks" && message.Inventory.Total != nil && ref != ""
	if message.State == "grounded" || singleQuery {
		if ValidateMessage(message.Text) == nil {
			if ref != "" && len(ref)+1+len(message.Text) <= MaxRequestBytes {
				return ref + "\n" + message.Text
			}
			return message.Text
		}
	} else if message.State == "" && message.Inventory == nil {
		// Older combined reads have no provenance marker. Preserve the dialogue
		// boundary and topic without replaying unclassified message contents.
		return "The previous response concerned Tasks. Refer to the operator's preceding request for scope and presentation. Query current task records for follow-ups."
	}
	return ref
}

type Interpretation struct {
	Grounded    bool
	Discovery   *Discovery
	Selection   *Selection
	Inventory   *Inventory
	Execution   *ExecutionSummary
	Plan        Plan
	Text        string
	Task        *TaskSummary
	Review      *OperationReview
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
			} else if outcome.Grounded {
				state = "grounded"
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
			reply.Execution = outcome.Execution
			reply.Inventory = outcome.Inventory
			reply.Discovery, reply.Selection = outcome.Discovery, outcome.Selection
			if reply.Discovery != nil {
				reply.State = "discovery"
			}
			// A continuing watch announces its terminal result separately. Reviews
			// already expose a decision or a receipt in the index.
			reply.Unread = outcome.Review == nil && (outcome.Plan.Kind != WatchTask || outcome.Task == nil || outcome.Task.Terminal())
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
