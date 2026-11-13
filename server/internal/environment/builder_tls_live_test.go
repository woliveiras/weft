package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
	"github.com/woliveiras/weft/server/internal/storage/fsstore"
)

func TestBoundedBuildRealTLS(t *testing.T) {
	if os.Getenv("WEFT_TEST_ENV_TLS") != "1" {
		t.Skip("run scripts/test-environment-build-tls.sh")
	}
	const host = "tcp://sandbox-daemon:2376"
	base := "weft-tls-fixture:latest"
	online := os.Getenv("WEFT_TEST_ENV_TLS_ONLINE") == "1"
	const worker = "moby/buildkit:buildx-stable-1"
	const proxy = "weft/egress:latest"
	if os.Getenv("DOCKER_TLS_VERIFY") != "1" || os.Getenv("DOCKER_CERT_PATH") == "" {
		t.Fatal("the integration requires the Compose Docker client TLS configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, err := fsstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("tls-build-test-%d", time.Now().UnixNano())
	hosts := []string{"example.com"}
	if online {
		base = "golang:1.26-alpine"
		hosts = []string{"registry-1.docker.io", "auth.docker.io", "production.cloudflare.docker.com", "production.cloudfront.docker.com", "docker-images-prod.6aa30f8b08e16409b46e0173d6de2f56.r2.cloudflarestorage.com"}
	}
	budget := &sandboxcapacity.Manager{Store: store, Scope: id, Policy: sandboxcapacity.Policy{MaxActive: 3, TotalMemory: "2g", DefaultMemory: "512m", TotalCPU: "4", DefaultCPU: "1"}}
	m, err := New(Options{Store: store, Workspaces: fakeWorkspaces{dir: t.TempDir()}, DockerHost: host, EgressImage: proxy, Capacity: budget,
		Policy: environmentpolicy.Policy{SetupNetworks: []string{"bridge", "egress"}, EgressHosts: hosts}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.docker(ctx, nil, io.Discard, "image", "inspect", proxy); err != nil {
		t.Fatalf("required image %s must already be present in the dedicated daemon", proxy)
	}
	var workerTag string
	if !online {
		workerTag = configureOfflineTLSBuild(t, ctx, m, id, base, worker)
	}
	t.Run("reset-orphaned-builder", func(t *testing.T) {
		graphID := id + "-orphan"
		endpoint, err := m.builderContext(ctx, graphID, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			if err := m.Reset(cleanup, graphID, "test"); err != nil {
				t.Error(err)
			}
		})
		var out bytes.Buffer
		if err := m.docker(ctx, nil, &out, "--context", endpoint, "buildx", "create", "--name", builderName(graphID), "--driver", "docker-container", endpoint); err != nil {
			t.Fatalf("create test builder: %v", err)
		}
		if err := m.removeBuilderContext(ctx, endpoint); err != nil {
			t.Fatal(err)
		}
		checkBuilder := func(present bool) {
			t.Helper()
			out.Reset()
			if err := m.docker(ctx, nil, &out, "buildx", "ls", "--format", "{{.Name}}"); err != nil || slices.Contains(strings.Fields(out.String()), builderName(graphID)) != present {
				t.Fatalf("orphaned builder presence should be %t: %v\n%s", present, err, &out)
			}
		}
		checkBuilder(true)
		if err := m.Reset(ctx, graphID, "test"); err != nil {
			t.Fatal(err)
		}
		checkBuilder(false)
	})
	for _, tc := range []struct {
		name string
		env  *domain.EnvironmentSpec
	}{
		{"image", &domain.EnvironmentSpec{Image: base, Network: domain.EnvironmentNetwork{Setup: "bridge"}}},
		{"dockerfile-egress", &domain.EnvironmentSpec{
			Dockerfile: "FROM " + base + "\nRUN test -n \"$HTTPS_PROXY\" && grep -q '^nameserver 127.0.0.1$' /etc/resolv.conf\n",
			Network:    domain.EnvironmentNetwork{Setup: "egress", SetupAllowedHosts: hosts},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := graphWith(tc.env)
			g.Metadata.ID = id + "-" + tc.name
			final := ImageRef(g.Metadata.ID, Hash(tc.env))
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
				defer stop()
				if err := m.Reset(cleanup, g.Metadata.ID, "test"); err != nil {
					t.Error(err)
				}
			})
			var log bytes.Buffer
			image, _, err := m.withBuildCapacity(ctx, g, &log, func(ctx context.Context) (string, []CacheMount, error) {
				image, err := m.prepareBoundedImage(ctx, g, final, &log)
				return image, nil, err
			})
			if err != nil {
				t.Fatalf("bounded TLS build: %v\n%s", err, &log)
			}
			expected := []string{"Environment capacity: released"}
			if !online {
				expected = append(expected, "using local image "+workerTag)
			}
			for _, line := range expected {
				if !strings.Contains(log.String(), line) {
					t.Fatalf("missing %q: %s", line, &log)
				}
			}
			if tc.env.Network.Setup == "egress" && !strings.Contains(log.String(), "Preparation egress proxy ready") {
				t.Fatalf("missing preparation proxy: %s", &log)
			}
			var out bytes.Buffer
			if err := m.docker(ctx, nil, &out, "image", "inspect", "--format", "{{.Config.User}}", image); err != nil || strings.TrimSpace(out.String()) != "10001" {
				t.Fatalf("sandbox user in loaded image: %q (%v)", &out, err)
			}
			for _, check := range []struct {
				args []string
				name string
			}{
				{[]string{"context", "ls", "--format", "{{.Name}}"}, builderEndpointName(g.Metadata.ID)},
				{[]string{"buildx", "ls", "--format", "{{.Name}}"}, builderName(g.Metadata.ID)},
				{[]string{"ps", "-a", "--format", "{{.Names}}"}, builderContainer(g.Metadata.ID)},
			} {
				out.Reset()
				if err := m.docker(ctx, nil, &out, check.args...); err != nil || slices.Contains(strings.Fields(out.String()), check.name) {
					t.Fatalf("cleanup of %s: %q (%v)", check.name, &out, err)
				}
			}
		})
	}
}

func configureOfflineTLSBuild(t *testing.T, ctx context.Context, m *Manager, id, base, worker string) string {
	t.Helper()
	if err := m.docker(ctx, nil, io.Discard, "image", "inspect", worker); err != nil {
		t.Fatalf("offline TLS integration requires %s in the dedicated daemon cache", worker)
	}
	// The worker tries to pull its image before falling back to the daemon's
	// cache. Point that attempt at a closed loopback port, and give the solve
	// an OCI layout so the production --pull flag never needs a registry.
	workerTag := "127.0.0.1:1/" + id + ":latest"
	if err := m.docker(ctx, nil, io.Discard, "tag", worker, workerTag); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if err := m.docker(cleanup, nil, io.Discard, "image", "rm", workerTag); err != nil {
			t.Error(err)
		}
	})
	var platform bytes.Buffer
	if err := m.docker(ctx, nil, &platform, "info", "--format", "{{.OSType}} {{.Architecture}}"); err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(platform.String())
	if len(fields) != 2 {
		t.Fatalf("invalid daemon platform: %q", &platform)
	}
	arch := fields[1]
	switch arch {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	}
	layout := os.Getenv("WEFT_TEST_TLS_OCI_LAYOUT")
	digest, err := fixtureManifest(layout, fields[0], arch)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEFT_TEST_BUILDKIT_IMAGE", workerTag)
	t.Setenv("WEFT_TEST_BUILD_CONTEXT", base+"=oci-layout://"+layout+"@"+digest)
	wrapper := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(wrapper, []byte(`#!/bin/sh
set -eu
if [ "$1" = --context ] && [ "$3" = buildx ]; then
  endpoint=$2 operation=$4
  shift 4
  case "$operation" in
    create) set -- --driver-opt "image=$WEFT_TEST_BUILDKIT_IMAGE" "$@" ;;
    build) set -- --build-context "$WEFT_TEST_BUILD_CONTEXT" "$@" ;;
  esac
  set -- --context "$endpoint" buildx "$operation" "$@"
fi
exec docker "$@"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	m.opt.DockerBin = wrapper
	return workerTag
}

func fixtureManifest(layout, osName, arch string) (string, error) {
	return findFixtureManifest(layout, filepath.Join(layout, "index.json"), osName, arch)
}

func findFixtureManifest(layout, path, osName, arch string) (string, error) {
	var index struct {
		Manifests []struct {
			Digest, MediaType string
			Platform          *struct{ OS, Architecture string }
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return "", err
	}
	for _, entry := range index.Manifests {
		if entry.Platform != nil && (entry.Platform.OS != osName || entry.Platform.Architecture != arch) {
			continue
		}
		if entry.Platform != nil {
			return entry.Digest, nil
		}
		if strings.HasSuffix(entry.MediaType, ".index.v1+json") || strings.HasSuffix(entry.MediaType, ".manifest.list.v2+json") {
			algorithm, value, ok := strings.Cut(entry.Digest, ":")
			if !ok || algorithm != "sha256" || len(value) != 64 || strings.ContainsAny(value, "/\\") {
				return "", fmt.Errorf("invalid OCI index digest %q", entry.Digest)
			}
			return findFixtureManifest(layout, filepath.Join(layout, "blobs", algorithm, value), osName, arch)
		}
		if len(index.Manifests) == 1 && entry.Digest != "" {
			return entry.Digest, nil
		}
	}
	return "", fmt.Errorf("cached Alpine OCI layout has no manifest for %s/%s", osName, arch)
}

func TestFixtureManifestSelectsDaemonPlatform(t *testing.T) {
	layout := t.TempDir()
	index := `{"manifests":[
		{"digest":"sha256:amd","platform":{"os":"linux","architecture":"amd64"}},
		{"digest":"sha256:attestation","platform":{"os":"unknown","architecture":"unknown"}},
		{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}}
	]}`
	if err := os.WriteFile(filepath.Join(layout, "index.json"), []byte(index), 0600); err != nil {
		t.Fatal(err)
	}
	if digest, err := fixtureManifest(layout, "linux", "arm64"); err != nil || digest != "sha256:arm" {
		t.Fatalf("manifest = %q, %v", digest, err)
	}
	if _, err := fixtureManifest(layout, "linux", "s390x"); err == nil {
		t.Fatal("expected missing platform error")
	}
}
