package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestMonitorPersistsUnknownAuthorshipAndRestartGap(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	dir := t.TempDir()
	store, err := fsstore.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	targets := func(context.Context) ([]WorkspaceTarget, error) {
		return []WorkspaceTarget{{GraphID: "project", Dir: dir}}, nil
	}
	m := &WorkspaceMonitor{Auditor: events.NewBus(store, slog.Default()), Targets: targets}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "external"), []byte("private content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = fsstore.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m = &WorkspaceMonitor{Auditor: events.NewBus(store, slog.Default()), Targets: targets}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	observations, err := store.ListEvents(ctx, storage.EventFilter{GraphID: "project", Types: []domain.EventType{"workspace.observed"}, Ascending: true})
	if err != nil || len(observations) != 3 {
		t.Fatalf("%v %v", observations, err)
	}
	for i, ev := range observations {
		var data map[string]any
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatal(err)
		}
		if ev.ExecutionID != "" || data["attribution"] != "unknown" {
			t.Fatal("invented attribution")
		}
		if i == 2 && (data["status"] != "baseline" || data["gap"] != true) {
			t.Fatalf("missing restart gap: %v", data)
		}
	}
}
