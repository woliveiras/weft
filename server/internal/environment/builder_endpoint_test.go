package environment

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
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
			if tc.wantContext {
				h.docker.set(t, "buildx-create-output", "loaded TLS key from /extra dir/key.pem\n")
			}
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
				prefix = "--context " + endpoint + " "
				removeContext := slices.Index(calls, "context rm "+endpoint)
				createContext := slices.IndexFunc(calls, func(call string) bool {
					return strings.HasSuffix(call, " context create --from default "+endpoint)
				})
				removeBuilder := slices.Index(calls, prefix+"buildx rm --force "+builderName(g.Metadata.ID))
				finalCleanup := slices.Index(calls[removeBuilder+1:], "context rm "+endpoint)
				if removeContext < 0 || createContext <= removeContext || removeBuilder <= createContext || finalCleanup < 0 {
					t.Fatalf("unexpected context lifecycle: %v", calls)
				}
			}
			for _, call := range calls {
				if strings.Contains(call, "buildx ") && strings.HasPrefix(call, "--context ") != tc.wantContext {
					t.Fatalf("unexpected context on Buildx command: %s", call)
				}
			}
			for _, command := range []string{"ls", "create", "build", "rm"} {
				if !slices.ContainsFunc(calls, func(call string) bool {
					return strings.HasPrefix(call, prefix+"buildx "+command+" ")
				}) {
					t.Fatalf("missing Buildx %s: %v", command, calls)
				}
			}
			create := calls[slices.IndexFunc(calls, func(call string) bool { return strings.HasPrefix(call, prefix+"buildx create ") })]
			if !strings.Contains(create, "--name weft-builder-bookaneer --driver docker-container") ||
				strings.HasSuffix(create, " "+endpoint) != tc.wantContext || !strings.Contains(create, "--driver-opt network=isolated-build") {
				t.Fatalf("unexpected Buildx endpoint/network: %s", create)
			}
			if !strings.Contains(log.String(), builderName(g.Metadata.ID)) {
				t.Fatalf("missing safe create output: %s", &log)
			}
			if strings.Contains(log.String(), "/extra dir") {
				t.Fatalf("create output contained a key path: %s", &log)
			}
			assertBuilderFixture(t, h.docker, "builders", "")
			contexts := "default\n" + endpoint + "\nunrelated"
			if tc.wantContext {
				contexts = "default\nunrelated"
			}
			assertBuilderFixture(t, h.docker, "contexts", contexts)
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
			if failure == "buildx create" && !slices.Contains(calls, "context rm weft-builder-endpoint-bookaneer") {
				t.Fatalf("context left after failed create: %v", calls)
			}
		})
	}
}

func assertBuilderFixture(t *testing.T, docker *fakeDocker, file, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docker.dir, file))
	if err != nil || strings.TrimSpace(string(data)) != want {
		t.Fatalf("%s: got %q (%v), want %q", file, data, err, want)
	}
}

func TestSanitizeBuilderDiagnostic(t *testing.T) {
	const dir = "/private/client material"
	raw := `unable to connect: open /private/client material/key.pem: permission denied; --tlskey "/outside private/key" --tlscert=/outside/cert --tlscacert '/outside ca' "/other path/ca.pem" /extra/CLIENT.PEM`
	got := sanitizeBuilderDiagnostic(raw, dir)
	for _, forbidden := range []string{dir, "outside", "other path", "/extra/", ".pem", ".PEM", "--tls"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sensitive text %q in %q", forbidden, got)
		}
	}
	if !strings.Contains(got, "permission denied") {
		t.Fatalf("lost diagnostic: %s", got)
	}
	if got := sanitizeBuilderDiagnostic("error: -----BEGIN PRIVATE KEY-----\ncredential-fixture", dir); strings.Contains(got, "credential-fixture") {
		t.Fatalf("PEM material escaped: %s", got)
	}
}

