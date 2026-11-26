package chat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/stores"
)

func TestChatIndexUpgradePreservesRecordsAndProvidesStableOwnerPages(t *testing.T) {
	backends := []stores.Config{{Type: "sqlite"}, {Type: "fs"}}
	if dsn := os.Getenv("WEFT_TEST_CHAT_POSTGRES_DSN"); dsn != "" {
		backends = append(backends, stores.Config{Type: "postgres", DSN: dsn})
	}
	for _, backend := range backends {
		t.Run(backend.Type, func(t *testing.T) {
			ctx := t.Context()
			backend.Dir = t.TempDir()
			store, err := stores.Open(ctx, backend)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			sessions := Sessions{Store: store}
			const owner = "owner-a"
			stamp := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
			makeSession := func(number int) *Session {
				id := fmt.Sprintf("%032x", number)
				return &Session{ID: id, LastActivity: stamp, Messages: []Message{{ID: id, Role: "user", Text: "Retained prompt", Created: stamp}}}
			}
			legacy := makeSession(1)
			key := sessionPrefix + owner + "/" + legacy.ID
			old, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.PutSetting(ctx, key, old, 0, "previous version"); err != nil {
				t.Fatal(err)
			}
			for i := 2; i <= summaryPageSize+2; i++ {
				if err := sessions.save(ctx, owner, makeSession(i)); err != nil {
					t.Fatal(err)
				}
			}
			if err := sessions.save(ctx, "other-owner", makeSession(summaryPageSize+3)); err != nil {
				t.Fatal(err)
			}
			if err := sessions.EnsureIndex(ctx); err != nil {
				t.Fatal(err)
			}
			record, err := store.GetSetting(ctx, key)
			if err != nil || record.Revision != 1 || !bytes.Equal(old, record.Value) {
				t.Fatalf("changed retained session: %+v %v", record, err)
			}
			page, err := sessions.List(ctx, owner, false, "")
			if err != nil || len(page.Items) != summaryPageSize || page.Next == "" || page.Items[0].ID != legacy.ID {
				t.Fatalf("first owned page: %+v %v", page, err)
			}
			boundary := page.Items[len(page.Items)-1]
			if _, err := sessions.Archive(ctx, owner, boundary.ID, true); err != nil {
				t.Fatal(err)
			}
			following, err := sessions.List(ctx, owner, false, page.Next)
			if err != nil || len(following.Items) != 2 || following.Next != "" || following.Items[0].Key <= boundary.Key {
				t.Fatalf("page after archived boundary: %+v %v", following, err)
			}
			index := store.(storage.ChatIndex)
			visited, err := index.ListWatchedChats(ctx, "", 32)
			if err != nil || len(visited) != 0 {
				t.Fatalf("scheduler scanned completed history: %+v %v", visited, err)
			}
		})
	}
}

func TestSessionCursorRequiresTimestampAndAnOpaqueID(t *testing.T) {
	for _, after := range []string{"other-owner/opaque", "0~8921e80ce56b4b50a83981d63b1a911b", "123~../cross-owner"} {
		if _, err := (Sessions{}).List(t.Context(), "owner-a", false, after); !errors.Is(err, ErrInvalidID) {
			t.Errorf("invalid cursor %q: %v", after, err)
		}
	}
}

func TestSummaryTracksPendingRequestSeparatelyFromDurableWatches(t *testing.T) {
	now := time.Now().UTC()
	session := &Session{Messages: []Message{{Role: "user", State: "pending", Created: now}}}
	for _, state := range []string{"pending", "interpreting", "interpreted"} {
		session.Messages[0].State = state
		session.Messages[0].Attempt = now.Add(time.Second)
		got := session.summary("owner", "key")
		want := map[string]time.Time{"pending": now.Add(time.Minute), "interpreting": now.Add(time.Second + time.Minute)}[state]
		if !got.RequestActiveUntil.Equal(want) || got.WatchCount != 0 {
			t.Fatalf("state %s summary %+v", state, got)
		}
	}
	session.TitleRetryState, session.TitleRetryStarted = "pending", now
	if !session.summary("owner", "key").RequestActiveUntil.Equal(now.Add(time.Minute)) {
		t.Fatal("title retry absent from session list")
	}
}

