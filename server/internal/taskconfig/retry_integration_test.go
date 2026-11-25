package taskconfig

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
	"github.com/woliveiras/weft/server/internal/storage/sqlstore"
)

type retryLostResponseStore struct{ storage.Store }

func (s retryLostResponseStore) PutSetting(ctx context.Context, key string, value []byte, revision int64, actor string) (int64, error) {
	next, err := s.Store.PutSetting(ctx, key, value, revision, actor)
	if err == nil && revision == 0 && strings.HasPrefix(key, prefix) {
		return 0, errors.New("response unavailable after committed draft write")
	}
	return next, err
}

func TestRetryCommittedWriteSurvivesLostResponseAndRestart(t *testing.T) {
	ctx, root := t.Context(), t.TempDir()
	open := func() storage.Store {
		t.Helper()
		s, err := sqlstore.OpenSQLite(ctx, filepath.Join(root, "tasks.db"))
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
	source, err := m.Save(ctx, "", good(), 0, "operator")
	if err != nil {
		t.Fatal(err)
	}
	source, err = m.Transition(ctx, source, "failed", "system:task")
	if err != nil {
		t.Fatal(err)
	}
	m.Store = retryLostResponseStore{s}
	if d, created, err := m.RetryFailed(ctx, source.ID, source.Revision, "operator"); err == nil || created || d != nil {
		t.Fatal("uncertain write reported success", d, created, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	defer s.Close()
	m.Store = s
	child, created, err := m.RetryFailed(ctx, source.ID, source.Revision, "operator")
	if err != nil || created || child.RetryOfTaskID != source.ID {
		t.Fatal("uncertain retry duplicated after restart", child, created, err)
	}
	page, err := m.List(ctx, "", 100)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("unexpected retry count", page, err)
	}
}

func TestRetryDurabilityConcurrencyAndProvenance(t *testing.T) {
	for _, adapter := range []string{"fs", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			ctx, root := t.Context(), t.TempDir()
			open := func() storage.Store {
				t.Helper()
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
			source, err := m.Save(ctx, "", good(), 0, "operator")
			if err != nil {
				t.Fatal(err)
			}
			source.ExecutionGraphID = "task-run-retained"
			source.Submitted = true
			source.Resolution = &Resolution{State: "resolved"}
			source, err = m.Transition(ctx, source, "failed", "system:task")
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(source)
			type result struct {
				draft   *Draft
				created bool
				err     error
			}
			results := make(chan result, 8)
			var wg sync.WaitGroup
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					d, created, err := m.RetryFailed(ctx, source.ID, source.Revision, "operator")
					results <- result{d, created, err}
				}()
			}
			wg.Wait()
			close(results)
			var child *Draft
			creations := 0
			for result := range results {
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.created {
					creations++
				}
				if child != nil && child.ID != result.draft.ID {
					t.Fatal("multiple children")
				}
				child = result.draft
			}
			if creations != 1 || !reflect.DeepEqual(child.Input, source.Input) {
				t.Fatal("retry did not create one input snapshot", creations)
			}
			after, err := m.Get(ctx, source.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterJSON, _ := json.Marshal(after)
			if string(before) != string(afterJSON) {
				t.Fatal("source mutated")
			}
			// Editing and model interpretation must retain server-owned provenance.
			input := child.Input
			input.Title = "Updated retry"
			child, err = m.Save(ctx, child.ID, input, child.Revision, "operator")
			if err != nil {
				t.Fatal(err)
			}
			if child.RetryOfTaskID != source.ID || child.RetryOfRevision != source.Revision {
				t.Fatal("save lost provenance", child)
			}
			child, err = m.SavePrompt(ctx, PromptInput{ID: child.ID, Revision: child.Revision, Request: "Refine the retained request", GraphID: child.GraphID}, "operator")
			if err != nil {
				t.Fatal(err)
			}
			if child.RetryOfTaskID != source.ID || child.RetryOfRevision != source.Revision {
				t.Fatal("provenance lost", child)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s = open()
			defer s.Close()
			m.Store = s
			replay, created, err := m.RetryFailed(ctx, source.ID, source.Revision, "operator")
			if err != nil || created || replay.ID != child.ID || replay.Request != child.Request || replay.RetryOfTaskID != source.ID {
				t.Fatal("restart reset child", replay, err)
			}
			if err = m.DeleteDraft(ctx, child.ID, replay.Revision, "operator"); err != nil {
				t.Fatal(err)
			}
			if _, _, err = m.RetryFailed(ctx, source.ID, source.Revision, "operator"); !errors.Is(err, storage.ErrConflict) {
				t.Fatal("deleted child recreated", err)
			}
		})
	}
}

func TestRetryPreservesStaleWorkflowPin(t *testing.T) {
	s, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	g := &domain.Graph{Metadata: domain.GraphMetadata{ID: "factory"}, Spec: domain.GraphSpec{Nodes: []domain.Node{{ID: "reader"}}}}
	if _, err = s.SaveGraph(ctx, g, 0); err != nil {
		t.Fatal(err)
	}
	m := Manager{Store: s}
	source, err := m.Save(ctx, "", good(), 0, "operator")
	if err != nil {
		t.Fatal(err)
	}
	source, err = m.Transition(ctx, source, "failed", "system:task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveGraph(ctx, g, 1); err != nil {
		t.Fatal(err)
	}
	child, _, err := m.RetryFailed(ctx, source.ID, source.Revision, "operator")
	if err != nil || child.GraphVersion != 1 {
		t.Fatal("retry advanced pin", child, err)
	}
	if _, err = m.Save(ctx, child.ID, child.Input, child.Revision, "operator"); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("stale workflow save accepted", err)
	}
}
