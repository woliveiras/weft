package environment

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
)

func TestBoundedBuilderEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name, host, verify, tls string
		wantContext             bool
	}{
		{name: "plain TCP", host: "tcp://sandbox-daemon:2375"},
		{name: "native Unix socket", host: "unix:///var/run/docker.sock", verify: "1"},
		{name: "native default"},
		{name: "verified TLS", host: "tcp://sandbox-daemon:2376", verify: "1", wantContext: true},
		{name: "TLS without verification", host: "tcp://sandbox-daemon:2376", tls: "1", wantContext: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := setup(t)
			h.m.opt.DockerHost = tc.host
			t.Setenv("DOCKER_HOST", "")
			t.Setenv("DOCKER_TLS_VERIFY", tc.verify)
			t.Setenv("DOCKER_TLS", tc.tls)
			t.Setenv("DOCKER_CERT_PATH", t.TempDir())
			for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
				if err := os.WriteFile(filepath.Join(os.Getenv("DOCKER_CERT_PATH"), name), []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			const endpoint = "weft-builder-endpoint-bookaneer"
			h.docker.set(t, "contexts", "default\n"+endpoint+"\nunrelated\n")
			g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22"})
			ctx := context.WithValue(context.Background(), buildBudgetKey{}, buildBudget{g.Metadata.ID, 64 << 20, 500})
			ctx = context.WithValue(ctx, preparationKey{}, preparationNetwork{"isolated-build", "http://172.18.0.2:3128"})
			var log bytes.Buffer
			if _, err := h.m.prepareBoundedImage(ctx, g, "test", &log); err != nil {
				t.Fatalf("build: %v\n%s", err, &log)
			}
			calls := h.docker.calls(t)
			prefix := ""
			if tc.wantContext {
				if len(calls) != 8 || calls[0] != "context ls --format {{.Name}}" || calls[1] != "context rm "+endpoint || !strings.HasSuffix(calls[2], " context create --from default "+endpoint) || calls[7] != "context rm "+endpoint {
					t.Fatalf("unexpected context lifecycle: %v", calls)
				}
				prefix = "--context " + endpoint + " "
				calls = calls[3:7]
			}
			if len(calls) != 4 || calls[0] != prefix+"buildx ls --format {{.Name}}" || !strings.HasPrefix(calls[1], prefix+"buildx create --name weft-builder-bookaneer --driver docker-container ") || !strings.HasPrefix(calls[2], prefix+"buildx build ") || calls[3] != prefix+"buildx rm --force weft-builder-bookaneer" {
				t.Fatalf("unexpected Buildx commands: %v", calls)
			}
			if strings.HasSuffix(calls[1], " "+endpoint) != tc.wantContext || !strings.Contains(calls[1], "--driver-opt network=isolated-build") {
				t.Fatalf("unexpected Buildx endpoint/network: %s", calls[1])
			}
			config, err := os.ReadFile(filepath.Join(h.docker.dir, "buildkitd-config"))
			if err != nil || !strings.Contains(string(config), `nameservers = ["127.0.0.1"]`) {
				t.Fatalf("worker DNS config: %q, %v", config, err)
			}
		})
	}
}

func TestBoundedBuilderConnectionErrorsHideCLIOutput(t *testing.T) {
	for _, failure := range []string{"missing key", "unreadable certificate", "buildx create"} {
		t.Run(failure, func(t *testing.T) {
			h := setup(t)
			dir := t.TempDir()
			t.Setenv("DOCKER_CERT_PATH", dir)
			t.Setenv("DOCKER_TLS_VERIFY", "1")
			for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
				path := filepath.Join(dir, name)
				if name == "key.pem" && failure == "missing key" {
					continue
				}
				if name == "cert.pem" && failure == "unreadable certificate" {
					// A directory cannot be read as a certificate, even as root.
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "buildx create" {
				h.docker.set(t, "fail-buildx-create", "")
			}
			g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22"})
			ctx := context.WithValue(context.Background(), buildBudgetKey{}, buildBudget{g.Metadata.ID, 64 << 20, 500})
			var log bytes.Buffer
			_, err := h.m.prepareBoundedImage(ctx, g, "test", &log)
			if err == nil || !strings.Contains(err.Error(), "sandbox daemon build connection") {
				t.Fatalf("unexpected failure: %v", err)
			}
			for _, output := range []string{err.Error(), log.String()} {
				if strings.Contains(output, dir) || strings.Contains(output, "boom:") || strings.Contains(output, "step output") {
					t.Fatalf("raw Docker diagnostic escaped: %s", output)
				}
			}
			calls := h.docker.calls(t)
			if slices.ContainsFunc(calls, func(call string) bool { return strings.Contains(call, "buildx build ") }) {
				t.Fatalf("build ran after endpoint failure: %v", calls)
			}
			if failure == "buildx create" && calls[len(calls)-1] != "context rm weft-builder-endpoint-bookaneer" {
				t.Fatalf("context left after failed create: %v", calls)
			}
		})
	}
}
