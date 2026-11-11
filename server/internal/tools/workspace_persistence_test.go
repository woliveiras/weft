package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestWorkspaceObservationsPersistAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := fsstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, events.NewBus(store, slog.Default()))
	p := principal(t.TempDir(), domain.ToolGrant{Name: "shell.exec"})
	res, err := gw.Invoke(ctx, p, "shell.exec", json.RawMessage(`{"command":"printf persisted > result.txt"}`))
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = fsstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	found, err := store.ListEvents(ctx, storage.EventFilter{GraphID: p.GraphID, NodeID: p.NodeID, Types: []domain.EventType{"workspace.observed"}})
	if err != nil || len(found) != 1 || found[0].ExecutionID != p.ExecutionID {
		t.Fatalf("%+v %v", found, err)
	}
	other, err := store.ListEvents(ctx, storage.EventFilter{GraphID: "another", Types: []domain.EventType{"workspace.observed"}})
	if err != nil || len(other) != 0 {
		t.Fatalf("scope leaked: %v %v", other, err)
	}
}
