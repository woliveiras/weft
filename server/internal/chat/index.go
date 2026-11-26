package chat

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/storage"
)

const summaryPageSize = 30

type SummaryPage struct {
	Items          []storage.ChatSummary
	Next           string
	ArchivedUnread int
}

func (s Sessions) index() (storage.ChatIndex, error) {
	index, ok := s.Store.(storage.ChatIndex)
	if !ok {
		return nil, errors.New("storage does not support the chat session index")
	}
	return index, nil
}

func (s Sessions) List(ctx context.Context, owner string, archived bool, after string) (SummaryPage, error) {
	if !ownerPattern.MatchString(owner) {
		return SummaryPage{}, ErrInvalidID
	}
	filter := storage.ChatSummaryFilter{Owner: owner, Archived: archived, Limit: summaryPageSize + 1}
	if after != "" {
		stamp, id, ok := strings.Cut(after, "~")
		nanos, err := strconv.ParseInt(stamp, 10, 64)
		if !ok || err != nil || nanos <= 0 || !requestIDPattern.MatchString(id) {
			return SummaryPage{}, ErrInvalidID
		}
		filter.AfterActivity = time.Unix(0, nanos).UTC()
		filter.AfterKey = sessionPrefix + owner + "/" + id
	}
	index, err := s.index()
	if err != nil {
		return SummaryPage{}, err
	}
	items, err := index.ListChatSummaries(ctx, filter)
	if err != nil {
		return SummaryPage{}, err
	}
	page := SummaryPage{Items: items}
	if len(items) > summaryPageSize {
		page.Items = items[:summaryPageSize]
		last := page.Items[summaryPageSize-1]
		page.Next = fmt.Sprintf("%d~%s", last.LastActivity.UnixNano(), last.ID)
	}
	page.ArchivedUnread, err = index.ArchivedChatUnread(ctx, owner)
	return page, err
}

func (s *Session) summary(owner, key string) storage.ChatSummary {
	meta := storage.ChatSummary{Key: key, Owner: owner, ID: s.ID, Title: s.Title, Archived: s.Archived, LastActivity: s.LastActivity}
	for _, watch := range s.Watches {
		if watch.Complete {
			continue
		}
		meta.Running++
		meta.WatchCount++
		if watch.Unknown {
			meta.Unknown++
		} else if watch.Task.DecisionNeeded {
			meta.Decisions++
		}
	}
	for _, message := range s.Messages {
		if message.Unread {
			meta.Unread++
			if message.Task != nil {
				switch message.Task.State {
				case "succeeded":
					meta.UnreadSuccess++
				case "cancelled":
					meta.UnreadCancellation++
				case "failed", "interrupted", "blocked":
					meta.UnreadFailure++
				}
			}
		}
		if review := message.Review; review != nil {
			if (review.Skill != nil || review.TaskCorrection != nil) && review.State == ReviewApplying {
				meta.WatchCount++
			}
			if review.PR != nil && (review.State == ReviewQueued || review.State == ReviewApplying) {
				meta.WatchCount++
			}
			if review.Guidance != nil && (review.State == ReviewQueued || review.State == ReviewApplying) {
				meta.WatchCount++
			}
			if (review.TaskStart != nil || review.TaskCancel != nil || review.TaskRetry != nil || review.Approval != nil) && review.State == ReviewApplying {
				meta.WatchCount++
			}
			if (review.Publication != nil || review.Adoption != nil) && review.State == ReviewApplying {
				meta.WatchCount++
			}
			if review.WorkflowControl != nil && (review.State == ReviewApplying || review.State == ReviewAccepted) {
				meta.WatchCount++
				if review.State == ReviewAccepted {
					meta.Running++
				}
			}
			switch review.State {
			case ReviewAwaiting:
				meta.ReviewExpiries = append(meta.ReviewExpiries, review.ExpiresAt)
			case ReviewApplying, ReviewQueued:
				meta.Running++
			case ReviewCreated, ReviewApplied, ReviewConsumed:
				if message.Unread {
					meta.UnreadSuccess++
				}
			case ReviewConflict, ReviewIncomplete, ReviewRunEnded:
				if message.Unread {
					meta.UnreadFailure++
				}
			case ReviewUnknown:
				meta.Unknown++
			}
		}
	}
	return meta
}

// EnsureIndex upgrades transcripts written before indexed recents. It does
// not bump transcript revisions or rewrite their message data.
func (s Sessions) EnsureIndex(ctx context.Context) error {
	index, err := s.index()
	if err != nil {
		return err
	}
	after := ""
	for {
		gaps, err := index.ListChatIndexGaps(ctx, after, 32)
		if err != nil || len(gaps) == 0 {
			return err
		}
		for _, record := range gaps {
			after = record.Key
			session, err := decodeSession(record)
			if err != nil {
				return err
			}
			owner, _, _ := strings.Cut(strings.TrimPrefix(record.Key, sessionPrefix), "/")
			if err := index.IndexChatSetting(ctx, session.summary(owner, record.Key), record.Revision); err != nil &&
				!errors.Is(err, storage.ErrConflict) {
				return err
			}
		}
	}
}
