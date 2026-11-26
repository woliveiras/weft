package chat

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/stores"
	"github.com/woliveiras/weft/server/internal/taskconfig"
)

func TestSessionStorageSeparatesOwnersAndSurvivesReopen(t *testing.T) {
	ctx := t.Context()
	cfg := stores.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "chat.db")}
	backend, err := stores.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sessions := Sessions{Store: backend}
	const id = "8921e80ce56b4b50a83981d63b1a911b"
	s1, created, err := sessions.Create(ctx, "owner-a", id, "Follow the first task.")
	if err != nil || !created || len(s1.Messages) != 1 {
		t.Fatalf("create: %+v %v %v", s1, created, err)
	}
	again, created, err := sessions.Create(ctx, "owner-a", id, "Follow the first task.")
	if err != nil || created || again.ID != s1.ID {
		t.Fatalf("replay: %+v %v %v", again, created, err)
	}
	if _, _, err = sessions.Create(ctx, "owner-a", id, "Follow a different task."); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("different payload replay: %v", err)
	}
	if _, err = sessions.Get(ctx, "owner-b", id); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("cross-owner get: %v", err)
	}
	if _, err = sessions.Archive(ctx, "owner-b", id, true); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("cross-owner archive: %v", err)
	}
	if _, err = sessions.Archive(ctx, "owner-a", id, true); err != nil {
		t.Fatal(err)
	}
	if err = backend.Close(); err != nil {
		t.Fatal(err)
	}
	backend, err = stores.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	sessions.Store = backend
	for _, owner := range []string{"owner-a", "owner-b"} {
		active, err := sessions.List(ctx, owner, false, "")
		if err != nil || len(active.Items) != 0 {
			t.Fatalf("active list for %s: %+v %v", owner, active, err)
		}
	}
	archived, err := sessions.List(ctx, "owner-a", true, "")
	if err != nil || len(archived.Items) != 1 || archived.Items[0].ID != id || !archived.Items[0].Archived {
		t.Fatalf("archived list: %+v %v", archived, err)
	}
	if _, err = sessions.Archive(ctx, "owner-a", id, false); err != nil {
		t.Fatal(err)
	}
	reopened, err := sessions.Get(ctx, "owner-a", id)
	if err != nil || reopened.Archived || len(reopened.Messages) != 1 || reopened.Messages[0].Text != "Follow the first task." {
		t.Fatalf("restored: %+v %v", reopened, err)
	}
}

func TestConcurrentFirstMessageProducesOneSession(t *testing.T) {
	ctx := t.Context()
	backend, err := stores.Open(ctx, stores.Config{Type: "fs", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	sessions := Sessions{Store: backend}
	const id = "56b457af3a6d42c98d13929a176de531"
	var wait sync.WaitGroup
	outcomes := make(chan bool, 6)
	for range cap(outcomes) {
		wait.Go(func() {
			_, created, err := sessions.Create(ctx, "owner-a", id, "Remember this request.")
			if err != nil {
				t.Error(err)
			}
			outcomes <- created
		})
	}
	wait.Wait()
	close(outcomes)
	count := 0
	for created := range outcomes {
		if created {
			count++
		}
	}
	list, err := sessions.List(ctx, "owner-a", false, "")
	if err != nil || count != 1 || len(list.Items) != 1 {
		t.Fatalf("unique creations=%d list=%+v err=%v", count, list, err)
	}
}

func TestArchivedWatchSettlesOnceFromCanonicalTaskAfterRestart(t *testing.T) {
	ctx := t.Context()
	cfg := stores.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "observations.db")}
	backend, err := stores.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskManager := taskconfig.Manager{Store: backend}
	if _, err := backend.SaveGraph(ctx, &domain.Graph{
		Metadata: domain.GraphMetadata{ID: "fixture", Name: "Fixture"},
		Spec:     domain.GraphSpec{Nodes: []domain.Node{{ID: "echo"}}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	task, err := taskManager.Save(ctx, "", taskconfig.Input{
		Title: "Release checklist", Request: "Confirm docs", GraphID: "fixture", GraphVersion: 1, NodeID: "echo", Outcome: "freeform",
	}, 0, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	const owner = "owner-a"
	const id = "1721e80ce56b4b50a83981d63b1a911b"
	sessions := Sessions{Store: backend}
	if _, _, err = sessions.Create(ctx, owner, id, "Follow release checklist."); err != nil {
		t.Fatal(err)
	}
	_, claim, err := sessions.ClaimInterpretation(ctx, owner, id, id, false)
	if err != nil || claim.IsZero() {
		t.Fatalf("claim: %s %v", claim, err)
	}
	if _, err := sessions.FinishInterpretation(ctx, owner, id, id, claim, Interpretation{
		Plan: Plan{Kind: WatchTask, Title: "Watch checklist"}, Text: "Following task",
		Task: &TaskSummary{ID: task.ID, State: task.Status},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = sessions.Archive(ctx, owner, id, true); err != nil {
		t.Fatal(err)
	}
	if err = backend.Close(); err != nil {
		t.Fatal(err)
	}

	backend, err = stores.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	sessions.Store = backend
	taskManager.Store = backend
	if _, err := taskManager.Transition(ctx, task, "failed", "fixture"); err != nil {
		t.Fatal(err)
	}
	observe := func(ctx context.Context, id string) (TaskSummary, error) {
		draft, err := taskManager.Get(ctx, id)
		if err != nil {
			return TaskSummary{}, err
		}
		return TaskSummary{ID: draft.ID, State: draft.Status}, nil
	}
	for range 2 {
		if _, err := sessions.ReconcilePage(ctx, "", func(owner string) bool { return owner == "owner-a" }, observe); err != nil {
			t.Fatal(err)
		}
	}
	finished, err := sessions.Get(ctx, owner, id)
	if err != nil || !finished.Archived || len(finished.Messages) != 3 || !finished.Watches[0].Complete {
		t.Fatalf("settled watch: %+v %v", finished, err)
	}
	result := finished.Messages[2]
	if !result.Unread || result.Task.ID != task.ID || result.Task.State != "failed" {
		t.Fatalf("recorded outcome: %+v", result)
	}
	page, err := sessions.List(ctx, owner, false, "")
	if err != nil || page.ArchivedUnread != 1 || len(page.Items) != 0 {
		t.Fatalf("archived outcome visible from Recents: %+v %v", page, err)
	}
	read, err := sessions.MarkRead(ctx, owner, id, result.ID)
	if err != nil || read.Messages[2].Unread || !read.LastActivity.Equal(finished.LastActivity) {
		t.Fatalf("mark read: %+v %v", read, err)
	}
	page, err = sessions.List(ctx, owner, false, "")
	if err != nil || page.ArchivedUnread != 0 {
		t.Fatalf("archived notification was not cleared: %+v %v", page, err)
	}
}