func TestBuilderConnectionAndCleanupIntegration(t *testing.T) {
	for _, tc := range []struct {
		failure, operation string
		tls, stale, ready  bool
	}{
		{failure: "context-ls", operation: "list Docker contexts", tls: true},
		{failure: "context-rm", operation: "remove Docker TLS context", tls: true, stale: true},
		{failure: "context-create", operation: "create Docker TLS context", tls: true},
		{failure: "buildx-ls", operation: "list bounded builders", tls: true},
		{failure: "buildx-rm", operation: "remove stale bounded builder", tls: true, stale: true},
		{failure: "buildx-create", operation: "create bounded builder", tls: true},
		{failure: "buildx-build", tls: true},
		{failure: "final-context-rm", operation: "remove Docker TLS context", tls: true, ready: true},
		{failure: "final-buildx-rm", operation: "remove bounded builder", tls: true, ready: true},
		{failure: "buildx-ls"},
		{failure: "buildx-create"},
	} {
		t.Run(tc.failure, func(t *testing.T) {
			h := capacityHarness(t)
			var server bytes.Buffer
			h.m.opt.Log = slog.New(slog.NewJSONHandler(&server, nil))
			t.Setenv("DOCKER_TLS", "")
			t.Setenv("DOCKER_TLS_VERIFY", "")
			t.Setenv("DOCKER_CERT_PATH", "")
			if tc.tls {
				t.Setenv("DOCKER_TLS_VERIFY", "1")
			}
			g := graphWith(&domain.EnvironmentSpec{Image: "alpine:3.22"})
			endpoint := builderEndpointName(g.Metadata.ID)
			h.docker.set(t, "contexts", "default\n")
			if tc.stale && tc.failure == "context-rm" {
				h.docker.set(t, "contexts", "default\n"+endpoint+"\n")
			}
			if tc.stale && tc.failure == "buildx-rm" {
				h.docker.set(t, "builders", builderName(g.Metadata.ID)+"\n")
			}
			diagnostic := ""
			if tc.operation != "" {
				diagnostic = "cannot access /outside/private/key.pem"
				if tc.failure == "buildx-create" {
					t.Setenv("DOCKER_CERT_PATH", h.docker.dir+"/")
					for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
						h.docker.set(t, name, "fixture")
					}
					diagnostic = strings.Repeat("earlier Docker output\n", 700) + "cannot access " + h.docker.dir + ": permission denied; " + diagnostic
				}
			}
			h.docker.set(t, "fail-"+tc.failure, diagnostic)
			if _, err := h.m.Build(context.Background(), g, true, "test"); err != nil {
				t.Fatal(err)
			}
			v := h.wait(t, g)
			h.m.mu.Lock()
			b := h.m.running[g.Metadata.ID]
			h.m.mu.Unlock()
			if b != nil {
				<-b.done
			}
			if tc.ready {
				if v.State != StateReady || v.Image == "" || !strings.Contains(v.Log, "Warning: could not "+tc.operation) {
					t.Fatalf("expected ready image with cleanup warning: %+v", v)
				}
			} else if v.State != StateFailed {
				t.Fatalf("expected failed build: %+v", v)
			}
			if tc.operation != "" {
				safeError := "sandbox daemon build connection: could not " + tc.operation
				if !tc.ready && (!strings.Contains(v.Error, safeError) || !strings.Contains(v.Log, safeError)) {
					t.Fatalf("missing safe error: %+v", v)
				}
				logged := server.String()
				if strings.Contains(v.Error+v.Log+logged, "/outside/private") ||
					!strings.Contains(logged, `"graph":"bookaneer"`) ||
					strings.Count(logged, `"operation":"`+tc.operation+`"`) != 1 ||
					!strings.Contains(logged, `"diagnostic":`) {
					t.Fatalf("unexpected diagnostic: %+v\n%s", v, logged)
				}
				if tc.failure == "buildx-create" && (!strings.Contains(logged, "[earlier Docker output truncated]") ||
					!strings.Contains(logged, "[TLS directory]: permission denied") || strings.Contains(logged, h.docker.dir)) {
					t.Fatalf("truncated diagnostic lost its sanitized cause: %s", logged)
				}
				if !tc.ready && !strings.Contains(logged, "[PEM file]") {
					t.Fatalf("sanitized cause missing: %s", logged)
				}
			} else if !strings.Contains(v.Error, "boom: "+tc.failure+" failed") {
				t.Fatalf("raw non-TLS Docker cause missing: %+v", v)
			}
			if strings.HasPrefix(tc.failure, "buildx-") {
				assertBuilderFixture(t, h.docker, "contexts", "default")
			}
			if tc.failure == "buildx-build" {
				assertBuilderFixture(t, h.docker, "builders", "")
			}
			if tc.ready {
				if tc.failure == "final-context-rm" {
					assertBuilderFixture(t, h.docker, "builders", "")
					assertBuilderFixture(t, h.docker, "contexts", "default\n"+endpoint)
				} else {
					assertBuilderFixture(t, h.docker, "builders", builderName(g.Metadata.ID))
					assertBuilderFixture(t, h.docker, "contexts", "default")
				}
				if err := os.Remove(filepath.Join(h.docker.dir, "fail-"+tc.failure)); err != nil {
					t.Fatal(err)
				}
				if _, err := h.m.Build(context.Background(), g, true, "test"); err != nil {
					t.Fatal(err)
				}
				if v := h.wait(t, g); v.State != StateReady || strings.Contains(v.Log, "Warning:") {
					t.Fatalf("retry did not recover the stale context: %+v", v)
				}
				assertBuilderFixture(t, h.docker, "contexts", "default")
				assertBuilderFixture(t, h.docker, "builders", "")
			}
		})
	}
}

