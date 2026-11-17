package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

// TestCheckEvidenceIsPersistedAndQueryableByType wires the gateway to the
// real event bus and store, as the server does, and reads the evidence back
// the way the task view does (events filtered by type).
func TestCheckEvidenceIsPersistedAndQueryableByType(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil))))
	dir := t.TempDir()
	p := principal(dir, domain.ToolGrant{Name: "shell.exec"})
	p.Sandbox = &ProcessSandbox{Root: dir}
	p.Checks = []domain.Check{{Name: "lint", Command: "echo 'main.go:3:1: unused variable x' >&2; exit 2"}}
	if _, err := gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"lint"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Invoke(ctx, p, CheckToolName, json.RawMessage(`{"name":"missing"}`)); err != nil {
		t.Fatalf("an unknown check is a tool error for the model, not a gateway failure: %v", err)
	}
	evs, err := st.ListEvents(ctx, storage.EventFilter{GraphID: "g", Types: []domain.EventType{domain.EvCheckCompleted}, Ascending: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want one evidence event (the unknown check is not evidence), got %d", len(evs))
	}
	var run CheckRun
	if err := json.Unmarshal(evs[0].Data, &run); err != nil {
		t.Fatal(err)
	}
	if run.Passed || run.ExitCode != 2 || run.Format != FormatGo || len(run.Failures) != 1 || run.Failures[0].File != "main.go" || run.Failures[0].Line != 3 {
		t.Fatalf("evidence %+v", run)
	}
	if evs[0].NodeID != "dev" || evs[0].ExecutionID != "exe_1" || evs[0].Actor == "" {
		t.Fatalf("attribution %+v", evs[0])
	}
}
