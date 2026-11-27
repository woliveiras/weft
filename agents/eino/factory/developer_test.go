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

func TestNextDevelopmentProgress(t *testing.T) {
	for _, tc := range []struct {
		state, id string
		want      int
	}{
		{"2", "retry", 3},
		{`{"iteration":3,"inputId":"qa"}`, "qa", 3},
		{`{"iteration":3,"inputId":"qa"}`, "another", 4},
		{`{"iteration":2}`, "", 3},
		{`{invalid}`, "qa", 0},
		{"0", "qa", 0},
	} {
		got, err := nextDevelopmentProgress([]byte(tc.state), tc.id)
		if tc.want == 0 {
			if err == nil {
				t.Error("expected invalid saved state", tc.state)
			}
			continue
		}
		if err != nil || got.Iteration != tc.want || got.InputID != tc.id {
			t.Errorf("state %s, input %q: %+v %v", tc.state, tc.id, got, err)
		}
	}
}
