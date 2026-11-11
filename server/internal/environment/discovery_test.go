package environment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectDefinition(t *testing.T) {
	for _, tc := range []struct {
		name, source, body string
		valid              bool
	}{
		{"image", "weft", "image: alpine", true},
		{"unknown", "weft", "image: alpine\nprivileged: true", false},
		{"both", "weft", "image: alpine\ndockerfile: FROM alpine", false},
		{"multi", "weft", "image: alpine\n---\nimage: other", false},
		{"malformed", "weft", "[", false},
		{"devcontainer", "devcontainer", `{"image":"node:22","postCreateCommand":"npm ci"}`, true},
		{"devcontainer unsafe", "devcontainer", `{"image":"node:22","privileged":true}`, false},
		{"devcontainer command", "devcontainer", `{"image":"node:22","postCreateCommand":{}}`, false},
		{"dockerfile", "dockerfile", "FROM alpine\nRUN true", true},
		{"invalid dockerfile", "dockerfile", "RUN true", false},
		{"compose", "compose", "services:\n  web: {image: node}\n  db: {image: postgres}", true},
		{"compose empty", "compose", "services: {}", false},
		{"compose invalid service", "compose", "services: {web: []}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := inspectDefinition(tc.source, []byte(tc.body))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func writeDiscoveryFile(t *testing.T, dir, name, body string) {
	t.Helper()
	target := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverFilesystemPriority(t *testing.T) {
	files := []struct{ name, body, source string }{
		{".weft/environment.yaml", "image: alpine", "weft"},
		{".devcontainer/devcontainer.json", `{"image":"node"}`, "devcontainer"},
		{"tools/Dockerfile.dev", "FROM alpine", "dockerfile"},
		{"compose.yaml", "services: {web: {image: node}}", "compose"},
	}
	for i := range files {
		t.Run(files[i].source, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range files[i:] {
				writeDiscoveryFile(t, dir, f.name, f.body)
			}
			got, err := Discover(context.Background(), dir)
			if err != nil || got.Source != files[i].source || got.Status != "review_required" || len(got.Candidates) != 1 || len(got.Candidates[0].SHA256) != 64 {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestDiscoverBoundaries(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := Discover(context.Background(), t.TempDir())
		if err != nil || got.Status != "proposal_required" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		_, err := Discover(context.Background(), filepath.Join(t.TempDir(), "missing"))
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Discover(ctx, t.TempDir())
		if err == nil {
			t.Fatal("expected error")
		}
	})
	for _, body := range []string{"invalid", strings.Repeat("x", maxDefinitionBytes+1)} {
		t.Run("no fallback", func(t *testing.T) {
			dir := t.TempDir()
			writeDiscoveryFile(t, dir, ".weft/environment.yaml", body)
			writeDiscoveryFile(t, dir, "Dockerfile", "FROM alpine")
			got, err := Discover(context.Background(), dir)
			if err != nil || got.Source != "weft" || got.Status != "invalid" {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		outside := t.TempDir()
		writeDiscoveryFile(t, outside, "environment.yaml", "image: secret")
		if err := os.Symlink(outside, filepath.Join(dir, ".weft")); err != nil {
			t.Fatal(err)
		}
		got, err := Discover(context.Background(), dir)
		if err == nil && (got.Status != "invalid" || got.Candidates[0].SHA256 != "") {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("ambiguous and ignored", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"b/Dockerfile", "a/Dockerfile", "node_modules/Dockerfile"} {
			writeDiscoveryFile(t, dir, name, "FROM alpine")
		}
		got, err := Discover(context.Background(), dir)
		if err != nil || len(got.Candidates) != 2 || got.Candidates[0].Path != "a/Dockerfile" {
			t.Fatalf("%+v %v", got, err)
		}
	})
}
