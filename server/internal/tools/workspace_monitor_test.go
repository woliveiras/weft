package tools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func monitorFixture(t *testing.T) (*WorkspaceMonitor, *captureAuditor, string) {
	t.Helper()
	dir := t.TempDir()
	aud := &captureAuditor{}
	m := &WorkspaceMonitor{Auditor: aud, Targets: func(context.Context) ([]WorkspaceTarget, error) {
		return []WorkspaceTarget{{GraphID: "project", Dir: dir}}, nil
	}}
	return m, aud, dir
}

func monitorData(t *testing.T, aud *captureAuditor) map[string]any {
	t.Helper()
	ev := aud.evs[len(aud.evs)-1]
	if ev.ExecutionID != "" || ev.NodeID != "" || ev.Actor != "workspace-monitor" {
		t.Fatalf("invented author: %+v", ev)
	}
	var data map[string]any
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMonitorBaselineUnchangedAndRecovery(t *testing.T) {
	m, aud, dir := monitorFixture(t)
	ctx := context.Background()
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if data := monitorData(t, aud); data["status"] != "baseline" || data["gap"] != true {
		t.Fatalf("%v", data)
	}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(aud.evs) != 1 {
		t.Fatal("unchanged workspace emitted events")
	}
	targets := m.Targets
	m.Targets = func(context.Context) ([]WorkspaceTarget, error) { return nil, errors.New("store unavailable") }
	if m.Scan(ctx) == nil {
		t.Fatal("discovery error hidden")
	}
	m.Targets = targets
	if err := os.WriteFile(filepath.Join(dir, "external"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if data := monitorData(t, aud); data["gap"] != true || data["attribution"] != "unknown" {
		t.Fatalf("%v", data)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if !errors.Is(m.Scan(ctx), context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}

// Real independent processes write concurrently without invoking the gateway.
func TestMonitorExternalConcurrentProcessesIntegration(t *testing.T) {
	m, aud, dir := monitorFixture(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "shared"), []byte("baseline"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			cmd := exec.Command("sh", "-c", `printf changed > "$1"; printf concurrent > shared`, "writer", name)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: %v %s", name, err, out)
			}
		}(name)
	}
	wg.Wait()
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	data := monitorData(t, aud)
	if data["attribution"] != "unknown" || len(data["changes"].([]any)) != 3 {
		t.Fatalf("%v", data)
	}
	// The process remains alive across a scan, then writes after its launcher exits.
	cmd := exec.Command("sh", "-c", `(sleep 0.1; printf later > background) >/dev/null 2>&1 &`)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	eventuallyMonitorFile(t, m, aud, dir, "background")
}

func eventuallyMonitorFile(t *testing.T, m *WorkspaceMonitor, aud *captureAuditor, dir, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			if err := m.Scan(context.Background()); err != nil {
				t.Fatal(err)
			}
			data := monitorData(t, aud)
			for _, change := range data["changes"].([]any) {
				if change.(map[string]any)["path"] == name {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background change was not observed")
}

func TestMonitorUnavailableWorkspaceAndRecovery(t *testing.T) {
	m, aud, dir := monitorFixture(t)
	ctx := context.Background()
	// An unreadable/missing root cannot imply that all its files were deleted.
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	parked := dir + "-parked"
	if err := os.Rename(dir, parked); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(parked, dir)
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	data := monitorData(t, aud)
	if data["status"] != "partial" || data["gap"] != true || len(data["changes"].([]any)) != 0 {
		t.Fatalf("%v", data)
	}
	if err := os.Rename(parked, dir); err != nil {
		t.Fatal(err)
	}
	if err := m.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	data = monitorData(t, aud)
	if data["gap"] != true {
		t.Fatal("gap was hidden on recovery")
	}
}

func TestMonitorRunStopsWithController(t *testing.T) {
	m, _, _ := monitorFixture(t)
	m.Interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx, slog.Default()) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop")
	}
}
