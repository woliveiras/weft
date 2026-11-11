package taskconfig

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
			if err != nil || loaded.Request != draft.Request || loaded.Revision != 1 || loaded.Source == nil || *loaded.Source != *draft.Source {
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
	if _, err := (Manager{Store: unavailableList{}}).List(t.Context(), "", 50); err == nil {
		t.Fatal("failure hidden")
	}
}
