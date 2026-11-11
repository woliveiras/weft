package environment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func discoverSelection(t *testing.T, dir, service string) Selection {
	t.Helper()
	d, err := Discover(context.Background(), dir)
	if err != nil || len(d.Candidates) == 0 {
		t.Fatalf("discovery %+v %v", d, err)
	}
	c := d.Candidates[0]
	return Selection{Source: d.Source, Path: c.Path, SHA256: c.SHA256, Service: service}
}

func TestResolveSources(t *testing.T) {
	for _, tc := range []struct{ name, body, service, image, setup string }{
		{".weft/environment.yaml", "image: alpine\nsetup: [echo ok]", "", "alpine", "echo ok"},
		{".devcontainer/devcontainer.json", `{"image":"node:22","postCreateCommand":"npm ci"}`, "", "node:22", "npm ci"},
		{"Dockerfile.dev", "FROM alpine\nRUN echo ok", "", "", ""},
		{"compose.yaml", "services: {dev: {image: node:22}, db: {image: postgres}}", "dev", "node:22", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeDiscoveryFile(t, dir, tc.name, tc.body)
			sel := discoverSelection(t, dir, tc.service)
			env, err := Resolve(context.Background(), dir, sel)
			if err != nil {
				t.Fatal(err)
			}
			if env.Network.Setup != "bridge" || env.Network.Run != "none" {
				t.Fatalf("implicit network defaults: %+v", env.Network)
			}
			if env.Image != tc.image || env.Origin.Path != tc.name || env.Origin.Files[tc.name] != sel.SHA256 {
				t.Fatalf("%+v", env)
			}
			if tc.setup != "" && (len(env.Setup) != 1 || env.Setup[0] != tc.setup) {
				t.Fatalf("setup %+v", env)
			}
			if err := ValidateAccepted(context.Background(), dir, env); err != nil {
				t.Fatal(err)
			}
			writeDiscoveryFile(t, dir, tc.name, tc.body+"\n ")
			if err := ValidateAccepted(context.Background(), dir, env); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("stale source accepted: %v", err)
			}
		})
	}
}

func TestResolveRejectsUnsafeOrUnsupportedDefinitions(t *testing.T) {
	for _, tc := range []struct{ name, body, service string }{
		{".weft/environment.yaml", "image: alpine\nnetwork: {run: bridge}", ""},
		{".weft/environment.yaml", "image: alpine\ncaches: [/workspace]", ""},
		{".weft/environment.yaml", "image: alpine\ncpu: '100'", ""},
		{".weft/environment.yaml", "image: alpine\nprivileged: true", ""},
		{"Dockerfile", "FROM alpine\nCOPY ../outside /workspace", ""},
		{"Dockerfile", "FROM alpine\nRUN --mount=type=secret,id=key cat /key", ""},
		{"Dockerfile", "# syntax=evil/frontend\nFROM alpine", ""},
		{"Dockerfile", "FROM alpine\nONBUILD RUN echo hello", ""},
		{"Dockerfile", "FROM alpine\nRUN <<EOF\necho hello\nEOF", ""},
		{"compose.yaml", "services: {dev: {image: node, privileged: true}}", "dev"},
		{"compose.yaml", "services: {dev: {image: node, volumes: ['/tmp:/tmp']}}", "dev"},
		{"compose.yaml", "services: {dev: {image: '${IMAGE}'}}", "dev"},
		{"compose.yaml", "services: {dev: {build: '../outside'}}", "dev"},
		{"compose.yaml", "services: {dev: {image: node}}", "missing"},
	} {
		t.Run(tc.name+tc.body, func(t *testing.T) {
			dir := t.TempDir()
			writeDiscoveryFile(t, dir, tc.name, tc.body)
			_, err := Resolve(context.Background(), dir, discoverSelection(t, dir, tc.service))
			if !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("expected rejection, got %v", err)
			}
		})
	}
}

func TestResolveReferencedDockerfileAndTampering(t *testing.T) {
	for _, name := range []string{".weft/environment.yaml", "compose.yaml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			body := "dockerfile: build/Containerfile"
			service := ""
			if name == "compose.yaml" {
				body = "services: {dev: {build: {context: build, dockerfile: Containerfile}}}"
				service = "dev"
			}
			writeDiscoveryFile(t, dir, name, body)
			writeDiscoveryFile(t, dir, "build/Containerfile", "FROM alpine")
			sel := discoverSelection(t, dir, service)
			env, err := Resolve(context.Background(), dir, sel)
			if err != nil {
				t.Fatal(err)
			}
			if len(env.Origin.Files) != 2 || env.Dockerfile != "FROM alpine" {
				t.Fatalf("%+v", env)
			}
			env.Setup = []string{"echo tampered"}
			if err := ValidateAccepted(context.Background(), dir, env); err == nil {
				t.Fatal("accepted altered definition")
			}
			env.Setup = nil
			writeDiscoveryFile(t, dir, "build/Containerfile", "FROM debian")
			if err := ValidateAccepted(context.Background(), dir, env); err == nil {
				t.Fatal("accepted altered reference")
			}
		})
	}
}

func TestResolveSelectionErrors(t *testing.T) {
	dir := t.TempDir()
	writeDiscoveryFile(t, dir, "Dockerfile", "FROM alpine")
	sel := discoverSelection(t, dir, "")
	bad := sel
	bad.Path = "../Dockerfile"
	if _, err := Resolve(context.Background(), dir, bad); err == nil {
		t.Fatal("accepted traversal")
	}
	bad = sel
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := Resolve(context.Background(), dir, bad); err == nil {
		t.Fatal("accepted stale hash")
	}
	writeDiscoveryFile(t, dir, ".weft/environment.yaml", "image: alpine")
	if _, err := Resolve(context.Background(), dir, sel); err == nil {
		t.Fatal("accepted lower priority")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, dir, sel); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestResolveMissingAndEscapingReference(t *testing.T) {
	dir := t.TempDir()
	writeDiscoveryFile(t, dir, ".weft/environment.yaml", "dockerfile: Containerfile")
	sel := discoverSelection(t, dir, "")
	if _, err := Resolve(context.Background(), dir, sel); err == nil {
		t.Fatal("accepted missing file")
	}
	outside := t.TempDir()
	writeDiscoveryFile(t, outside, "Containerfile", "FROM alpine")
	if err := os.Symlink(filepath.Join(outside, "Containerfile"), filepath.Join(dir, "Containerfile")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), dir, sel); err == nil {
		t.Fatal("accepted escaping reference")
	}
}
