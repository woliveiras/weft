package chat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

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
