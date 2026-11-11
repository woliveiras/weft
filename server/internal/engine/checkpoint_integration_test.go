package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/queue/memory"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// scriptProvider answers each round with the next scripted tool calls; a
// nil round fails the model call, an empty one ends the turn. A round may
// wait on a gate first.
type scriptProvider struct {
	mu      sync.Mutex
	rounds  [][]llm.ToolCall
	n       int
	results []string
	gate    map[int]chan struct{}
	reached map[int]chan struct{}
}

func (*scriptProvider) Name() string                                    { return "script" }
func (*scriptProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *scriptProvider) Chat(ctx context.Context, r *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.mu.Lock()
	i := p.n
	p.n++
	if last := r.Messages[len(r.Messages)-1]; last.Role == llm.RoleTool {
		p.results = append(p.results, last.Content)
	}
	gate, reached := p.gate[i], p.reached[i]
	p.mu.Unlock()
	if reached != nil {
		close(reached)
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if i >= len(p.rounds) || p.rounds[i] == nil {
		return nil, errors.New("scripted model failure")
	}
	if len(p.rounds[i]) == 0 {
		return &llm.ChatResponse{Message: llm.ChatMessage{Content: "done"}}, nil
	}
	return &llm.ChatResponse{Message: llm.ChatMessage{ToolCalls: p.rounds[i]}}, nil
}

func (p *scriptProvider) toolResults() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.results...)
}

func call(id, name string, args any) llm.ToolCall {
	b, _ := json.Marshal(args)
	return llm.ToolCall{ID: id, Name: name, Arguments: b}
}

func write(id, path, content string) llm.ToolCall {
	return call(id, "filesystem.write", map[string]any{"path": path, "content": content})
}

// checkpointRig is an engine whose registry carries the checkpoint tools.
func checkpointRig(t *testing.T, p *scriptProvider) *rig {
	t.Helper()
	dir := t.TempDir()
	var reg *tools.Registry
	r := newRig(t, openStore(t, dir), memory.New(), dir, "checkpoint-instance", func(o *engine.Options) {
		o.Models.(*llm.Registry).Add(llm.ProviderConfig{Name: "script"}, p)
		reg = o.Tools.(gwAdapter).reg
	})
	reg.MustRegister(r.eng.CheckpointTools()...)
	return r
}

func checkpointGraph(id, grants string) string {
	return fmt.Sprintf("metadata: {name: %s}\nspec:\n  workspace: {git: true}\n  nodes:\n    - id: dev\n      runtime: {kind: native}\n      prompt: Edit the files\n      model: {provider: script, name: fixture}\n      loop: {mode: once}\n      isolation: {mode: process}\n      tools: [%s]\n      limits: {turnTimeout: 20s, maxToolCalls: 20}\n", id, grants)
}

func finishedExecution(t *testing.T, r *rig, graphID string) *domain.Execution {
	t.Helper()
	var x *domain.Execution
	eventually(t, 15*time.Second, "turn finished", func() bool {
		xs, _ := r.store.ListExecutions(r.ctx, storage.ExecutionFilter{GraphID: graphID, NodeID: "dev", Limit: 1})
		if len(xs) == 1 && xs[0].Status.Terminal() {
			x = xs[0]
			return true
		}
		return false
	})
	return x
}

