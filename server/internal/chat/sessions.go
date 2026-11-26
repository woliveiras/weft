package chat

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const sessionPrefix = "chat/sessions/"

var (
	requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	ownerPattern     = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)
	ErrInvalidID     = errors.New("a valid chat or request identifier is required")
)

// Message stores the text and safe operation references rendered in the chat.
// Raw provider responses and task evidence remain outside the transcript.
type Message struct {
	Discovery   *Discovery        `json:"discovery,omitempty"`
	Selection   *Selection        `json:"selection,omitempty"`
	Inventory   *Inventory        `json:"inventory,omitempty"`
	Execution   *ExecutionSummary `json:"execution,omitempty"`
	ID          string            `json:"id"`
	Role        string            `json:"role"`
	Text        string            `json:"text"`
	State       string            `json:"state,omitempty"`
	Attempt     time.Time         `json:"attempt,omitempty"`
	Task        *TaskSummary      `json:"task,omitempty"`
	Review      *OperationReview  `json:"review,omitempty"`
	Destination string            `json:"destination,omitempty"`
	Unread      bool              `json:"unread,omitempty"`
	Created     time.Time         `json:"createdAt"`
}

type TaskSummary struct {
	LinkOnly          bool                 `json:"linkOnly,omitempty"`
	Repositories      []RepositoryEvidence `json:"repositories,omitempty"`
	OtherRepositories int                  `json:"otherRepositories,omitempty"`
	RetryOf           string               `json:"retryOf,omitempty"`
	ID                string               `json:"id"`
	State             string               `json:"state"`
	GraphID           string               `json:"graphId,omitempty"`
	ExecutionID       string               `json:"executionId,omitempty"`
	DecisionNeeded    bool                 `json:"decisionNeeded,omitempty"`
	SettledBlock      bool                 `json:"settledBlock,omitempty"`
}

type Watch struct {
	RequestID string      `json:"requestId"`
	Task      TaskSummary `json:"task"`
	Complete  bool        `json:"complete,omitempty"`
	Observed  time.Time   `json:"observedAt"`
	Unknown   bool        `json:"unknown,omitempty"`
}

type Session struct {
	ID                string    `json:"id"`
	Title             string    `json:"title,omitempty"`
	Archived          bool      `json:"archived"`
	LastActivity      time.Time `json:"lastActivity"`
	Messages          []Message `json:"messages"`
	Watches           []Watch   `json:"watches,omitempty"`
	Revision          int64     `json:"-"`
	TitleRetryState   string    `json:"titleRetryState,omitempty"`
	TitleRetryStarted time.Time `json:"titleRetryStarted,omitzero"`
}

// Sessions uses revisioned settings so each conversation mutation is atomic
// on SQLite, Postgres and filesystem stores without an in-memory authority.
type Sessions struct {
	Store storage.Store
}

func sessionKey(owner, id string) (string, error) {
	if !ownerPattern.MatchString(owner) || !requestIDPattern.MatchString(id) {
		return "", ErrInvalidID
	}
	return sessionPrefix + owner + "/" + id, nil
}

func decodeSession(record *storage.Setting) (*Session, error) {
	var session Session
	if err := json.Unmarshal(record.Value, &session); err != nil {
		return nil, errors.New("stored chat session is invalid")
	}
	_, id, ok := strings.Cut(strings.TrimPrefix(record.Key, sessionPrefix), "/")
	if !ok || id != session.ID || len(session.Messages) == 0 || session.Messages[0].ID != id {
		return nil, errors.New("stored chat session has an invalid identity")
	}
	session.Revision = record.Revision
	return &session, nil
}

func (s Sessions) Get(ctx context.Context, owner, id string) (*Session, error) {
	key, err := sessionKey(owner, id)
	if err != nil {
		return nil, err
	}
	record, err := s.Store.GetSetting(ctx, key)
	if err != nil {
		return nil, err
	}
	return decodeSession(record)
}

func (s Sessions) Create(ctx context.Context, owner, id, text string) (*Session, bool, error) {
	if err := ValidateMessage(text); err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	session := &Session{
		ID: id, LastActivity: now,
		Messages: []Message{{ID: id, Role: "user", Text: text, State: "pending", Created: now}},
	}
	if err := s.save(ctx, owner, session); !errors.Is(err, storage.ErrConflict) {
		return session, err == nil, err
	}
	actual, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, false, err
	}
	if actual.Messages[0].Text != text {
		return nil, false, storage.ErrConflict
	}
	return actual, false, nil
}

func (s Sessions) save(ctx context.Context, owner string, session *Session) error {
	key, err := sessionKey(owner, session.ID)
	if err != nil {
		return err
	}
	doc, err := json.Marshal(session)
	if err != nil {
		return err
	}
	index, err := s.index()
	if err != nil {
		return err
	}
	revision, err := index.PutChatSetting(ctx, session.summary(owner, key), doc, session.Revision)
	if err != nil {
		return err
	}
	session.Revision = revision
	return nil
}

func (s Sessions) update(ctx context.Context, owner, id string, change func(*Session) (bool, error)) (*Session, error) {
	for range 5 {
		session, err := s.Get(ctx, owner, id)
		if err != nil {
			return nil, err
		}
		changed, err := change(session)
		if err != nil || !changed {
			return session, err
		}
		if err = s.save(ctx, owner, session); !errors.Is(err, storage.ErrConflict) {
			return session, err
		}
	}
	return nil, storage.ErrConflict
}

func (s Sessions) Archive(ctx context.Context, owner, id string, archived bool) (*Session, error) {
	return s.update(ctx, owner, id, func(session *Session) (bool, error) {
		if session.Archived == archived {
			return false, nil
		}
		session.Archived = archived
		return true, nil
	})
}

func (s Sessions) MarkRead(ctx context.Context, owner, id, messageID string) (*Session, error) {
	return s.update(ctx, owner, id, func(session *Session) (bool, error) {
		for i := range session.Messages {
			if message := &session.Messages[i]; message.ID == messageID {
				unread := message.Unread
				message.Unread = false
				return unread, nil
			}
		}
		return false, storage.ErrNotFound
	})
}
