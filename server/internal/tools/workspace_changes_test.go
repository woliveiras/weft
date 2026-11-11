package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestWorkspaceSnapshotAndDiff(t *testing.T) {
	dir := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("old", "a")
	write("gone", "b")
	before := snapshotWorkspace(context.Background(), dir)
	write("old", "c")
	write("new", "d")
	if err := os.Remove(filepath.Join(dir, "gone")); err != nil {
		t.Fatal(err)
	}
	changes, complete := diffWorkspace(before, snapshotWorkspace(context.Background(), dir))
	want := []workspaceChange{{"gone", "deleted"}, {"new", "created"}, {"old", "modified"}}
	if !complete || !reflect.DeepEqual(changes, want) {
		t.Fatalf("%+v complete=%v", changes, complete)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "outside")); err != nil {
		t.Fatal(err)
	}
	snap := snapshotWorkspace(context.Background(), dir)
	if !snap.complete || len(snap.files) != 3 {
		t.Fatalf("symlink was followed: %+v", snap)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if snapshotWorkspace(ctx, dir).complete || snapshotWorkspace(context.Background(), filepath.Join(dir, "missing")).complete {
		t.Fatal("failed scan reported complete")
	}
}

func TestWorkspacePartialDoesNotInventCreatesOrDeletes(t *testing.T) {
	before := workspaceSnapshot{files: map[string]string{"old": "a"}, complete: false}
	after := workspaceSnapshot{files: map[string]string{"new": "b"}, complete: false}
	changes, complete := diffWorkspace(before, after)
	if complete || len(changes) != 0 {
		t.Fatalf("%v %v", changes, complete)
	}
	for i := 0; i < maxWorkspaceChanges+1; i++ {
		after.files[strings.Repeat("x", i+1)] = "value"
	}
	before.complete = true
	after.complete = true
	changes, complete = diffWorkspace(before, after)
	if complete || len(changes) != maxWorkspaceChanges {
		t.Fatal("missing change limit")
	}
}

func TestReadOnlyOverridesMutationGrants(t *testing.T) {
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, &captureAuditor{})
	p := principal(t.TempDir(), domain.ToolGrant{Name: "*"})
	p.WorkspaceReadOnly = true
	for _, name := range []string{"filesystem.write", "filesystem.edit", "filesystem.move", "filesystem.mkdir", "filesystem.delete", "git.commit", "git.checkout", "git.status", "git.diff", "git.show", "shell.exec"} {
		if _, err := gw.Invoke(context.Background(), p, name, json.RawMessage(`{}`)); !errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if result, err := gw.Invoke(context.Background(), p, "filesystem.list", nil); err != nil || result.IsError {
		t.Fatalf("read denied: %v %+v", err, result)
	}
}

// Integration: the real gateway, filesystem tools and OS shell share the same
// observation pipeline, including commands that write and then fail.
func TestWorkspaceGatewayShellIntegration(t *testing.T) {
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "filesystem.write"}, domain.ToolGrant{Name: "shell.exec"})
	ctx := context.Background()
	if res, err := gw.Invoke(ctx, p, "filesystem.write", json.RawMessage(`{"path":"file","content":"before"}`)); err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	if res, err := gw.Invoke(ctx, p, "shell.exec", json.RawMessage(`{"command":"printf after > file; printf created > new; exit 7"}`)); err != nil || !strings.Contains(res.Content, "exit code: 7") {
		t.Fatalf("%v %+v", err, res)
	}
	var observations []map[string]json.RawMessage
	for _, ev := range aud.evs {
		if ev.Type != "workspace.observed" {
			continue
		}
		if ev.ExecutionID != p.ExecutionID || ev.NodeID != p.NodeID {
			t.Fatal("missing attribution")
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatal(err)
		}
		observations = append(observations, data)
	}
	if len(observations) != 2 {
		t.Fatalf("observations: %v", observations)
	}
	var changes []workspaceChange
	if err := json.Unmarshal(observations[1]["changes"], &changes); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes, []workspaceChange{{"file", "modified"}, {"new", "created"}}) {
		t.Fatalf("%+v", changes)
	}
	p.WorkspaceReadOnly = true
	if _, err := gw.Invoke(ctx, p, "shell.exec", json.RawMessage(`{"command":"printf forbidden > file"}`)); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(p.Workdir, "file"))
	if err != nil || string(b) != "after" {
		t.Fatalf("read-only write: %s %v", b, err)
	}
}

func TestDockerReadOnlyWorkspaceMount(t *testing.T) {
	sb, err := NewDockerSandbox(DockerSandboxConfig{Name: "reader", Image: "fixture", HostDir: "/fixture", WorkspaceReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(sb.RunArgs(), " "), "/fixture:/workspace:ro") {
		t.Fatal("workspace mount is writable")
	}
}

func TestReadOnlyRefusesExistingWritableContainer(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf true"), 0700); err != nil {
		t.Fatal(err)
	}
	sb, err := NewDockerSandbox(DockerSandboxConfig{Name: "reader", Image: "fixture", HostDir: "/fixture", WorkspaceReadOnly: true, DockerBin: bin})
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("%v", err)
	}
}

func TestWorkspaceSnapshotReportsByteLimit(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxWorkspaceBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if snapshotWorkspace(context.Background(), dir).complete {
		t.Fatal("oversized scan reported complete")
	}
}

func TestWorkspaceObservationSurvivesCancellation(t *testing.T) {
	aud := &captureAuditor{}
	gw := NewGateway(Builtin(BuiltinConfig{}), nil, aud)
	p := principal(t.TempDir(), domain.ToolGrant{Name: "shell.exec"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = gw.Invoke(ctx, p, "shell.exec", json.RawMessage(`{"command":"printf changed > cancelled.txt; sleep 30"}`))
	}()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	defer cancel()
waitForWrite:
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(p.Workdir, "cancelled.txt")); err == nil {
				break waitForWrite
			}
		case <-deadline:
			cancel()
			<-done
			t.Fatal("shell did not write")
		}
	}
	cancel()
	<-done
	for _, ev := range aud.evs {
		if ev.Type == "workspace.observed" && strings.Contains(string(ev.Data), "cancelled.txt") {
			return
		}
	}
	t.Fatal("cancelled command lost its change observation")
}
