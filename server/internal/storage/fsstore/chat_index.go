package fsstore

import (
	"context"
	"slices"
	"strings"

	"github.com/woliveiras/weft/server/internal/storage"
)

func cloneChatSummary(meta storage.ChatSummary) storage.ChatSummary {
	meta.PendingReviews = slices.Clone(meta.PendingReviews)
	meta.ReviewExpiries = slices.Clone(meta.ReviewExpiries)
	return meta
}

func (s *Store) PutChatSetting(ctx context.Context, meta storage.ChatSummary, value []byte, expected int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.putSettingLocked(meta.Key, value, expected, "chat", &meta)
}

func (s *Store) IndexChatSetting(ctx context.Context, meta storage.ChatSummary, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	record := s.settings[meta.Key]
	if record == nil || record.Revision != expected {
		return storage.ErrConflict
	}
	next := *record
	meta = cloneChatSummary(meta)
	meta.Revision = expected
	next.Chat = &meta
	if err := writeJSON(s.settingPath(meta.Key), &next); err != nil {
		return err
	}
	s.settings[meta.Key] = &next
	return nil
}

func (s *Store) ListChatSummaries(ctx context.Context, filter storage.ChatSummaryFilter) ([]storage.ChatSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]storage.ChatSummary, 0)
	for _, record := range s.settings {
		meta := record.Chat
		if meta == nil || meta.Owner != filter.Owner || meta.Archived != filter.Archived {
			continue
		}
		if filter.AfterKey != "" && (meta.LastActivity.After(filter.AfterActivity) ||
			(meta.LastActivity.Equal(filter.AfterActivity) && meta.Key <= filter.AfterKey)) {
			continue
		}
		out = append(out, cloneChatSummary(*meta))
	}
	slices.SortFunc(out, func(a, b storage.ChatSummary) int {
		if a.LastActivity.Equal(b.LastActivity) {
			return strings.Compare(a.Key, b.Key)
		}
		return b.LastActivity.Compare(a.LastActivity)
	})
	return out[:min(len(out), storage.Limit(filter.Limit, 31, 200))], nil
}

func (s *Store) ArchivedChatUnread(ctx context.Context, owner string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	count := 0
	for _, record := range s.settings {
		if meta := record.Chat; meta != nil && meta.Owner == owner && meta.Archived {
			count += meta.Unread
		}
	}
	return count, nil
}

func (s *Store) ListWatchedChats(ctx context.Context, after string, limit int) ([]storage.ChatSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]storage.ChatSummary, 0)
	for _, record := range s.settings {
		if meta := record.Chat; meta != nil && meta.WatchCount > 0 && meta.Key > after {
			out = append(out, cloneChatSummary(*meta))
		}
	}
	slices.SortFunc(out, func(a, b storage.ChatSummary) int { return strings.Compare(a.Key, b.Key) })
	return out[:min(len(out), storage.Limit(limit, 32, 200))], nil
}

func (s *Store) ListChatIndexGaps(ctx context.Context, after string, limit int) ([]*storage.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var keys []string
	for key, record := range s.settings {
		if key > after && strings.HasPrefix(key, "chat/sessions/") &&
			(record.Chat == nil || record.Chat.Revision != record.Revision) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	n := min(len(keys), storage.Limit(limit, 32, 200))
	out := make([]*storage.Setting, 0, n)
	for _, key := range keys[:n] {
		out = append(out, cloneSetting(s.settings[key]))
	}
	return out, nil
}
