package factory

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOfflineScaffoldRunsWithoutDependencies(t *testing.T) {
	dir := t.TempDir()
	change := scaffold("request-t1", "Feature with accents á")
	for _, file := range change.Files {
		if !validChange(file) {
			t.Fatal(file)
		}
		target := filepath.Join(dir, file.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(*file.Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=auto", "GOPROXY=off", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("offline QA: %s: %v", out, err)
	}
	for _, name := range []string{"../a", "/a", ".git/config", "a/../b", "a\\b"} {
		file := change.Files[0]
		file.Path = name
		if validChange(file) {
			t.Fatal("accepted invalid path", name)
		}
	}
}
