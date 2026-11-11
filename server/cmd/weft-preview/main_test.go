package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidRelayArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"connect", "80"}, {"connect", "65536"}, {"connect", "internal:5173"}, {"serve", "missing.json"}} {
		if e := run(args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestSupervisorReportsCommandFailure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	for _, command := range [][]string{{"sh", "-c", "exit 0"}, {"sh", "-c", "exit 7"}, {"nonexistent-preview-command"}} {
		b, e := json.Marshal(map[string]any{"command": command, "cwd": dir})
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		e = run([]string{"serve", p})
		wantSuccess := len(command) > 2 && command[2] == "exit 0"
		if (e == nil) != wantSuccess {
			t.Fatal(command, e)
		}
	}
}
func TestSupervisorRejectsCacheOutsideSandboxHome(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if e := os.WriteFile(p, []byte(`{"command":["sh","-c","exit 0"],"caches":["/etc"]}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := run([]string{"serve", p}); e == nil {
		t.Fatal("unrestricted cache target accepted")
	}
}