func TestUnreadInspectionReplyStaysDistinctFromTerminalSuccess(t *testing.T) {
	s := Session{Messages: []Message{{Role: "assistant", Unread: true, Text: "Select a task"}}}
	meta := s.summary("owner", "key")
	if meta.Unread != 1 || meta.UnreadSuccess != 0 || meta.UnreadFailure != 0 || meta.Running != 0 {
		t.Fatal(meta)
	}
}

func TestDecisionCountUsesCurrentRoleAndExactExpiry(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Minute)
	session := Session{
		Watches: []Watch{{Task: TaskSummary{DecisionNeeded: true}}},
		Messages: []Message{
			{Review: &OperationReview{Configuration: &ConfigReview{}, State: ReviewAwaiting, ExpiresAt: future}},
			{Review: &OperationReview{Guidance: &Guidance{}, State: ReviewAwaiting, ExpiresAt: future}},
			{Review: &OperationReview{TaskDraft: &TaskCreation{}, State: ReviewAwaiting, ExpiresAt: now}},
			{Review: &OperationReview{State: ReviewDeclined, ExpiresAt: future}},
		},
	}
	summary := session.summary("owner", "key")
	summary.PendingReviews = append(summary.PendingReviews, storage.ChatPendingReview{MinimumRole: "unknown", ExpiresAt: future})
	for role, want := range map[config.Role]int{config.RoleViewer: 1, config.RoleOperator: 2, config.RoleAdmin: 3} {
		got, err := (Sessions{}).DecisionCount(t.Context(), "owner", summary, role, now)
		if err != nil || got != want {
			t.Errorf("%s decisions: got %d, %v; want %d", role, got, err, want)
		}
	}
}

func TestLegacyDecisionCountReadsOnlyLiveReviewsFromTheOwner(t *testing.T) {
	store, err := stores.Open(t.Context(), stores.Config{Type: "sqlite", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions := Sessions{Store: store}
	const owner = "old-owner"
	const id = "ab6353dce56b4b50a83981d63b1a811b"
	now := time.Now().UTC()
	future := now.Add(time.Minute)
	session := Session{ID: id, LastActivity: now, Messages: []Message{
		{ID: id, Role: "user"},
		{Review: &OperationReview{Guidance: &Guidance{}, State: ReviewAwaiting, ExpiresAt: future}},
		{Review: &OperationReview{Configuration: &ConfigReview{}, State: ReviewAwaiting, ExpiresAt: future}},
	}}
	if err := sessions.save(t.Context(), owner, &session); err != nil {
		t.Fatal(err)
	}
	legacy := session.summary(owner, sessionPrefix+owner+"/"+id)
	legacy.PendingReviews = nil
	if err := store.(storage.ChatIndex).IndexChatSetting(t.Context(), legacy, session.Revision); err != nil {
		t.Fatal(err)
	}
	page, err := sessions.List(t.Context(), owner, false, "")
	if err != nil || len(page.Items) != 1 || len(page.Items[0].PendingReviews) != 0 || len(page.Items[0].ReviewExpiries) != 2 {
		t.Fatalf("listing the expiry-only index: %+v, %v", page, err)
	}
	legacy = page.Items[0]
	for role, want := range map[config.Role]int{config.RoleViewer: 0, config.RoleOperator: 1, config.RoleAdmin: 2} {
		got, err := sessions.DecisionCount(t.Context(), owner, legacy, role, now)
		if err != nil || got != want {
			t.Errorf("legacy %s decisions: got %d, %v; want %d", role, got, err, want)
		}
	}
	if got, err := sessions.DecisionCount(t.Context(), "different-owner", legacy, config.RoleAdmin, now); got != 0 || !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing owned transcript: got %d, %v", got, err)
	}
	if got, err := (Sessions{}).DecisionCount(t.Context(), owner, legacy, config.RoleAdmin, future); err != nil || got != 0 {
		t.Fatalf("expired legacy reviews: got %d, %v", got, err)
	}
}
