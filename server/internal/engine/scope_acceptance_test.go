package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/instructions"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/runtime"
)

type scopeObservation struct {
	input          *runtime.TurnInput
	content        string
	err            error
	escapesAllowed []string
}
type scopeRuntime struct {
	instructionRuntime
	observations chan scopeObservation
}

func (r scopeRuntime) Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error) {
	observation := scopeObservation{input: in}
	result, err := env.InvokeTool(ctx, "filesystem.read", json.RawMessage(`{"path":"marker.txt"}`))
	observation.err = err
	if result != nil {
		observation.content = fmt.Sprint(result.Content)
	}
	other := "scope-alpha"
	if in.GraphID == other {
		other = "scope-beta"
	}
	for _, path := range []string{"../" + other + "/marker.txt", "escape.txt", filepath.Join(filepath.Dir(in.Workdir), other, "marker.txt")} {
		args, _ := json.Marshal(map[string]string{"path": path})
		if _, err := env.InvokeTool(ctx, "filesystem.read", args); err == nil {
			observation.escapesAllowed = append(observation.escapesAllowed, path)
		}
	}
	r.observations <- observation
	return &runtime.TurnResult{Summary: "scope checked"}, nil
}

// Both graphs use the same node ID and engine/store. Isolation must come from
// platform-selected graph scope, not globally unique node IDs or separate servers.
func TestConcurrentGraphsKeepRuntimeInstructionsTasksAndFilesScoped(t *testing.T) {
	dir := t.TempDir()
	capture := scopeRuntime{observations: make(chan scopeObservation, 2)}
	r := newRig(t, openStore(t, dir), memory.New(), dir, "scope-acceptance", func(o *engine.Options) { o.Runtimes[domain.RuntimeEcho] = capture })
	manager := instructions.Manager{Store: r.store}
	for _, id := range []string{"scope-alpha", "scope-beta"} {
		root := filepath.Join(dir, "ws", id)
		steering := filepath.Join(root, ".weft", "steering")
		if err := os.MkdirAll(steering, 0700); err != nil {
			t.Fatal(err)
		}
		for path, body := range map[string]string{filepath.Join(root, "marker.txt"): id + " file", filepath.Join(steering, "scope.md"): id + " steering"} {
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		other := "scope-alpha"
		if id == other {
			other = "scope-beta"
		}
		if err := os.Symlink(filepath.Join(dir, "ws", other, "marker.txt"), filepath.Join(root, "escape.txt")); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Save(t.Context(), instructions.Input{ID: id, Title: id, Body: id + " skill", Enabled: true}, 0, "test", false); err != nil {
			t.Fatal(err)
		}
		if err := r.store.PutOperatorMessage(t.Context(), &domain.OperatorMessage{ID: id, GraphID: id, NodeID: "worker", Replica: -1, Text: id + " operator", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"scope-alpha", "scope-beta"} {
		r.deploy(fmt.Sprintf("metadata: {name: %s}\nspec:\n  instructions: {projectSteering: true, skillIds: [%s]}\n  nodes:\n    - id: worker\n      prompt: '%s task'\n      runtime: {kind: echo}\n      loop: {mode: once}\n      tools: [{name: filesystem.read}]\n", id, id, id))
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case observation := <-capture.observations:
			in := observation.input
			if seen[in.GraphID] {
				t.Fatal("duplicate graph turn")
			}
			seen[in.GraphID] = true
			if observation.err != nil || observation.content != in.GraphID+" file" || len(observation.escapesAllowed) > 0 {
				t.Fatalf("file isolation: %+v", observation)
			}
			for _, suffix := range []string{" task", " steering", " skill"} {
				if !strings.Contains(in.Prompt, in.GraphID+suffix) {
					t.Fatalf("missing scoped input %s", suffix)
				}
			}
			if len(in.OperatorMessages) != 1 || in.OperatorMessages[0] != in.GraphID+" operator" {
				t.Fatal("operator scope mismatch")
			}
			snapshot, err := manager.Snapshot(t.Context(), in.ExecutionID)
			if err != nil || snapshot.GraphID != in.GraphID || snapshot.NodeID != "worker" {
				t.Fatal("snapshot scope mismatch", err)
			}
			raw, err := json.Marshal(struct {
				Input    *runtime.TurnInput
				Snapshot *instructions.Snapshot
			}{in, snapshot})
			if err != nil {
				t.Fatal(err)
			}
			other := "scope-alpha"
			if in.GraphID == other {
				other = "scope-beta"
			}
			if strings.Contains(string(raw), other) {
				t.Fatal("cross-graph runtime or snapshot disclosure")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("scoped runtime did not finish")
		}
	}
}
