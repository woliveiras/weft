package environment

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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
	const base = "golang:1.26-alpine"
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
	hosts := []string{"registry-1.docker.io", "auth.docker.io", "production.cloudflare.docker.com", "production.cloudfront.docker.com", "docker-images-prod.6aa30f8b08e16409b46e0173d6de2f56.r2.cloudflarestorage.com"}
	budget := &sandboxcapacity.Manager{Store: store, Scope: id, Policy: sandboxcapacity.Policy{MaxActive: 3, TotalMemory: "2g", DefaultMemory: "512m", TotalCPU: "4", DefaultCPU: "1"}}
	m, err := New(Options{Store: store, Workspaces: fakeWorkspaces{dir: t.TempDir()}, DockerHost: host, EgressImage: proxy, Capacity: budget,
		Policy: environmentpolicy.Policy{SetupNetworks: []string{"bridge", "egress"}, EgressHosts: hosts}})
	if err != nil {
		t.Fatal(err)
	}
	for _, image := range []string{base, proxy, "moby/buildkit:buildx-stable-1"} {
		if err := m.docker(ctx, nil, io.Discard, "image", "inspect", image); err != nil {
			t.Fatalf("required image %s must already be present in the dedicated daemon", image)
		}
	}
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
			if !strings.Contains(log.String(), "Environment capacity: released") {
				t.Fatalf("missing capacity release: %s", &log)
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
