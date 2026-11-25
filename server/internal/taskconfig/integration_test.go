package taskconfig

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

func TestDraftSurvivesStoreRestart(t *testing.T) {
	for _, adapter := range []string{"fs", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			root := t.TempDir()
			ctx := t.Context()
			open := func() storage.Store {
				var s storage.Store
				var err error
				if adapter == "fs" {
					s, err = fsstore.Open(root)
				} else {
					s, err = sqlstore.OpenSQLite(ctx, filepath.Join(root, "tasks.db"))
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				return s
			}
			s := open()
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}
			if _, err := s.SaveGraph(ctx, g, 0); err != nil {
				t.Fatal(err)
			}
			input := good()
			input.Source = &IssueSource{Kind: "github", ConnectionID: "personal", Reference: "octocat/Hello-World#42"}
			draft, err := (Manager{Store: s}).Save(ctx, "", input, 0, "operator")
			if err != nil {
				t.Fatal(err)
			}
			events, err := s.ListEvents(ctx, storage.EventFilter{GraphID: "factory"})
			if err != nil || len(events) != 1 {
				t.Fatal(events, err)
			}
			b, _ := json.Marshal(events)
			if strings.Contains(string(b), good().Request) {
				t.Fatal("request in audit")
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s = open()
			defer s.Close()
			m := Manager{Store: s}
			loaded, err := m.Get(ctx, draft.ID)
			if err != nil || loaded.ResultNodeID != input.NodeID || loaded.Request != draft.Request || loaded.Revision != 1 || loaded.Source == nil || *loaded.Source != *draft.Source {
				t.Fatal(loaded, err)
			}
			updated := good()
			updated.Title = "Revised"
			next, err := m.Save(ctx, draft.ID, updated, 1, "another-operator")
			if err != nil || next.Revision != 2 || next.CreatedBy != "operator" || next.UpdatedBy != "another-operator" {
				t.Fatal(next, err)
			}
			if _, err = m.Save(ctx, draft.ID, good(), 1, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("stale save", err)
			}
			if _, err = s.SaveGraph(ctx, g, 1); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Save(ctx, draft.ID, good(), 2, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("stale workflow", err)
			}
			final, err := m.Get(ctx, draft.ID)
			if err != nil || final.Title != "Revised" {
				t.Fatal(final, err)
			}
		})
	}
}

type unavailableList struct{ storage.Store }

func (unavailableList) ListSettings(context.Context, string) ([]*storage.Setting, error) {
	return nil, errors.New("unavailable")
}
func TestListFailure(t *testing.T) {
	manager := Manager{Store: unavailableList{}}
	if _, err := manager.List(t.Context(), "", 50); err == nil {
		t.Fatal("failure hidden")
	}
	if _, err := manager.ListRecentlyUpdated(t.Context(), "", 50); err == nil {
		t.Fatal("failure hidden for recently updated tasks")
	}
}