func TestResetReconcilesOnlyGraphBuilderIntegration(t *testing.T) {
	for _, staleContext := range []bool{false, true} {
		t.Run(fmt.Sprint(staleContext), func(t *testing.T) {
			h := capacityHarness(t)
			t.Setenv("DOCKER_TLS_VERIFY", "1")
			const graphID = "bookaneer"
			h.docker.set(t, "builders", "default\n"+builderName(graphID)+"*\n"+builderName("other")+"\nexternal\n")
			contexts := "default\n" + builderEndpointName("other") + "\nexternal\n"
			if staleContext {
				h.docker.set(t, "contexts", contexts+builderEndpointName(graphID)+"\n")
			} else {
				h.docker.set(t, "contexts", contexts)
			}
			if err := h.m.Reset(context.Background(), graphID, "test"); err != nil {
				t.Fatal(err)
			}
			assertBuilderFixture(t, h.docker, "builders", "default\n"+builderName("other")+"\nexternal")
			assertBuilderFixture(t, h.docker, "contexts", strings.TrimSpace(contexts))
		})
	}
}

func TestBuilderDiagnosticTail(t *testing.T) {
	raw := strings.Repeat("earlier output\n", 1000) + "cannot open /private/client: permission denied"
	for _, dir := range []string{"/private/client/", "/private//nested/../client/"} {
		got := builderDiagnosticTail(raw, dir)
		if !strings.HasPrefix(got, "[earlier Docker output truncated]") ||
			!strings.HasSuffix(got, "cannot open [TLS directory]: permission denied") ||
			strings.Contains(got, "/private/") {
			t.Fatalf("diagnostic tail lost its safe cause: %q", got)
		}
	}
	got := builderDiagnosticTail("open /private//client without a filename", "/private//client/")
	if strings.Contains(got, "/private/") {
		t.Fatalf("unnormalized path leaked: %q", got)
	}
}

func TestResetContextLookupOnlyWithTLS(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(fmt.Sprint(tls), func(t *testing.T) {
			h := capacityHarness(t)
			t.Setenv("DOCKER_TLS", "")
			t.Setenv("DOCKER_TLS_VERIFY", "")
			if tls {
				t.Setenv("DOCKER_TLS_VERIFY", "1")
			}
			h.docker.set(t, "fail-context-ls", "open /private/client/key.pem")
			err := h.m.Reset(t.Context(), "bookaneer", "test")
			if tls && (err == nil || !strings.Contains(err.Error(), "clean up Docker TLS context") || strings.Contains(err.Error(), "build connection") || strings.Contains(err.Error(), "/private/")) {
				t.Fatalf("unexpected cleanup error: %v", err)
			}
			if !tls && (err != nil || slices.ContainsFunc(h.docker.calls(t), func(call string) bool { return strings.HasPrefix(call, "context ") })) {
				t.Fatalf("plain reset called Docker contexts: %v, %v", err, h.docker.calls(t))
			}
		})
	}
}
