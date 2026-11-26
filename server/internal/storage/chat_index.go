package storage

import (
	"context"
	"time"
)

// ChatSummary contains list and scheduler fields without the transcript. The
// index adapter commits this projection with the corresponding setting.
type ChatSummary struct {
	Key                string      `json:"key"`
	Owner              string      `json:"owner"`
	ID                 string      `json:"id"`
	Title              string      `json:"title"`
	LastActivity       time.Time   `json:"lastActivity"`
	Archived           bool        `json:"archived"`
	Running            int         `json:"running"`
	WatchCount         int         `json:"watchCount"`
	Unread             int         `json:"unread"`
	UnreadSuccess      int         `json:"unreadSuccess"`
	UnreadFailure      int         `json:"unreadFailure"`
	UnreadCancellation int         `json:"unreadCancellation"`
	Decisions          int         `json:"decisions"`
	ReviewExpiries     []time.Time `json:"reviewExpiries,omitempty"`
	Unknown            int         `json:"unknown"`
	Revision           int64       `json:"revision"`
}

type ChatSummaryFilter struct {
	Owner         string
	Archived      bool
	AfterActivity time.Time
	AfterKey      string
	Limit         int
}

// ChatIndex is implemented by the supported durable stores. Reindexing old
// setting documents uses a revision guard without modifying their contents.
type ChatIndex interface {
	PutChatSetting(context.Context, ChatSummary, []byte, int64) (int64, error)
	IndexChatSetting(context.Context, ChatSummary, int64) error
	ListChatSummaries(context.Context, ChatSummaryFilter) ([]ChatSummary, error)
	ArchivedChatUnread(context.Context, string) (int, error)
	ListWatchedChats(context.Context, string, int) ([]ChatSummary, error)
	ListChatIndexGaps(context.Context, string, int) ([]*Setting, error)
}