func TestRecentlyUpdatedTaskPagination(t *testing.T) {
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Store: store}
	now := time.Date(2026, 10, 16, 12, 0, 0, 123456789, time.UTC)
	write := func(id string, updatedAt time.Time, revision int64) {
		t.Helper()
		payload, err := json.Marshal(Draft{ID: id, UpdatedAt: updatedAt})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PutSetting(ctx, prefix+id, payload, revision, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	write("tsk_a", now, 0)
	write("tsk_b", now, 0)
	write("tsk_z", now.Add(-time.Hour), 0)
	first, err := manager.ListRecentlyUpdated(ctx, "", 1)
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != "tsk_b" || first.Next == "" {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	write("tsk_b", now.Add(time.Minute), 1)
	second, err := manager.ListRecentlyUpdated(ctx, first.Next, 2)
	if err != nil || len(second.Items) != 2 || second.Items[0].ID != "tsk_a" || second.Items[1].ID != "tsk_z" || second.Next != "" {
		t.Fatalf("second page: %+v, %v", second, err)
	}
}

// Deletion survives adapter restart and shares the task revision fence with
// admission, editing and in-flight model interpretation.
func TestDraftDeletionDurabilityAndAdmissionFence(t *testing.T) {
	for _, adapter := range []string{"fs", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			root := t.TempDir()
			ctx := t.Context()
			open := func() storage.Store {
				var s storage.Store
				var err error
				if adapter == "fs" {
					s, err = fsstore.Open(root)
				} else {
					s, err = sqlstore.OpenSQLite(ctx, filepath.Join(root, "tasks.db"))
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				return s
			}
			s := open()
			m := Manager{Store: s}
			g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}
			if _, err := s.SaveGraph(ctx, g, 0); err != nil {
				t.Fatal(err)
			}
			d, err := m.Save(ctx, "", good(), 0, "operator")
			if err != nil {
				t.Fatal(err)
			}
			another, err := m.Save(ctx, "", good(), 0, "operator")
			if err != nil {
				t.Fatal(err)
			}
			if err = m.DeleteDraft(ctx, d.ID, d.Revision, "operator"); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Transition(ctx, d, "preparing", "admin"); !errors.Is(err, storage.ErrNotFound) {
				t.Fatal("stale admission resurrected deleted draft", err)
			}
			if _, err = m.Save(ctx, d.ID, good(), d.Revision, "operator"); !errors.Is(err, storage.ErrNotFound) {
				t.Fatal("stale edit resurrected draft", err)
			}
			d.Resolution = &Resolution{State: "resolved"}
			if _, err = m.persistPrompt(ctx, d, d.Revision, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("in-flight model result resurrected draft", err)
			}
			// If admission claims first, deletion must lose even with its fresh revision.
			claim, err := m.Transition(ctx, another, "preparing", "admin")
			if err != nil {
				t.Fatal(err)
			}
			if err = m.DeleteDraft(ctx, another.ID, claim.Revision, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("admitted draft deleted", err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s = open()
			defer s.Close()
			m.Store = s
			if _, err = m.Get(ctx, d.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Fatal("deletion lost after restart", err)
			}
			for _, list := range []func(context.Context, string, int) (Page, error){m.List, m.ListRecentlyUpdated} {
				page, err := list(ctx, "", 1)
				if err != nil || len(page.Items) != 1 || page.Items[0].ID != another.ID || page.Next != "" {
					t.Fatal("tombstone leaked or broke pagination", page, err)
				}
			}
			stored, err := s.GetSetting(ctx, prefix+d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(stored.Value), good().Request) {
				t.Fatal("request retained in deletion marker")
			}
		})
	}
}

func TestDeletedDraftSubmissionCannotReplay(t *testing.T) {
	s, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	m := Manager{Store: s}
	p := PromptInput{Request: "A request retained through interpretation"}
	key := "deletion-replay-key-0001"
	d, created, err := m.SaveSubmission(t.Context(), p, key, "admin")
	if err != nil || !created {
		t.Fatal(d, created, err)
	}
	if err = m.DeleteDraft(t.Context(), d.ID, d.Revision, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, created, err = m.SaveSubmission(t.Context(), p, key, "admin"); !errors.Is(err, storage.ErrConflict) || created {
		t.Fatal("deleted submission replayed", created, err)
	}
	if _, err = m.SavePrompt(t.Context(), PromptInput{ID: d.ID, Revision: d.Revision, Request: "Try again"}, "admin"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("deleted prompt recreated", err)
	}
}

type deletionAdmissionRaceStore struct {
	storage.Store
	taskKey string
	ready   chan string
	gates   map[string]chan struct{}
}

func (s *deletionAdmissionRaceStore) PutSetting(ctx context.Context, key string, value []byte, revision int64, actor string) (int64, error) {
	if key == s.taskKey {
		s.ready <- actor
		select {
		case <-s.gates[actor]:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return s.Store.PutSetting(ctx, key, value, revision, actor)
}
func TestDraftDeletionAndAdmissionCompeteOnOneRevision(t *testing.T) {
	for _, winner := range []string{"operator", "admin"} {
		t.Run(winner, func(t *testing.T) {
			ctx := t.Context()
			s, err := fsstore.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err = s.SaveGraph(ctx, &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}, 0); err != nil {
				t.Fatal(err)
			}
			d, err := (Manager{Store: s}).Save(ctx, "", good(), 0, "operator")
			if err != nil {
				t.Fatal(err)
			}
			raced := &deletionAdmissionRaceStore{Store: s, taskKey: prefix + d.ID, ready: make(chan string, 2), gates: map[string]chan struct{}{"operator": make(chan struct{}), "admin": make(chan struct{})}}
			m := Manager{Store: raced}
			results := make(chan struct {
				actor string
				err   error
			}, 2)
			go func() {
				results <- struct {
					actor string
					err   error
				}{"operator", m.DeleteDraft(ctx, d.ID, d.Revision, "operator")}
			}()
			go func() {
				claim := *d
				claim.ExecutionGraphID = "task-run-" + strings.TrimPrefix(d.ID, "tsk_")
				_, err := m.Transition(ctx, &claim, "preparing", "admin")
				results <- struct {
					actor string
					err   error
				}{"admin", err}
			}()
			<-raced.ready
			<-raced.ready
			close(raced.gates[winner])
			first := <-results
			if first.actor != winner || first.err != nil {
				t.Fatal("winner failed", first)
			}
			loser := "operator"
			if winner == loser {
				loser = "admin"
			}
			close(raced.gates[loser])
			second := <-results
			if second.actor != loser || !errors.Is(second.err, storage.ErrConflict) {
				t.Fatal("losing claim was accepted", second)
			}
			current, err := (Manager{Store: s}).Get(ctx, d.ID)
			if winner == "operator" {
				if !errors.Is(err, storage.ErrNotFound) {
					t.Fatal("deleted draft survived", current, err)
				}
			} else if err != nil || current.Status != "preparing" || current.ExecutionGraphID == "" {
				t.Fatal("admission claim lost", current, err)
			}
		})
	}
}
