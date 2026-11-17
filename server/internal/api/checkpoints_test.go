package api_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/workspace"
)

const checkpointDoc = `
apiVersion: weft.io/v1
kind: Graph
metadata: {name: cp-api}
spec:
  workspace: {git: true}
  defaults: {runtime: {kind: echo}}
  nodes: [{id: dev}]
  channels: [{id: in, to: [dev]}]
`

func TestCheckpointAPIAuthorizationAndRestore(t *testing.T) {
	var root string
	srv, a := setupApp(t, func(c *config.Config) { root = c.Workspaces.Root })
	if c := call(t, srv, "POST", "/api/v1/graphs", adminTok, "application/yaml", checkpointDoc, nil); c != 201 {
		t.Fatalf("create graph: %d", c)
	}
	// Record a checkpoint the way the engine does before an agent edit.
	ctx := context.Background()
	g, _, err := a.Engine.GetGraph(ctx, "cp-api", 0)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.NewManager(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := ws.Ensure(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, _, err := ws.RecordCheckpoint(ctx, workspace.CheckpointScope{GraphID: "cp-api", NodeID: "dev", Dir: dir, Base: dir}, workspace.CheckpointMeta{ExecutionID: "exe-1", Reason: workspace.CheckpointBeforeEdit, Tool: "filesystem.write"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var list []workspace.Checkpoint
	if c := call(t, srv, "GET", "/api/v1/graphs/cp-api/checkpoints", viewerTok, "", "", &list); c != 200 || len(list) != 1 || list[0].ID != cp.ID || list[0].Tool != "filesystem.write" {
		t.Fatalf("viewer list: %d %+v", c, list)
	}
	if c := call(t, srv, "GET", "/api/v1/graphs/cp-api/checkpoints", "", "", "", nil); c != 401 {
		t.Fatalf("anonymous list: %d", c)
	}
	if c := call(t, srv, "GET", "/api/v1/graphs/ghost/checkpoints", viewerTok, "", "", nil); c != 404 {
		t.Fatalf("unknown graph: %d", c)
	}
	body := `{"checkpoint":"` + cp.ID + `","confirm":true}`
	for _, tok := range []string{"", viewerTok, operatorTok} {
		if c := call(t, srv, "POST", "/api/v1/graphs/cp-api/checkpoints/restore", tok, "application/json", body, nil); c != 401 && c != 403 {
			t.Fatalf("restore with token %q: %d", tok, c)
		}
	}
	for _, bad := range []string{`{"checkpoint":"` + cp.ID + `"}`, `{"checkpoint":"` + cp.ID + `","confirm":true,"path":"/etc"}`, `{"checkpoint":"../x","confirm":true}`, `{"confirm":true}`, `not json`} {
		if c := call(t, srv, "POST", "/api/v1/graphs/cp-api/checkpoints/restore", adminTok, "application/json", bad, nil); c != 400 {
			t.Fatalf("restore %s: %d", bad, c)
		}
	}
	if b, _ := os.ReadFile(file); string(b) != "bad\n" {
		t.Fatal("a refused request changed the workspace")
	}
	if c := call(t, srv, "POST", "/api/v1/graphs/cp-api/checkpoints/restore", adminTok, "application/json", `{"checkpoint":"dev/r0/9","confirm":true}`, nil); c != 404 {
		t.Fatalf("unknown checkpoint: %d", c)
	}
	var res workspace.RestoreResult
	if c := call(t, srv, "POST", "/api/v1/graphs/cp-api/checkpoints/restore", adminTok, "application/json", body, &res); c != 200 || res.Restored.ID != cp.ID || res.Saved.Reason != workspace.CheckpointBeforeRestore {
		t.Fatalf("admin restore: %d %+v", c, res)
	}
	if b, _ := os.ReadFile(file); string(b) != "good\n" {
		t.Fatalf("a.txt = %q", b)
	}
	evs, err := a.Store.ListEvents(ctx, storage.EventFilter{GraphID: "cp-api", Types: []domain.EventType{domain.EvCheckpointRestored}, Limit: 10})
	if err != nil || len(evs) != 1 || evs[0].Actor != "admin" || evs[0].NodeID != "dev" {
		t.Fatalf("audit %+v %v", evs, err)
	}
	// Newest first: the state before the restore leads the list.
	if c := call(t, srv, "GET", "/api/v1/graphs/cp-api/checkpoints", viewerTok, "", "", &list); c != 200 || len(list) != 2 || list[0].ID != res.Saved.ID {
		t.Fatalf("list after restore: %d %+v", c, list)
	}
	// The node and replica alone restore the newest checkpoint recorded before an edit.
	if c := call(t, srv, "POST", "/api/v1/graphs/cp-api/checkpoints/restore", adminTok, "application/json", `{"node":"dev","replica":0,"confirm":true}`, &res); c != 200 || res.Restored.ID != cp.ID {
		t.Fatalf("restore latest: %d %+v", c, res)
	}
}
