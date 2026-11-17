package environment

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

func contextEnv() *domain.EnvironmentSpec {
	return &domain.EnvironmentSpec{Context: ".", Dockerfile: "FROM alpine\nCOPY . /app", Origin: &domain.EnvironmentOrigin{DockerfilePath: "Dockerfile"}}
}
func snapshotEntries(t *testing.T, s *ContextSnapshot) map[string]string {
	t.Helper()
	out := map[string]string{}
	reader := tar.NewReader(s.File)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimPrefix(header.Name, contextFiles+"/")] = string(data)
	}
	return out
}

func TestContextIgnoreAndStableHash(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"src/app.txt": "hello", "build/drop.txt": "omit", "build/keep.txt": "keep", ".git/config": "synthetic", ".weft/worker/r0/log": "internal", ".env": "synthetic", ".env.local": "synthetic", ".dockerignore": "build/**\n!build/keep.txt\n!.env\n!.git/config\n"} {
		writeDiscoveryFile(t, dir, name, body)
	}
	env := contextEnv()
	first, err := SnapshotContext(context.Background(), dir, env)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	entries := snapshotEntries(t, first)
	if entries["src/app.txt"] != "hello" || entries["build/keep.txt"] != "keep" {
		t.Fatalf("missing included files: %+v", entries)
	}
	for _, name := range []string{"build/drop.txt", ".weft/worker/r0/log", ".git/config", ".env", ".env.local"} {
		if _, ok := entries[name]; ok {
			t.Fatalf("included excluded %s", name)
		}
	}
	if entries[contextDockerfile] != "FROM alpine\nCOPY [\"context/.\",\"/app\"]\n" {
		t.Fatal("missing accepted Dockerfile")
	}
	writeDiscoveryFile(t, dir, "build/drop.txt", "ignored changed")
	now := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "src/app.txt"), now, now); err != nil {
		t.Fatal(err)
	}
	same, err := SnapshotContext(context.Background(), dir, env)
	if err != nil {
		t.Fatal(err)
	}
	defer same.Close()
	if first.SHA256 != same.SHA256 {
		t.Fatal("mtime or ignored contents changed fingerprint")
	}
	writeDiscoveryFile(t, dir, "src/app.txt", "changed")
	changed, err := SnapshotContext(context.Background(), dir, env)
	if err != nil {
		t.Fatal(err)
	}
	defer changed.Close()
	if first.SHA256 == changed.SHA256 {
		t.Fatal("included bytes did not change fingerprint")
	}
	if err := os.Chmod(filepath.Join(dir, "src/app.txt"), 0755); err != nil {
		t.Fatal(err)
	}
	mode, err := SnapshotContext(context.Background(), dir, env)
	if err != nil {
		t.Fatal(err)
	}
	defer mode.Close()
	if changed.SHA256 == mode.SHA256 {
		t.Fatal("mode change did not affect fingerprint")
	}
}

func TestContextSpecificIgnoreAndSubdirectory(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"service/app": "yes", "outside": "no", "service/drop": "no", "service/.dockerignore": "app\n", "docker/App.Dockerfile.dockerignore": "drop\n"} {
		writeDiscoveryFile(t, dir, name, body)
	}
	env := contextEnv()
	env.Context = "service"
	env.Origin.DockerfilePath = "docker/App.Dockerfile"
	s, err := SnapshotContext(context.Background(), dir, env)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries := snapshotEntries(t, s)
	if entries["app"] != "yes" {
		t.Fatalf("specific ignore did not win: %+v", entries)
	}
	for _, name := range []string{"outside", "drop"} {
		if _, ok := entries[name]; ok {
			t.Fatalf("unexpected %s", name)
		}
	}
	writeDiscoveryFile(t, dir, "docker/App.Dockerfile.dockerignore", "drop\n# reviewed change\n")
	next, err := SnapshotContext(context.Background(), dir, env)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if s.SHA256 == next.SHA256 {
		t.Fatal("ignore file change not recorded")
	}
}

func TestContextRejectsBoundariesAndCleansTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	writeDiscoveryFile(t, dir, "app", strings.Repeat("x", 100))
	env := contextEnv()
	if _, err := snapshotContext(context.Background(), dir, env, 50, 100); err == nil {
		t.Fatal("accepted oversized context")
	}
	if _, err := snapshotContext(context.Background(), dir, env, 1000, 0); err == nil {
		t.Fatal("accepted excessive entries")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SnapshotContext(ctx, dir, env); err == nil {
		t.Fatal("ignored cancellation")
	}
	for _, name := range []string{"../outside", "/tmp", "missing"} {
		bad := *env
		bad.Context = name
		if _, err := SnapshotContext(context.Background(), dir, &bad); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	outside := t.TempDir()
	writeDiscoveryFile(t, outside, "file", "outside")
	if err := os.Symlink(filepath.Join(outside, "file"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotContext(context.Background(), dir, env); err == nil {
		t.Fatal("accepted symlink")
	}
	writeDiscoveryFile(t, dir, ".dockerignore", "link\n")
	s, err := SnapshotContext(context.Background(), dir, env)
	if err != nil {
		t.Fatal(err)
	}
	name := s.File.Name()
	s.Close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("snapshot not removed: %v", err)
	}
	writeDiscoveryFile(t, dir, ".dockerignore", "[\n")
	if _, err := SnapshotContext(context.Background(), dir, env); err == nil {
		t.Fatal("accepted invalid ignore pattern")
	}
}

func TestCopyAndAddLocalOperands(t *testing.T) {
	for _, command := range []string{"COPY . /app", "ADD archive.tar /app/", `COPY ["file with spaces", "/app/"]`, "COPY src/*.go go.mod /app/"} {
		if err := validateRepositoryDockerfile("FROM alpine\n" + command); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	for _, command := range []string{"COPY ../outside /app", "ADD https://example.com/file /app", "ADD git@github.com:org/repo /app", "COPY $SOURCE /app", "COPY /etc/passwd /app", "COPY --from=other@ /file /app", `COPY ["app"]`, "COPY app"} {
		if err := validateRepositoryDockerfile("FROM alpine\n" + command); err == nil {
			t.Fatalf("accepted %s", command)
		}
	}
}

func TestAcceptedContextInvalidatesOnFilesAndChangesImageHash(t *testing.T) {
	dir := t.TempDir()
	writeDiscoveryFile(t, dir, "Dockerfile", "FROM alpine\nCOPY app /app")
	writeDiscoveryFile(t, dir, "app", "one")
	selection := discoverSelection(t, dir, "")
	env, err := Resolve(context.Background(), dir, selection)
	if err != nil {
		t.Fatal(err)
	}
	if env.Context != "." || env.Origin.ContextSHA256 == "" {
		t.Fatalf("context not persisted: %+v", env)
	}
	before := Hash(env)
	writeDiscoveryFile(t, dir, "app", "two")
	if err := ValidateAccepted(context.Background(), dir, env); err == nil {
		t.Fatal("accepted changed context")
	}
	next, err := Resolve(context.Background(), dir, selection)
	if err != nil {
		t.Fatal(err)
	}
	if Hash(next) == before {
		t.Fatal("context changes did not invalidate build hash")
	}
}

func TestContextBuildfileScopesAllLocalSources(t *testing.T) {
	input := "FROM alpine\nCOPY src/*.go \\\n go.mod /app/\nADD [\"archive with spaces.tar\",\"/app/\"]\nCOPY . /all\nRUN echo done\n"
	got, err := contextBuildfile(input)
	if err != nil {
		t.Fatal(err)
	}
	want := "FROM alpine\nCOPY [\"context/src/*.go\",\"context/go.mod\",\"/app/\"]\nADD [\"context/archive with spaces.tar\",\"/app/\"]\nCOPY [\"context/.\",\"/all\"]\nRUN echo done\n"
	if got != want {
		t.Fatalf("buildfile:\n%s\nwant:\n%s", got, want)
	}
	if _, err := contextBuildfile("FROM alpine\nCOPY ../outside /app"); err == nil {
		t.Fatal("rewrote unsafe source")
	}
}

func TestResolveContextFromWeftAndCompose(t *testing.T) {
	for _, tc := range []struct{ path, body, service string }{
		{".weft/environment.yaml", "dockerfile: build/Containerfile\ncontext: service\n", ""},
		{"compose.yaml", "services: {dev: {build: {context: service, dockerfile: Containerfile}}}", "dev"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			dir := t.TempDir()
			writeDiscoveryFile(t, dir, tc.path, tc.body)
			dockerfile := "build/Containerfile"
			if tc.service != "" {
				dockerfile = "service/Containerfile"
			}
			writeDiscoveryFile(t, dir, dockerfile, "FROM alpine\nCOPY app /app\n")
			writeDiscoveryFile(t, dir, "service/app", "included")
			writeDiscoveryFile(t, dir, "outside", "excluded")
			env, err := Resolve(context.Background(), dir, discoverSelection(t, dir, tc.service))
			if err != nil {
				t.Fatal(err)
			}
			if env.Context != "service" || env.Origin.DockerfilePath != dockerfile || env.Origin.ContextSHA256 == "" {
				t.Fatalf("context metadata: %+v", env)
			}
			snapshot, err := SnapshotContext(context.Background(), dir, env)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			entries := snapshotEntries(t, snapshot)
			if entries["app"] != "included" {
				t.Fatal("missing context source")
			}
			if _, ok := entries["outside"]; ok {
				t.Fatal("included outside context")
			}
			writeDiscoveryFile(t, dir, "outside", "outside changed")
			if err := ValidateAccepted(context.Background(), dir, env); err != nil {
				t.Fatalf("unrelated file invalidated context: %v", err)
			}
		})
	}
}

func TestRuntimeScratchDoesNotInvalidateAcceptedContext(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeDiscoveryFile(t, dir, "Dockerfile", "FROM alpine:3.22\nCOPY . /app\n")
	writeDiscoveryFile(t, dir, ".dockerignore", "!.weft/**\n")
	discovery, err := Discover(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	candidate := discovery.Candidates[0]
	env, err := Resolve(ctx, dir, Selection{Source: discovery.Source, Path: candidate.Path, SHA256: candidate.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, dir, ".weft/worker/r0/output.txt", "internal runtime data")
	if err := ValidateAccepted(ctx, dir, env); err != nil {
		t.Fatalf("runtime scratch invalidated approval: %v", err)
	}
	snapshot, err := SnapshotContext(ctx, dir, env)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for name := range snapshotEntries(t, snapshot) {
		if strings.HasPrefix(name, ".weft/") {
			t.Fatalf("runtime data exposed: %s", name)
		}
	}
	writeDiscoveryFile(t, dir, "Dockerfile", "FROM alpine:3.22\nRUN echo changed\n")
	if err := ValidateAccepted(ctx, dir, env); err == nil {
		t.Fatal("changed definition accepted")
	}
}