func checkpointEvents(t *testing.T, r *rig, graphID string) []*domain.Event {
	t.Helper()
	evs, err := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: graphID, Types: []domain.EventType{domain.EvCheckpointCreated, domain.EvCheckpointRestored, domain.EvCheckpointFailed}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func eventData(t *testing.T, ev *domain.Event) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func fileContent(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "<absent>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// One checkpoint per round before its first edit, an agent restore under an
// explicit grant, a failed turn that restores nothing, and a person's
// restore that undoes the agent's.
func TestCheckpointsAcrossANativeTurn(t *testing.T) {
	p := &scriptProvider{rounds: [][]llm.ToolCall{
		{write("w1", "a.txt", "v1\n")},
		{write("w2", "a.txt", "v2\n"), write("w3", "b.txt", "b\n")},
		{call("l1", "checkpoint.list", map[string]any{})},
		{call("r1", "checkpoint.restore", map[string]any{})},
		nil, // the model fails: the turn fails
	}}
	r := checkpointRig(t, p)
	r.deploy(checkpointGraph("cp", "filesystem.write, filesystem.read, checkpoint.list, checkpoint.restore"))
	x := finishedExecution(t, r, "cp")
	if x.Status != domain.ExecFailed {
		t.Fatalf("turn %s, want failed", x.Status)
	}
	ws := filepath.Join(r.dir, "ws", "cp")
	// The agent restored the checkpoint before round 2; the failure restored nothing.
	if fileContent(t, filepath.Join(ws, "a.txt")) != "v1\n" || fileContent(t, filepath.Join(ws, "b.txt")) != "<absent>" {
		t.Fatalf("workspace after the failed turn: a=%q b=%q", fileContent(t, filepath.Join(ws, "a.txt")), fileContent(t, filepath.Join(ws, "b.txt")))
	}
	results := p.toolResults()
	if len(results) < 4 || !strings.Contains(results[2], "dev/r0/2") || !strings.Contains(results[2], "before filesystem.write") ||
		!strings.Contains(results[3], "Restored checkpoint dev/r0/2") || !strings.Contains(results[3], "checkpoint dev/r0/3") {
		t.Fatalf("tool results %q", results)
	}

	list, err := r.eng.ListCheckpoints(r.ctx, "cp")
	if err != nil || len(list) != 3 {
		t.Fatalf("checkpoints %+v %v", list, err)
	}
	if list[0].Reason != workspace.CheckpointBeforeRestore || list[1].Reason != workspace.CheckpointBeforeEdit || list[1].ExecutionID != x.ID || list[1].Tool != "filesystem.write" {
		t.Fatalf("checkpoints %+v", list)
	}
	// HEAD and the branch did not move.
	out, err := exec.Command("git", "-C", ws, "log", "--format=%s").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "weft: initialise workspace" {
		t.Fatalf("history changed: %s %v", out, err)
	}

	evs := checkpointEvents(t, r, "cp")
	created, restored := 0, 0
	for _, ev := range evs {
		switch ev.Type {
		case domain.EvCheckpointCreated:
			created++
			if ev.ExecutionID != x.ID || ev.NodeID != "dev" || ev.Actor != "agent:dev#0" || eventData(t, ev)["tool"] != "filesystem.write" {
				t.Fatalf("created event %+v", ev)
			}
		case domain.EvCheckpointRestored:
			restored++
			if d := eventData(t, ev); d["source"] != "agent" || d["checkpoint"] != "dev/r0/2" || d["saved"] != "dev/r0/3" || ev.Actor != "agent:dev#0" {
				t.Fatalf("restored event %+v %v", ev, d)
			}
		default:
			t.Fatalf("unexpected %s: %s", ev.Type, ev.Data)
		}
	}
	if created != 2 || restored != 1 {
		t.Fatalf("created=%d restored=%d", created, restored)
	}

	// A person undoes the agent's restore.
	res, err := r.eng.RestoreCheckpoint(r.ctx, "cp", "", 0, "dev/r0/3", "alice")
	if err != nil || res.Restored.ID != "dev/r0/3" {
		t.Fatalf("restore %+v %v", res, err)
	}
	if fileContent(t, filepath.Join(ws, "a.txt")) != "v2\n" || fileContent(t, filepath.Join(ws, "b.txt")) != "b\n" {
		t.Fatal("person's restore not applied")
	}
	evs = checkpointEvents(t, r, "cp")
	last := evs[0] // newest first
	if last.Type != domain.EvCheckpointRestored || last.Actor != "alice" || eventData(t, last)["source"] != "api" {
		t.Fatalf("person's restore event %+v", last)
	}
	if _, err := r.eng.RestoreCheckpoint(r.ctx, "cp", "", 0, "dev/r0/99", "alice"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("unknown checkpoint: %v", err)
	}
	if _, err := r.eng.RestoreCheckpoint(r.ctx, "cp", "other", 0, "dev/r0/1", "alice"); !errors.Is(err, engine.ErrInvalid) {
		t.Fatalf("mismatched node: %v", err)
	}
	if _, err := r.eng.RestoreCheckpoint(r.ctx, "cp", "ghost", 0, "", "alice"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("unknown node: %v", err)
	}
}

// Without a grant naming checkpoint.restore (a wildcard is not enough) the
// agent cannot restore; checkpoints are still recorded before its edits.
func TestCheckpointRestoreToolNeedsAnExplicitGrant(t *testing.T) {
	p := &scriptProvider{rounds: [][]llm.ToolCall{
		{write("w1", "a.txt", "v1\n")},
		{write("w2", "a.txt", "v2\n")},
		{call("r1", "checkpoint.restore", map[string]any{})},
		{},
	}}
	r := checkpointRig(t, p)
	r.deploy(checkpointGraph("nogrant", `"*"`))
	x := finishedExecution(t, r, "nogrant")
	if x.Status != domain.ExecSucceeded {
		t.Fatalf("turn %s: %s", x.Status, x.Error)
	}
	if got := fileContent(t, filepath.Join(r.dir, "ws", "nogrant", "a.txt")); got != "v2\n" {
		t.Fatalf("a.txt = %q: the restore ran without a grant", got)
	}
	results := p.toolResults()
	if len(results) < 3 || !strings.Contains(results[2], "permission denied") {
		t.Fatalf("tool results %q", results)
	}
	denied, err := r.store.ListEvents(r.ctx, storage.EventFilter{GraphID: "nogrant", Types: []domain.EventType{domain.EvToolDenied}, Limit: 10})
	if err != nil || len(denied) != 1 || eventData(t, denied[0])["tool"] != "checkpoint.restore" {
		t.Fatalf("denied events %+v %v", denied, err)
	}
	for _, ev := range checkpointEvents(t, r, "nogrant") {
		if ev.Type != domain.EvCheckpointCreated {
			t.Fatalf("unexpected %s", ev.Type)
		}
	}
	if list, err := r.eng.ListCheckpoints(r.ctx, "nogrant"); err != nil || len(list) != 2 {
		t.Fatalf("checkpoints %+v %v", list, err)
	}
}

// A person cannot restore a replica's files under a running turn.
func TestCheckpointRestoreRefusedDuringATurn(t *testing.T) {
	gate, reached := make(chan struct{}), make(chan struct{})
	p := &scriptProvider{rounds: [][]llm.ToolCall{{write("w1", "a.txt", "v1\n")}, {}},
		gate: map[int]chan struct{}{1: gate}, reached: map[int]chan struct{}{1: reached}}
	r := checkpointRig(t, p)
	r.deploy(checkpointGraph("busy", "filesystem.write"))
	select {
	case <-reached:
	case <-time.After(15 * time.Second):
		t.Fatal("second round never started")
	}
	if _, err := r.eng.RestoreCheckpoint(r.ctx, "busy", "dev", 0, "", "alice"); !errors.Is(err, engine.ErrConflict) {
		t.Fatalf("restore during a turn: %v", err)
	}
	close(gate)
	if x := finishedExecution(t, r, "busy"); x.Status != domain.ExecSucceeded {
		t.Fatalf("turn %s", x.Status)
	}
	if _, err := r.eng.RestoreCheckpoint(r.ctx, "busy", "dev", 0, "", "alice"); err != nil {
		t.Fatalf("restore after the turn: %v", err)
	}
	if got := fileContent(t, filepath.Join(r.dir, "ws", "busy", "a.txt")); got != "<absent>" {
		t.Fatalf("a.txt = %q after restoring the checkpoint before the first edit", got)
	}
}

// A plain (non-Git) workspace has no checkpoints; edits still run.
func TestCheckpointsUnavailableOutsideGit(t *testing.T) {
	p := &scriptProvider{rounds: [][]llm.ToolCall{{write("w1", "a.txt", "v1\n")}, {}}}
	r := checkpointRig(t, p)
	r.deploy(strings.Replace(checkpointGraph("plain", "filesystem.write"), "  workspace: {git: true}\n", "", 1))
	if x := finishedExecution(t, r, "plain"); x.Status != domain.ExecSucceeded {
		t.Fatalf("turn %s: %s", x.Status, x.Error)
	}
	if got := fileContent(t, filepath.Join(r.dir, "ws", "plain", "a.txt")); got != "v1\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if evs := checkpointEvents(t, r, "plain"); len(evs) != 0 {
		t.Fatalf("checkpoint events in a plain workspace: %+v", evs)
	}
	if list, err := r.eng.ListCheckpoints(r.ctx, "plain"); err != nil || len(list) != 0 {
		t.Fatalf("list %+v %v", list, err)
	}
	if _, err := r.eng.RestoreCheckpoint(r.ctx, "plain", "dev", 0, "", "alice"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("restore in a plain workspace: %v", err)
	}
}
